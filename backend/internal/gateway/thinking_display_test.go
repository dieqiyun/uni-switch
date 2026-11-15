package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// displayVendor answers as Claude Opus 4.7 and later, Sonnet 5, Haiku 5.5,
// Fable and Mythos do: thinking.display defaults to "omitted", so a
// thinking block carries text only when "summarized" is asked for; 4.6
// defaults to "summarized" (platform.claude.com/docs/en/build-with-claude/
// thinking, Controlling thinking display).
type displayVendor struct {
	mu   sync.Mutex
	sent []map[string]any
}

func (v *displayVendor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	json.Unmarshal(b, &m)
	v.mu.Lock()
	v.sent = append(v.sent, m)
	v.mu.Unlock()
	model, _ := m["model"].(string)
	th, _ := m["thinking"].(map[string]any)
	thought := ""
	if d, set := th["display"]; d == "summarized" || !set && strings.Contains(model, "4-6") {
		thought = "Weighing the question."
	}
	if m["stream"] == true {
		delta, _ := json.Marshal(thought)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\""+model+"\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"+
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}\n\n"+
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":"+string(delta)+"}}\n\n"+
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig\"}}\n\n"+
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"+
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"+
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"+
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n"+
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n"+
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	content, _ := json.Marshal([]map[string]any{
		{"type": "thinking", "thinking": thought, "signature": "sig"},
		{"type": "text", "text": "ok"},
	})
	io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"`+model+`","content":`+string(content)+
		`,"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
}

func (v *displayVendor) last() map[string]any {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.sent[len(v.sent)-1]
}

// A Chat or Responses client that asks a Claude to reason sees its
// reasoning, as it does from OpenAI's (asked summary "auto") and from the
// Claude subscription (--thinking-display summarized): the request uni-switch
// builds asks thinking.display "summarized", which Claude Opus 4.7 and
// later otherwise leave out.
func TestTranslatedClaudeReasoningIsShown(t *testing.T) {
	fresh(t)
	up := &displayVendor{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	if err := provider.Save(provider.Provider{ID: "anth", Name: "Anth", Key: "k", Anthropic: srv.URL,
		Models: []string{"claude-opus-5-5", "claude-opus-4-7", "claude-fable-5-1", "claude-sonnet-4-6"}}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ model, path, body string }{
		{"claude-opus-5-5", "/v1/chat/completions", `"reasoning_effort":"high","messages":[{"role":"user","content":"hi"}]`},
		{"claude-opus-4-7", "/v1/chat/completions", `"reasoning_effort":"low","messages":[{"role":"user","content":"hi"}]`},
		{"claude-fable-5-1", "/v1/chat/completions", `"reasoning_effort":"medium","messages":[{"role":"user","content":"hi"}]`},
		{"claude-sonnet-4-6", "/v1/chat/completions", `"reasoning_effort":"high","messages":[{"role":"user","content":"hi"}]`},
		{"claude-opus-5-5", "/v1/responses", `"reasoning":{"effort":"high","summary":"auto"},"input":"hi"`},
	} {
		t.Run(c.model+c.path, func(t *testing.T) {
			code, out := post(t, c.path, `{"model":"anth/`+c.model+`",`+c.body+`}`)
			if code != 200 {
				t.Fatalf("status %d: %s", code, out)
			}
			if th, _ := up.last()["thinking"].(map[string]any); th["type"] != "adaptive" || th["display"] != "summarized" {
				t.Errorf("thinking = %v, want adaptive, display summarized", up.last()["thinking"])
			}
			if !strings.Contains(out, "Weighing the question.") {
				t.Errorf("the client got no reasoning: %s", out)
			}
		})
	}
}

// A relay that knows no thinking.display still answers a Chat client: it
// is asked again without it, as an Anthropic client's display is.
func TestTranslatedDisplayLeftOutWhereRefused(t *testing.T) {
	fresh(t)
	var mu sync.Mutex
	var thinking []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		json.Unmarshal(b, &m)
		th, _ := json.Marshal(m["thinking"])
		mu.Lock()
		thinking = append(thinking, string(th))
		mu.Unlock()
		if strings.Contains(string(th), "display") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"thinking.adaptive.display: Extra inputs are not permitted"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-opus-5-5\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"+
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"+
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"+
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"+
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n"+
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	t.Cleanup(srv.Close)
	if err := provider.Save(provider.Provider{ID: "strict", Name: "Strict", Key: "k", Anthropic: srv.URL,
		Models: []string{"claude-opus-5-5"}}); err != nil {
		t.Fatal(err)
	}
	h := New().Handler() // one gateway, which learns
	for i := range 2 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions",
			strings.NewReader(`{"model":"strict/claude-opus-5-5","reasoning_effort":"high","messages":[{"role":"user","content":"hi"}]}`)))
		if out := rec.Body.String(); rec.Code != 200 || !strings.Contains(out, `"ok"`) {
			t.Fatalf("request %d: status %d: %s", i, rec.Code, out)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	// the first asks with display and again without; the second, learned, without
	if want := []string{`{"display":"summarized","type":"adaptive"}`, `{"type":"adaptive"}`, `{"type":"adaptive"}`}; !slices.Equal(thinking, want) {
		t.Errorf("thinking sent = %v, want %v", thinking, want)
	}
}

