package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Call is built by the Go loopback listener from its applied provider snapshot.
// The pipe fixture uses the same service; credentials never enter arguments.
type Call struct {
	Upstream       Protocol          `json:"upstream,omitempty"`
	Source         Protocol          `json:"source"`
	Operation      string            `json:"operation"`
	BaseURL        string            `json:"baseUrl"`
	APIKey         string            `json:"apiKey"`
	AuthMode       string            `json:"authMode"`
	Token          string            `json:"token"`
	Authorization  string            `json:"authorization"`
	XAPIKey        string            `json:"xApiKey"`
	HasOrigin      bool              `json:"hasOrigin"`
	DefaultModel   string            `json:"defaultModel"`
	Models         []Model           `json:"models"`
	Desktop        bool              `json:"desktop"`
	Body           Object            `json:"body"`
	CacheDirectory string            `json:"cacheDirectory"`
	NativeRelay    bool              `json:"nativeRelay,omitempty"`
	Transport      string            `json:"transport,omitempty"`
	Project        string            `json:"project,omitempty"`
	AccountKind    string            `json:"accountKind,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
}
type Packet struct {
	Type    string            `json:"type"`
	Status  int               `json:"status,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Data    string            `json:"data,omitempty"`
}
type Sink func(Packet) error
type Service struct{ Client *http.Client }

