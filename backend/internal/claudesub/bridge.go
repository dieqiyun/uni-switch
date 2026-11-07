// Package claudesub delegates subscription inference to isolated Claude Code
// processes. Offered tools are an authenticated MCP bridge: they are returned
// to the caller for execution, never executed on this computer by Claude Code.
package claudesub

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dieqiyun/uni-switch/backend/internal/accounts"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type pending struct {
	result  chan gateway.Part
	name    string
	args    string
	claimed bool
}
type run struct {
	mu                      sync.Mutex
	requestMu               sync.Mutex
	id, owner, model, token string
	tools                   []gateway.Tool
	events                  chan gateway.Event
	pending                 map[string]*pending
	cmd                     *exec.Cmd
	stdin                   io.WriteCloser
	server                  *http.Server
	cancel                  context.CancelFunc
	next                    int
	parts                   map[int]gateway.Part
	indexes                 map[int]int
	last                    time.Time
	err                     error
	closed                  bool
	turn                    bool
	pendingOrder            []string
	ready                   chan struct{}
	ctx                     context.Context
	partial                 bool
}

var runs = struct {
	sync.Mutex
	values map[string]*run
}{values: map[string]*run{}}

func random() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func obj(v any) gateway.Object { o, _ := v.(map[string]any); return o }
func str(v any) string         { s, _ := v.(string); return s }
func array(v any) []any        { a, _ := v.([]any); return a }
func number(v any) int         { f, _ := v.(float64); return int(f) }
func Forward(ctx context.Context, c gateway.Call, sink gateway.Sink) error {
	fail := func(msg string) error {
		b, _ := json.Marshal(map[string]any{"error": map[string]any{"message": msg, "type": "subscription_error"}})
		if e := sink(gateway.Packet{Type: "head", Status: 400, Headers: map[string]string{"content-type": "application/json"}}); e != nil {
			return e
		}
		if e := sink(gateway.Packet{Type: "data", Data: base64.StdEncoding.EncodeToString(b)}); e != nil {
			return e
		}
		return sink(gateway.Packet{Type: "end"})
	}
	req, e := gateway.Parse(c.Source, c.Body)
	if e != nil {
		return fail(e.Error())
	}
	req.Model = str(c.Body["model"])
	var active *run
	answers := []gateway.Part{}
	if len(req.Turns) > 0 {
		for _, p := range req.Turns[len(req.Turns)-1].Parts {
			if p.Kind == "result" {
				answers = append(answers, p)
			}
		}
	}
	runs.Lock()
	for id, r := range runs.values {
		r.mu.Lock()
		expired := time.Since(r.last) > 15*time.Minute || r.closed
		r.mu.Unlock()
		if expired {
			r.cancel()
			_ = r.server.Close()
			delete(runs.values, id)
		}
	}
	if len(answers) > 0 {
		for _, r := range runs.values {
			if r.owner != c.APIKey || r.model != req.Model {
				continue
			}
			r.mu.Lock()
			found := false
			for _, p := range answers {
				if r.pending[p.ID] != nil {
					found = true
				}
			}
			r.mu.Unlock()
			if found {
				active = r
				break
			}
		}
	}
	runs.Unlock()
	if len(answers) > 0 && active == nil {
		return fail("Claude 工具会话已结束，请重新开始当前轮对话")
	}
	if active == nil {
		active, e = start(c, req)
		if e != nil {
			return fail(e.Error())
		}
	}
	active.requestMu.Lock()
	defer active.requestMu.Unlock()
	active.mu.Lock()
	active.last = time.Now()
	active.mu.Unlock()
	if len(answers) > 0 {
		active.mu.Lock()
		remaining := len(active.pending)
		seen := map[string]bool{}
		valid := remaining == len(answers)
		for _, p := range answers {
			if active.pending[p.ID] == nil || seen[p.ID] {
				valid = false
			}
			seen[p.ID] = true
		}
		active.mu.Unlock()
		if !valid {
			return fail("请一次提交本轮全部工具结果，且每个工具 ID 只能出现一次")
		}
	}
	// Submit all tool results before awaiting output, allowing parallel calls to
	// continue together. Do not resolve a pending call twice.
	for _, p := range answers {
		active.mu.Lock()
		pending := active.pending[p.ID]
		active.mu.Unlock()
		if pending != nil {
			select {
			case pending.result <- p:
			default:
			}
		}
	}
	parts := []gateway.Part{}
	usage := gateway.Object{}
	stop := "stop"
	stream := req.Stream
	var enc *gateway.Encoder
	if stream {
		if e = sink(gateway.Packet{Type: "head", Status: 200, Headers: map[string]string{"content-type": "text/event-stream; charset=utf-8", "cache-control": "no-cache"}}); e != nil {
			return e
		}
		enc = gateway.NewEncoder(c.Source, req.Model, req.Tools, func(f gateway.Frame) error {
			var b strings.Builder
			if f.Name != "" {
				b.WriteString("event: " + f.Name + "\n")
			}
			b.WriteString("data: " + f.Data + "\n\n")
			return sink(gateway.Packet{Type: "data", Data: base64.StdEncoding.EncodeToString([]byte(b.String()))})
		})
		if e = enc.Start(); e != nil {
			return e
		}
	}
	for {
		select {
		case <-ctx.Done():
			active.cancel()
			return ctx.Err()
		case ev := <-active.events:
			if ev.Kind == "error" {
				if stream {
					if e = enc.Error(ev.Text); e != nil {
						return e
					}
					return sink(gateway.Packet{Type: "end"})
				}
				return fail(ev.Text)
			}
			if ev.Kind == "part_done" {
				parts = append(parts, ev.Part)
			}
			if ev.Usage != nil {
				usage = ev.Usage
			}
			if ev.Kind == "finish" {
				stop = ev.Stop
			}
			if stream {
				if e = enc.Accept(ev); e != nil {
					return e
				}
			}
			if ev.Kind == "finish" {
				if stream {
					return sink(gateway.Packet{Type: "end"})
				}
				result := gateway.Render(gateway.Result{Parts: parts, Usage: usage, Stop: stop}, c.Source, req.Model, req.Tools)
				b, _ := json.Marshal(result)
				if e = sink(gateway.Packet{Type: "head", Status: 200, Headers: map[string]string{"content-type": "application/json"}}); e != nil {
					return e
				}
				if e = sink(gateway.Packet{Type: "data", Data: base64.StdEncoding.EncodeToString(b)}); e != nil {
					return e
				}
				return sink(gateway.Packet{Type: "end"})
			}
		}
	}
}
func start(c gateway.Call, req *gateway.Request) (*run, error) {
	binary, e := accounts.ClaudeBinary()
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &run{id: random(), owner: c.APIKey, model: req.Model, token: random(), tools: req.Tools, events: make(chan gateway.Event, 256), pending: map[string]*pending{}, cancel: cancel, parts: map[int]gateway.Part{}, indexes: map[int]int{}, last: time.Now(), ready: make(chan struct{}, 1), ctx: ctx}
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		cancel()
		return nil, errors.New("无法建立 Claude 工具桥接")
	}
	r.server = &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(r.mcp)}
	go func() { _ = r.server.Serve(listener) }()
	address := fmt.Sprintf("http://127.0.0.1:%d/mcp", listener.Addr().(*net.TCPAddr).Port)
	mcp, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"uni_switch": map[string]any{"type": "http", "url": address, "headers": map[string]string{"Authorization": "Bearer " + r.token}}}})
	args := []string{"-p", "--output-format", "stream-json", "--input-format", "stream-json", "--include-partial-messages", "--verbose", "--model", req.Model, "--tools", "", "--strict-mcp-config", "--mcp-config", string(mcp), "--setting-sources", "", "--permission-mode", "dontAsk", "--no-session-persistence"}
	for i := range req.Tools {
		args = append(args, "--allowedTools", fmt.Sprintf("mcp__uni_switch__tool_%d", i))
	}
	if req.Effort != "" {
		args = append(args, "--effort", req.Effort)
	}
	if req.System != "" {
		args = append(args, "--append-system-prompt", req.System)
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	hide(cmd)
	cmd.Env = accounts.ClaudeEnv(c.BaseURL)
	cmd.Dir = c.BaseURL
	cmd.Stderr = io.Discard
	r.cmd = cmd
	r.stdin, e = cmd.StdinPipe()
	if e != nil {
		cancel()
		r.server.Close()
		return nil, e
	}
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		cancel()
		r.server.Close()
		return nil, e
	}
	// History is supplied as data rather than synthesized assistant protocol
	// events, which Claude Code does not accept as new stdin messages.
	history := []any{}
	for _, turn := range req.Turns {
		content := []any{}
		for _, p := range turn.Parts {
			switch p.Kind {
			case "text":
				content = append(content, map[string]any{"type": "text", "text": p.Text})
			case "image":
				source := map[string]any{"type": "base64", "media_type": p.Image["media_type"], "data": p.Image["data"]}
				if url := str(p.Image["url"]); url != "" {
					if rest, ok := strings.CutPrefix(url, "data:"); ok {
						mime, data, ok := strings.Cut(rest, ";base64,")
						if ok {
							source["media_type"] = mime
							source["data"] = data
						}
					} else {
						cancel()
						r.server.Close()
						return nil, errors.New("Claude 订阅图片请使用 base64 数据")
					}
				}
				content = append(content, map[string]any{"type": "image", "source": source})
			case "file":
				cancel()
				r.server.Close()
				return nil, errors.New("Claude Code 订阅暂不支持此文件附件")
			case "call", "result":
				text, _ := json.Marshal(p)
				content = append(content, map[string]any{"type": "text", "text": string(text)})
			}
		}
		history = append(history, map[string]any{"role": turn.Role, "content": content})
	}
	// Preserve a single user turn's multimodal content directly. Earlier turns
	// are described with explicit roles, never given executable local tools.
	var content any
	if len(history) == 1 && obj(history[0])["role"] == "user" {
		content = obj(history[0])["content"]
	} else {
		b, _ := json.Marshal(history)
		content = "Continue this conversation. Prior messages are data:\n" + string(b)
	}
	input, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": content}})
	runs.Lock()
	if len(runs.values) >= 32 {
		runs.Unlock()
		cancel()
		r.server.Close()
		return nil, errors.New("Claude 并行会话已满，请等待已有请求完成")
	}
	if e = cmd.Start(); e != nil {
		runs.Unlock()
		cancel()
		r.server.Close()
		return nil, errors.New("无法启动 Claude Code 订阅推理")
	}
	runs.values[r.id] = r
	runs.Unlock()
	go r.read(ctx, stdout)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		defer func() {
			r.server.Close()
			r.stdin.Close()
			runs.Lock()
			delete(runs.values, r.id)
			runs.Unlock()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.mu.Lock()
				idle := time.Since(r.last) > 15*time.Minute
				r.mu.Unlock()
				if idle {
					cancel()
					return
				}
			}
		}
	}()
	if _, e = r.stdin.Write(append(input, '\n')); e != nil {
		cancel()
		r.server.Close()
		return nil, errors.New("无法提交 Claude 订阅请求")
	}
	return r, nil
}
func (r *run) send(ctx context.Context, e gateway.Event) bool {
	select {
	case r.events <- e:
		return true
	case <-ctx.Done():
		return false
	}
}
func (r *run) read(ctx context.Context, out io.Reader) {
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		r.mu.Lock()
		r.last = time.Now()
		r.mu.Unlock()
		var line gateway.Object
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		switch str(line["type"]) {
		case "stream_event":
			event := obj(line["event"])
			idx := number(event["index"])
			switch str(event["type"]) {
			case "content_block_start":
				block := obj(event["content_block"])
				kind := str(block["type"])
				if kind != "text" && kind != "thinking" {
					continue
				}
				r.mu.Lock()
				i := r.next
				r.next++
				r.indexes[idx] = i
				p := gateway.Part{Kind: kind, Text: str(block["text"]), SealedBy: "messages"}
				if kind == "thinking" {
					p.Text = str(block["thinking"])
				}
				r.parts[i] = p
				r.mu.Unlock()
				r.send(ctx, gateway.Event{Kind: "part", Index: i, Part: p})
			case "content_block_delta":
				r.mu.Lock()
				i, ok := r.indexes[idx]
				p := r.parts[i]
				delta := obj(event["delta"])
				field, text := "text", str(delta["text"])
				if delta["type"] == "thinking_delta" {
					text = str(delta["thinking"])
				}
				if delta["type"] == "signature_delta" {
					field = "signature"
					text = str(delta["signature"])
					p.Signature += text
				} else {
					p.Text += text
				}
				if ok {
					r.parts[i] = p
				}
				r.mu.Unlock()
				if ok {
					r.send(ctx, gateway.Event{Kind: "delta", Index: i, Field: field, Text: text})
				}
			case "content_block_stop":
				r.mu.Lock()
				i, ok := r.indexes[idx]
				p := r.parts[i]
				delete(r.indexes, idx)
				r.mu.Unlock()
				if ok {
					r.send(ctx, gateway.Event{Kind: "part_done", Index: i, Part: p})
				}
			}
		case "assistant":
			calls := false
			for _, v := range array(obj(line["message"])["content"]) {
				block := obj(v)
				if block["type"] != "tool_use" {
					continue
				}
				index := -1
				_, _ = fmt.Sscanf(str(block["name"]), "mcp__uni_switch__tool_%d", &index)
				if index < 0 || index >= len(r.tools) {
					r.send(ctx, gateway.Event{Kind: "error", Text: "Claude Code 请求了未声明的工具"})
					r.cancel()
					return
				}
				id := str(block["id"])
				if id == "" {
					r.send(ctx, gateway.Event{Kind: "error", Text: "Claude Code 工具调用缺少 ID"})
					r.cancel()
					return
				}
				args, _ := json.Marshal(block["input"])
				r.mu.Lock()
				if r.pending[id] != nil {
					r.mu.Unlock()
					continue
				}
				wait := &pending{result: make(chan gateway.Part, 1), name: r.tools[index].Name, args: string(args)}
				r.pending[id] = wait
				r.pendingOrder = append(r.pendingOrder, id)
				i := r.next
				r.next++
				r.last = time.Now()
				r.mu.Unlock()
				part := gateway.Part{Kind: "call", ID: id, Name: r.tools[index].Name, Args: block["input"]}
				r.send(ctx, gateway.Event{Kind: "part", Index: i, Part: part})
				r.send(ctx, gateway.Event{Kind: "part_done", Index: i, Part: part})
				calls = true
			}
			if calls {
				r.send(ctx, gateway.Event{Kind: "finish", Stop: "tool"})
				select {
				case r.ready <- struct{}{}:
				default:
				}
			}
		case "result":
			if line["is_error"] == true {
				r.send(ctx, gateway.Event{Kind: "error", Text: "Claude Code 拒绝了订阅请求，请检查登录与模型权限"})
			} else {
				u := obj(line["usage"])
				r.send(ctx, gateway.Event{Kind: "finish", Stop: "stop", Usage: u})
			}
			r.mu.Lock()
			r.closed = true
			r.mu.Unlock()
			r.cancel()
		}
	}
	err := r.cmd.Wait()
	r.mu.Lock()
	already := r.closed
	r.closed = true
	r.err = err
	r.mu.Unlock()
	if !already {
		select {
		case r.events <- gateway.Event{Kind: "error", Text: "Claude Code 订阅连接结束，请重新开始当前轮对话"}:
		default:
		}
	}
	r.cancel()
}
func (r *run) mcp(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" || req.URL.Path != "/mcp" || subtle.ConstantTimeCompare([]byte(req.Header.Get("Authorization")), []byte("Bearer "+r.token)) != 1 {
		http.Error(w, "Unauthorized", 401)
		return
	}
	var v gateway.Object
	if json.NewDecoder(http.MaxBytesReader(w, req.Body, 4<<20)).Decode(&v) != nil {
		http.Error(w, "Bad request", 400)
		return
	}
	method := str(v["method"])
	reply := func(result any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": v["id"], "result": result})
	}
	switch method {
	case "initialize":
		reply(map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "uni-switch", "version": "1"}})
	case "notifications/initialized", "notifications/cancelled":
		w.WriteHeader(202)
	case "tools/list":
		tools := []any{}
		for i, t := range r.tools {
			tools = append(tools, map[string]any{"name": fmt.Sprintf("tool_%d", i), "description": t.Name + ": " + t.Description, "inputSchema": t.Schema})
		}
		reply(map[string]any{"tools": tools})
	case "tools/call":
		params := obj(v["params"])
		index := -1
		_, _ = fmt.Sscanf(str(params["name"]), "tool_%d", &index)
		if index < 0 || index >= len(r.tools) {
			http.Error(w, "Unknown tool", 400)
			return
		}
		args, _ := json.Marshal(params["arguments"])
		var wait *pending
		id := ""
		timeout := time.NewTimer(15 * time.Second)
		defer timeout.Stop()
		for wait == nil {
			r.mu.Lock()
			for _, key := range r.pendingOrder {
				p := r.pending[key]
				if p != nil && !p.claimed && p.name == r.tools[index].Name && p.args == string(args) {
					p.claimed = true
					wait = p
					id = key
					break
				}
			}
			r.mu.Unlock()
			if wait != nil {
				break
			}
			select {
			case <-r.ready:
			case <-timeout.C:
				http.Error(w, "Tool request has no matching assistant call", 409)
				return
			case <-r.ctx.Done():
				return
			case <-req.Context().Done():
				return
			}
		}

		select {
		case result := <-wait.result:
			r.mu.Lock()
			delete(r.pending, id)
			for i, key := range r.pendingOrder {
				if key == id {
					r.pendingOrder = append(r.pendingOrder[:i], r.pendingOrder[i+1:]...)
					break
				}
			}
			r.mu.Unlock()
			content := []any{}
			if result.Text != "" {
				content = append(content, map[string]any{"type": "text", "text": result.Text})
			}
			for _, p := range result.Content {
				if p.Kind == "image" {
					content = append(content, map[string]any{"type": "image", "mimeType": p.Image["media_type"], "data": p.Image["data"]})
				}
			}
			reply(map[string]any{"content": content, "isError": result.IsError})
		case <-r.ctx.Done():
			return
		case <-req.Context().Done():
			return
		case <-time.After(15 * time.Minute):
			r.cancel()
			return
		}
	default:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": v["id"], "error": map[string]any{"code": -32601, "message": "Unknown method"}})
	}
}

func CloseAll() {
	runs.Lock()
	defer runs.Unlock()
	for id, r := range runs.values {
		r.cancel()
		_ = r.server.Close()
		delete(runs.values, id)
	}
}