// A budget turned into adaptive thinking for Opus 4.7 and later (an
// Anthropic client's own, or one uni-switch built for a model it knows by
// another name) still shows the thinking, as enabled thinking does by
// default; a provider that refuses display is asked again without it and
// from then on sent none (#1485).
func TestBudgetTurnedAdaptiveIsShown(t *testing.T) {
	fresh(t)
	up := &displayVendor{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	if err := provider.Save(provider.Provider{ID: "anth", Name: "Anth", Key: "k", Anthropic: srv.URL,
		Models: []string{"claude-opus-5-5", "opus"}}); err != nil {
		t.Fatal(err)
	}
	// uni-switch's "opus" is the vendor's claude-opus-4-7
	if err := provider.SetUpstreamName("anth/opus", "claude-opus-4-7"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, path, body string }{
		{"budget relayed", "/v1/messages",
			`{"model":"anth/claude-opus-5-5","max_tokens":32000,"thinking":{"type":"enabled","budget_tokens":10000},"messages":[{"role":"user","content":"hi"}]}`},
		{"budget relayed, streamed", "/v1/messages",
			`{"model":"anth/claude-opus-5-5","max_tokens":32000,"stream":true,"thinking":{"type":"enabled","budget_tokens":10000},"messages":[{"role":"user","content":"hi"}]}`},
		{"chat's effort, name mapped after", "/v1/chat/completions",
			`{"model":"anth/opus","reasoning_effort":"low","messages":[{"role":"user","content":"hi"}]}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			code, out := post(t, c.path, c.body)
			if code != 200 {
				t.Fatalf("status %d: %s", code, out)
			}
			if th, _ := up.last()["thinking"].(map[string]any); th["type"] != "adaptive" || th["display"] != "summarized" {
				t.Errorf("thinking = %v, want adaptive, display summarized", up.last()["thinking"])
			}
			if !strings.Contains(out, "Weighing the question.") {
				t.Errorf("the client got no thinking: %s", out)
			}
		})
	}

	var mu sync.Mutex
	var thinking []string
	strict := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		th, _ := json.Marshal(gjsonThinking(b))
		mu.Lock()
		thinking = append(thinking, string(th))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(th), "display") {
			w.WriteHeader(400)
			io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"thinking.adaptive.display: Extra inputs are not permitted"}}`)
			return
		}
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	t.Cleanup(strict.Close)
	if err := provider.Save(provider.Provider{ID: "strict", Name: "Strict", Key: "k", Anthropic: strict.URL,
		Models: []string{"claude-opus-5-5"}}); err != nil {
		t.Fatal(err)
	}
	h := New().Handler() // one gateway, which learns
	for i := range 2 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
			`{"model":"strict/claude-opus-5-5","max_tokens":32000,"thinking":{"type":"enabled","budget_tokens":10000},"messages":[{"role":"user","content":"hi"}]}`)))
		if out := rec.Body.String(); rec.Code != 200 || !strings.Contains(out, `"ok"`) {
			t.Fatalf("request %d: status %d: %s", i, rec.Code, out)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if want := []string{`{"display":"summarized","type":"adaptive"}`, `{"type":"adaptive"}`, `{"type":"adaptive"}`}; !slices.Equal(thinking, want) {
		t.Errorf("thinking sent = %v, want %v", thinking, want)
	}
}

func gjsonThinking(body []byte) any {
	var m map[string]any
	json.Unmarshal(body, &m)
	return m["thinking"]
}
