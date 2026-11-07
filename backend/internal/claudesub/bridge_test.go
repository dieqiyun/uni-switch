package claudesub

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("UNI_SWITCH_BRIDGE_HELPER") == "1" {
		helper()
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func helper() {
	if os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("CLAUDE_CONFIG_DIR") == "" {
		os.Exit(8)
	}
	config := ""
	for i, a := range os.Args {
		if a == "--mcp-config" && i+1 < len(os.Args) {
			config = os.Args[i+1]
		}
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(config), &m)
	server := obj(obj(m["mcpServers"])["uni_switch"])
	address := str(server["url"])
	token := str(obj(server["headers"])["Authorization"])
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		os.Exit(9)
	}
	var input map[string]any
	_ = json.Unmarshal(sc.Bytes(), &input)
	send := func(v any) { b, _ := json.Marshal(v); fmt.Println(string(b)) }
	stream := func(kind string, more map[string]any) {
		event := map[string]any{"type": kind, "index": 0}
		for k, v := range more {
			event[k] = v
		}
		send(map[string]any{"type": "stream_event", "event": event})
	}
	stream("content_block_start", map[string]any{"content_block": map[string]any{"type": "text", "text": ""}})
	stream("content_block_delta", map[string]any{"delta": map[string]any{"type": "text_delta", "text": "before tools"}})
	stream("content_block_stop", nil)
	send(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": "cli-one", "name": "mcp__uni_switch__tool_0", "input": map[string]any{"x": 1}}, map[string]any{"type": "tool_use", "id": "cli-two", "name": "mcp__uni_switch__tool_0", "input": map[string]any{"x": 2}}}}})
	var wg sync.WaitGroup
	for i := 1; i <= 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": i, "method": "tools/call", "params": map[string]any{"name": "tool_0", "arguments": map[string]any{"x": i}}})
			req, _ := http.NewRequest("POST", address, bytes.NewReader(body))
			req.Header.Set("Authorization", token)
			res, e := http.DefaultClient.Do(req)
			if e != nil {
				os.Exit(10)
			}
			raw, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if !strings.Contains(string(raw), fmt.Sprintf("result-%d", i)) {
				os.Exit(11)
			}
		}(i)
	}
	wg.Wait()
	stream("content_block_start", map[string]any{"content_block": map[string]any{"type": "text", "text": ""}})
	stream("content_block_delta", map[string]any{"delta": map[string]any{"type": "text_delta", "text": "after tools"}})
	stream("content_block_stop", nil)
	send(map[string]any{"type": "result", "is_error": false, "usage": map[string]any{"input_tokens": 3, "output_tokens": 2}})
}
func collect(t *testing.T, c gateway.Call) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	status := 0
	var out bytes.Buffer
	e := Forward(ctx, c, func(p gateway.Packet) error {
		if p.Type == "head" {
			status = p.Status
		}
		if p.Type == "data" {
			b, _ := base64.StdEncoding.DecodeString(p.Data)
			out.Write(b)
		}
		return nil
	})
	if e != nil || status != 200 {
		t.Fatal(e, status, out.String())
	}
	var body map[string]any
	if e = json.Unmarshal(out.Bytes(), &body); e != nil {
		t.Fatal(e, out.String())
	}
	return body
}
func TestClaudeCLIParallelToolsReturnToCallerAndContinue(t *testing.T) {
	t.Setenv("UNI_SWITCH_BRIDGE_HELPER", "1")
	t.Setenv("ANTHROPIC_API_KEY", "synthetic-client-key-must-not-use")
	exe, _ := os.Executable()
	t.Setenv("UNI_SWITCH_CLAUDE_BINARY", exe)
	defer CloseAll()
	c := gateway.Call{Source: gateway.Responses, APIKey: "synthetic-owner", BaseURL: t.TempDir(), Body: gateway.Object{"model": "claude-test", "input": "hello", "tools": []any{gateway.Object{"type": "function", "name": "f", "parameters": gateway.Object{"type": "object"}}}}}
	out := collect(t, c)
	calls := []map[string]any{}
	for _, v := range array(out["output"]) {
		it := obj(v)
		if it["type"] == "function_call" {
			calls = append(calls, it)
		}
	}
	if len(calls) != 2 || calls[0]["call_id"] == calls[1]["call_id"] {
		t.Fatal(out)
	}
	input := []any{}
	for _, call := range calls {
		input = append(input, call)
	}
	for _, call := range calls {
		var args map[string]any
		_ = json.Unmarshal([]byte(str(call["arguments"])), &args)
		input = append(input, gateway.Object{"type": "function_call_output", "call_id": call["call_id"], "output": fmt.Sprintf("result-%d", number(args["x"]))})
	}
	c.Body["input"] = input
	out = collect(t, c)
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), "after tools") || strings.Contains(string(b), "before tools") {
		t.Fatal(string(b))
	}
}
func TestMCPRejectsMissingAuthentication(t *testing.T) {
	r := &run{token: "synthetic"}
	req, _ := http.NewRequest("POST", "http://127.0.0.1/mcp", strings.NewReader(`{"method":"tools/call"}`))
	w := &testWriter{header: http.Header{}}
	r.mcp(w, req)
	if w.status != 401 {
		t.Fatal(w.status)
	}
}

type testWriter struct {
	header http.Header
	status int
}

func (w *testWriter) Header() http.Header         { return w.header }
func (w *testWriter) WriteHeader(n int)           { w.status = n }
func (w *testWriter) Write(b []byte) (int, error) { return len(b), nil }
