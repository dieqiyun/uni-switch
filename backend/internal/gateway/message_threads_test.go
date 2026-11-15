package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A Claude Code Message Threads turn is refused before it is routed, the
// way Claude Code reads as "send it again stateless" (400,
// error.details.error_code thread_unsupported_request), create and
// continue alike. A continue passed on reached the model as its latest
// turn alone, with no history, system or tools (#2480, zane668,
// Vincentzyx). The bodies are #2480's own synthetic ones. A request with
// no thread, or "thread": null, is served as before.
func TestMessageThreadsSentAgainWhole(t *testing.T) {
	fresh(t)
	var mu sync.Mutex
	var asked []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		asked = append(asked, string(b))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "a", Name: "A", Key: "k", Models: []string{"fixture-model"}, Anthropic: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	s := New()
	const create = `{"model":"a/fixture-model","max_tokens":128,"stream":false,"thread":{"type":"create"},` +
		`"system":"虚构测试规则：保留标记 CTX_MARKER_ALPHA。",` +
		`"tools":[{"name":"noop","description":"无副作用的虚构工具","input_schema":{"type":"object","properties":{},"additionalProperties":false}}],` +
		`"messages":[{"role":"user","content":"记住虚构标记 CTX_MARKER_ALPHA。"}]}`
	const continued = `{"model":"a/fixture-model","max_tokens":128,"stream":false,"thread":{"type":"continue","previous_message_id":"msg_1"},` +
		`"messages":[{"role":"user","content":"之前的虚构标记是什么？有哪些可用工具？"}]}`
	for _, path := range []string{"/v1/messages", "/messages"} {
		for name, body := range map[string]string{"create": create, "continue": continued} {
			code, raw := postJSON(t, s, path, body)
			var e struct {
				Type  string `json:"type"`
				Error struct {
					Type    string `json:"type"`
					Details struct {
						ErrorCode string `json:"error_code"`
					} `json:"details"`
				} `json:"error"`
			}
			json.Unmarshal([]byte(raw), &e)
			if code != 400 || e.Type != "error" || e.Error.Type != "invalid_request_error" || e.Error.Details.ErrorCode != "thread_unsupported_request" {
				t.Errorf("%s %s: %d %s, want 400 with error.details.error_code thread_unsupported_request", path, name, code, raw)
			}
		}
	}
	mu.Lock()
	n := len(asked)
	mu.Unlock()
	if n != 0 {
		t.Fatalf("a thread's turn reached the upstream %d times: %q", n, asked)
	}
	// the same turn sent again whole, as Claude Code does after the 400
	whole := strings.Replace(create, `"thread":{"type":"create"},`, ``, 1)
	nulled := strings.Replace(create, `{"type":"create"}`, `null`, 1)
	for name, body := range map[string]string{"no thread": whole, "thread null": nulled} {
		if code, raw := postJSON(t, s, "/v1/messages", body); code != 200 {
			t.Errorf("%s: %d %s, want it served", name, code, raw)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 2 {
		t.Fatalf("upstream asked %d times, want 2", len(asked))
	}
	if !strings.Contains(asked[0], "CTX_MARKER_ALPHA") || !strings.Contains(asked[0], `"noop"`) {
		t.Errorf("the whole turn lost its system or tools upstream: %s", asked[0])
	}
}