func NewService() *Service {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	t.ResponseHeaderTimeout = 45 * time.Second
	t.TLSHandshakeTimeout = 10 * time.Second
	return &Service{Client: &http.Client{Transport: t, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func endpoint(base string, p Protocol) (string, error) {
	u, e := url.Parse(base)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", invalid("API 地址无效")
	}
	path := strings.TrimRight(u.Path, "/")
	explicit := false
	for _, tail := range []string{"/chat/completions", "/responses", "/messages"} {
		if strings.HasSuffix(path, tail) {
			path = strings.TrimSuffix(path, tail)
			explicit = true
			break
		}
	}
	resource := "/" + string(p)
	if p == Chat {
		resource = "/chat/completions"
	}
	if p == Messages && !strings.HasSuffix(path, "/v1") && !(explicit && strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/messages")) {
		path += "/v1"
	} else if path == "" && !explicit {
		path = "/v1"
	}
	u.Path = path + resource
	return u.String(), nil
}
func equal(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
func (c Call) authorized() bool {
	return !c.HasOrigin && len(c.Token) == 64 && (equal(c.Authorization, "Bearer "+c.Token) || (c.Source == Messages || c.Source == Gemini || c.Source == Chat) && equal(c.XAPIKey, c.Token))
}
func Authorized(c Call) bool { return c.authorized() }
func failure(p Protocol, status int, message string) Object {
	if p == Gemini {
		name := map[int]string{400: "INVALID_ARGUMENT", 401: "UNAUTHENTICATED", 403: "PERMISSION_DENIED", 404: "NOT_FOUND", 408: "DEADLINE_EXCEEDED", 429: "RESOURCE_EXHAUSTED", 500: "INTERNAL", 502: "UNAVAILABLE", 503: "UNAVAILABLE", 504: "DEADLINE_EXCEEDED"}[status]
		if name == "" {
			name = "UNKNOWN"
		}
		return Object{"error": Object{"code": status, "message": message, "status": name}}
	}
	typ := "api_error"
	switch status {
	case 400:
		typ = "invalid_request_error"
	case 401:
		typ = "authentication_error"
	case 403:
		typ = "permission_error"
	case 404:
		typ = "not_found_error"
	case 429:
		typ = "rate_limit_error"
	}
	return Object{"type": "error", "error": Object{"type": typ, "message": message}}
}
func sendJSON(s Sink, status int, v any) error {
	if e := s(Packet{Type: "head", Status: status, Headers: map[string]string{"content-type": "application/json"}}); e != nil {
		return e
	}
	if e := s(Packet{Type: "data", Data: base64.StdEncoding.EncodeToString([]byte(encode(v)))}); e != nil {
		return e
	}
	return s(Packet{Type: "end"})
}
func (s *Service) post(ctx context.Context, c Call, p Protocol, body Object) (*http.Response, error) {
	var u string
	var e error
	if p == Gemini {
		parsed, err := url.Parse(c.BaseURL)
		if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, invalid("Gemini 地址无效")
		}
		action := "generateContent"
		if body["stream"] == true {
			action = "streamGenerateContent"
		}
		if c.Transport == "codeassist" {
			action = "streamGenerateContent"
			parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1internal:" + action
			body = Object{"model": body["model"], "project": c.Project, "request": clone(body)}
			delete(obj(body["request"]), "model")
			delete(obj(body["request"]), "stream")
			if c.AccountKind == "antigravity" {
				body["userAgent"] = "antigravity"
				body["requestType"] = "agent"
				body["requestId"] = uid("agent-")
				first := ""
				for _, turn := range list(field(body, "request", "contents")) {
					if obj(turn)["role"] != "user" {
						continue
					}
					for _, p := range list(obj(turn)["parts"]) {
						if text := str(obj(p)["text"]); text != "" {
							first = text
							break
						}
					}
					break
				}
				if first == "" {
					first = uid("")
				}
				hash := sha256.Sum256([]byte(first))
				obj(body["request"])["sessionId"] = "-" + strconv.FormatUint(binary.BigEndian.Uint64(hash[:8])&0x7fffffffffffffff, 10)
			} else {
				body["user_prompt_id"] = uid("")
			}
		} else {
			path := strings.TrimSuffix(strings.TrimRight(parsed.Path, "/"), "/openai")
			if path == "" || path == "/v1" {
				path = "/v1beta"
			}
			parsed.Path = path + "/models/" + str(body["model"]) + ":" + action
			body = clone(body)
			delete(body, "model")
			delete(body, "stream")
		}
		if action == "streamGenerateContent" {
			parsed.RawQuery = "alt=sse"
		}
		u = parsed.String()
	} else {
		u, e = endpoint(c.BaseURL, p)
	}
	if c.Transport == "chatgpt" {
		body = clone(body)
		body["stream"] = true
		body["store"] = false
		for _, k := range []string{"max_output_tokens", "max_completion_tokens", "temperature", "top_p", "previous_response_id", "user", "safety_identifier", "prompt_cache_options"} {
			delete(body, k)
		}
		if body["instructions"] == nil {
			body["instructions"] = ""
		}
		if items := list(body["input"]); items != nil {
			for _, v := range items {
				it := obj(v)
				delete(it, "id")
			}
		}
	}
	if e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(encode(body)))
	if e != nil {
		return nil, invalid("无法建立上游请求")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("User-Agent", "uni-switch-core/1")
	if c.Transport == "codeassist" {
		req.Header.Del("Accept")
		if c.AccountKind == "antigravity" {
			req.Header.Set("User-Agent", "antigravity/hub/2.9.1 "+runtime.GOOS+"/"+runtime.GOARCH)
		} else {
			platform := runtime.GOOS
			if platform == "windows" {
				platform = "win32"
			}
			arch := runtime.GOARCH
			if arch == "amd64" {
				arch = "x64"
			}
			req.Header.Set("User-Agent", "GeminiCLI/0.61.0/"+str(body["model"])+" ("+platform+"; "+arch+"; terminal)")
		}
	}
	if p == Messages {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	if p == Gemini && c.Transport != "codeassist" {
		req.Header.Set("x-goog-api-key", c.APIKey)
	} else if c.AuthMode == "x-api-key" {
		req.Header.Set("x-api-key", c.APIKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	return s.Client.Do(req)
}
func limited(r io.Reader) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, maxBody+1))
	if e != nil {
		return nil, errors.New("上游响应中断")
	}
	if len(b) > maxBody {
		return nil, errors.New("上游响应超过大小限制")
	}
	return b, nil
}
func (s *Service) Forward(parent context.Context, c Call, sink Sink) error {
	fail := func(status int, message string) error {
		return sendJSON(sink, status, failure(c.Source, status, message))
	}
	if !c.authorized() {
		return fail(401, "本地转换服务认证失败，请重新应用配置")
	}
	if c.Operation == "muse_models" {
		return sendJSON(sink, 200, c.museModels())
	}
	if c.Operation == "cursor_models" {
		return sendJSON(sink, 200, c.cursorModels())
	}
	if c.Operation == "models" {
		return sendJSON(sink, 200, c.modelList())
	}
	if c.Operation == "compact" {
		// Magpie's own routed models reject /responses/compact. Compaction
		// uses a compaction_trigger on /responses instead (codexBackend).
		return fail(400, "转换模型不支持 /responses/compact；请使用 /responses 中的 compaction_trigger")
	}
	if c.NativeRelay && c.Upstream == Responses && c.Transport == "" {
		m, err := c.resolveModel(str(c.Body["model"]))
		if err == nil && field(m.profile(c.BaseURL), "endpoints", "responses") == false && field(m.profile(c.BaseURL), "endpoints", "chatCompletions") != false {
			c.Upstream = Chat
		}
	}
	if c.Source == c.Upstream && (c.NativeRelay || c.Source == Gemini) && c.Operation != "count_tokens" && c.Transport == "" {
		m, err := c.resolveModel(str(c.Body["model"]))
		if err != nil {
			return fail(400, err.Error())
		}
		body := clone(c.Body)
		body["model"] = m.ID
		clientIDs := newMessageIDs(c.Body)
		if c.Source == Messages {
			restoreMessageIDs(body)
		} else if c.Source == Chat {
			repairChatHistory(body)
		}
		response, err := s.post(parent, c, c.Upstream, body)
		if err != nil {
			return fail(502, "无法连接上游服务")
		}
		defer response.Body.Close()
		return relayHTTP(parent, response, sink, c.Upstream, clientIDs)
	}
	r, e := Parse(c.Source, c.Body)
	if e != nil {
		return fail(400, e.Error())
	}
	model, e := c.resolveModel(r.Model)
	if e != nil {
		return fail(400, e.Error())
	}
	r.Model = model.ID
	clientIDs := newMessageIDs(c.Body)
	cache := cacheFor(c, model.ID)
	cache.restore(r)
	if c.Operation == "count_tokens" {
		// Count the parsed content, as gateway.estimate does. A local estimate
		// does not depend on a vendor supporting the Responses endpoint.
		if c.Source == Gemini {
			return sendJSON(sink, 200, Object{"totalTokens": estimate(r)})
		}
		return sendJSON(sink, 200, Object{"input_tokens": estimate(r)})
	}
	to := Messages
	if c.Source == Messages {
		to = Responses
		if len(r.Stop) > 0 || field(model.profile(c.BaseURL), "endpoints", "responses") == false {
			to = Chat
		}
	}
	if c.Upstream != "" {
		if c.Upstream != Messages && c.Upstream != Responses && c.Upstream != Chat && c.Upstream != Gemini {
			return fail(400, "未知上游协议")
		}
		to = c.Upstream
	}
	compacting := prepareCompaction(r)
	body, e := Build(r, to, model, c.BaseURL)
	if e != nil {
		return fail(400, e.Error())
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	response, e := s.post(ctx, c, to, body)
	if e != nil {
		return fail(502, "无法连接上游服务，请检查地址、网络和证书")
	}
	// A missing model, failed authentication or rate limit is not evidence of
	// a wrong API. Only the reviewed endpoint refusal words authorize replay.
	if to == Responses && response.StatusCode >= 400 && response.StatusCode < 500 {
		b, readErr := limited(response.Body)
		response.Body.Close()
		if readErr != nil {
			return fail(502, readErr.Error())
		}
		if wrongEndpoint(response.StatusCode, b) && field(model.profile(c.BaseURL), "endpoints", "chatCompletions") != false {
			to = Chat
			body, e = Build(r, to, model, c.BaseURL)
			if e != nil {
				return fail(400, e.Error())
			}
			response, e = s.post(ctx, c, to, body)
			if e != nil {
				return fail(502, "无法连接 Chat Completions 上游")
			}
		} else {
			response.Body = io.NopCloser(bytes.NewReader(b))
		}
	}
	defer func() { response.Body.Close() }()
	// Only a recognised prefill refusal is retried with Magpie's userLast.
	// Failed requests never switch to an old Rust implementation.
	if response.StatusCode == 400 && r.endsWithAssistant() && r.Source != Messages {
		b, readErr := limited(response.Body)
		if readErr != nil {
			return fail(502, readErr.Error())
		}
		if prefillRejection.Match(b) {
			response.Body.Close()
			r.Turns = append(r.Turns, Turn{"user", []Part{{Kind: "text", Text: r.modelLastAsk()}}})
			body, e = Build(r, to, model, c.BaseURL)
			if e != nil {
				return fail(400, e.Error())
			}
			response, e = s.post(ctx, c, to, body)
			if e != nil {
				return fail(502, "上游续写请求失败")
			}
		} else {
			response.Body = io.NopCloser(bytes.NewReader(b))
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		b, _ := limited(response.Body)
		var v Object
		_ = decode(b, &v)
		message := str(field(v, "error", "message"))
		if message == "" {
			message = "上游拒绝了请求"
		}
		if c.APIKey != "" {
			message = strings.ReplaceAll(message, c.APIKey, "[已隐藏密钥]")
		}
		runes := []rune(message)
		if len(runes) > 800 {
			message = string(runes[:800])
		}
		return fail(response.StatusCode, fmt.Sprintf("上游 HTTP %d：%s", response.StatusCode, message))
	}
	// Reset the read deadline after every body read. Dropping the Rust pipe
	// kills this request process and cancels the owned upstream immediately.
	if !r.Stream || compacting {
		timer := time.AfterFunc(90*time.Second, cancel)
		defer timer.Stop()
		reader := &idleReader{response.Body, timer}
		var result Result
		if strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
			parts := map[int]Part{}
			var order []int
			result.Usage = Object{}
			e = DecodeStream(to, reader, func(ev Event) error {
				switch ev.Kind {
				case "part":
					order = append(order, ev.Index)
				case "part_done":
					parts[ev.Index] = ev.Part
				case "finish":
					result.Stop = ev.Stop
					result.Usage = ev.Usage
				}
				return nil
			})
			for _, i := range order {
				result.Parts = append(result.Parts, parts[i])
			}
		} else {
			b, err := limited(reader)
			if err != nil {
				return fail(502, err.Error())
			}
			var v Object
			if decode(b, &v) != nil {
				return fail(502, "上游没有返回有效 JSON")
			}
			result, e = DecodeResult(to, v)
		}
		if e != nil {
			return fail(502, e.Error())
		}
		if compacting {
			return compactReply(result, c.Source, str(c.Body["model"]), r.Stream, sink)
		}
		cache.renderResult(&result, c.Source, to)
		if c.Source == Messages {
			clientIDs.result(&result)
		}
		return sendJSON(sink, 200, Render(result, c.Source, str(c.Body["model"]), r.Tools))
	}
	if !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		return fail(502, "上游没有返回 SSE 流")
	}
	if e := sink(Packet{Type: "head", Status: 200, Headers: map[string]string{"content-type": "text/event-stream; charset=utf-8", "cache-control": "no-cache", "x-accel-buffering": "no"}}); e != nil {
		return e
	}
	enc := NewEncoder(c.Source, str(c.Body["model"]), r.Tools, func(f Frame) error {
		return sink(Packet{Type: "data", Data: base64.StdEncoding.EncodeToString(frameBytes(f))})
	})
	if e := enc.Start(); e != nil {
		return e
	}
	emit := enc.Accept
	if c.Source == Messages {
		emit = clientIDs.stream(emit)
	}
	if e := s.translateStream(ctx, c, to, r, model, response, cache.stream(c.Source, to, emit)); e != nil {
		if parent.Err() != nil {
			return parent.Err()
		}
		if e := enc.Error(e.Error()); e != nil {
			return e
		}
	}
	return sink(Packet{Type: "end"})
}

type idleReader struct {
	io.Reader
	timer *time.Timer
}

func (r *idleReader) Read(b []byte) (int, error) {
	n, e := r.Reader.Read(b)
	if n > 0 {
		r.timer.Reset(90 * time.Second)
	}
	return n, e
}

// ServePipe is a dedicated, bounded one-request streaming transport. Its stdout
// contains typed NDJSON packets only; it never opens another management port.
func ServePipe(ctx context.Context, in io.Reader, out io.Writer) error {
	b, e := limited(in)
	if e != nil {
		return e
	}
	var c Call
	if decode(b, &c) != nil {
		return invalid("网关管道请求无效")
	}
	enc := json.NewEncoder(out)
	return NewService().Forward(ctx, c, func(p Packet) error { return enc.Encode(p) })
}
