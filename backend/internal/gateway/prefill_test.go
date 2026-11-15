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

// The refusals of #1447: OpenCode Go's Claude models over Anthropic
// Messages, and Volcengine's Agent Plan over Responses and Chat. The
// messages are the report's, word for word; the envelopes are each
// vendor's error shape (OpenCode Go's as its 403 for a lapsed plan reads).
const (
	openCodeGoPrefillRefusal = `{"type":"error","error":{"type":"api_error","message":"Upstream request failed: [invalid_request_error] This model does not support assistant message prefill. The conversation must end with a user message."}}`
	volcenginePrefillRefusal = `{"error":{"code":"InvalidParameter","message":"The parameter input[194].role specified in the request are not valid: The last message cannot be from the assistant for a model that does not support prefill","param":"input[194].role","type":"BadRequest"}}`
)

// noPrefill is an upstream whose model refuses a conversation ending with
// the assistant's message, as #1447's do, and answers "OK" to one ending
// with the user's. proto is the API it speaks; cut, when set, severs the
// first reply after "hel" (Anthropic only). Bodies journals each call.
type noPrefill struct {
	proto  provider.Protocol
	refuse string
	cut    bool
	mu     sync.Mutex
	bodies []map[string]any
}

func (f *noPrefill) serve(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	b, _ := io.ReadAll(r.Body)
	json.Unmarshal(b, &body)
	f.mu.Lock()
	call := len(f.bodies)
	f.bodies = append(f.bodies, body)
	f.mu.Unlock()
	field := "messages"
	if f.proto == provider.Responses {
		field = "input"
	}
	items, _ := body[field].([]any)
	last, _ := items[len(items)-1].(map[string]any)
	if last["role"] == "assistant" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		io.WriteString(w, f.refuse)
		return
	}
	switch f.proto {
	case provider.Anthropic:
		if f.cut && call == 0 {
			writeStream(w, []string{anthStart("m", 7), anthText("hel")}, true, "")
			return
		}
		writeStream(w, []string{anthStart("m", 7), anthText("OK"), anthStop(1)}, false, "")
	case provider.Chat:
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"c1","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":9,"completion_tokens":1}}`)
	case provider.Responses:
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"resp_1","object":"response","status":"completed","model":"m","output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"OK","annotations":[]}]}],"usage":{"input_tokens":9,"output_tokens":1,"total_tokens":10}}`)
	}
}

func (f *noPrefill) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.bodies)
}

// lastUser is the text of call's last message when it is the user's, ""
// when it isn't.
func (f *noPrefill) lastUser(call int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	field := "messages"
	if f.proto == provider.Responses {
		field = "input"
	}
	items, _ := f.bodies[call][field].([]any)
	last, _ := items[len(items)-1].(map[string]any)
	if last["role"] != "user" {
		return ""
	}
	if s, ok := last["content"].(string); ok {
		return s
	}
	var sb strings.Builder
	parts, _ := last["content"].([]any)
	for _, p := range parts {
		pm, _ := p.(map[string]any)
		s, _ := pm["text"].(string)
		sb.WriteString(s)
	}
	return sb.String()
}

// prefillServer saves a provider speaking only f's API and serves its
// requests with one gateway, which remembers what it learns across them.
func prefillServer(t *testing.T, f *noPrefill) func(path, body string) (int, string) {
	t.Helper()
	fresh(t)
	up := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(up.Close)
	p := provider.Provider{ID: "up", Name: "UP", Key: "k", Models: []string{"m", "other"}}
	switch f.proto {
	case provider.Anthropic:
		p.Anthropic = up.URL
	case provider.Chat:
		p.Chat = up.URL + "/v1"
	case provider.Responses:
		p.Responses = up.URL + "/v1"
	}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	h := New().Handler()
	return func(path, body string) (int, string) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
		return rec.Code, rec.Body.String()
	}
}

// #1447's repro: a Responses request ending with the assistant's message,
// to a model served over Anthropic Messages that takes no prefill, is
// answered after it, with a user turn the model takes, rather than ending
// with the refusal; and the next such request goes so at once.
func TestTrailingAssistantToNoPrefillModel(t *testing.T) {
	f := &noPrefill{proto: provider.Anthropic, refuse: openCodeGoPrefillRefusal}
	post := prefillServer(t, f)
	ask := `{"model":"up/m","stream":false,"input":[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"Reply with exactly: OK"}]},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"O"}]}]}`
	code, body := post("/v1/responses", ask)
	if code != 200 || !strings.Contains(body, `"OK"`) {
		t.Fatalf("status %d, want the model's answer: %s", code, body)
	}
	if f.calls() != 2 || f.lastUser(1) != continueText {
		t.Fatalf("calls %d, the retry's last user turn %q: want it asked again with a user turn after the assistant's", f.calls(), f.lastUser(1))
	}
	// remembered: asked so at once, streamed and on Chat as well
	code, body = post("/v1/chat/completions", `{"model":"up/m","stream":true,"messages":[
		{"role":"user","content":"Reply with exactly: OK"},{"role":"assistant","content":"O"}]}`)
	if code != 200 || chatTextOf(body) != "OK" {
		t.Fatalf("status %d, want the model's answer: %s", code, body)
	}
	if f.calls() != 3 || f.lastUser(2) != continueText {
		t.Fatalf("calls %d: the model that refused a prefill was asked one again", f.calls())
	}
	// only that model: another of the provider's still gets the
	// conversation as it was
	post("/v1/chat/completions", `{"model":"up/other","messages":[
		{"role":"user","content":"hi"},{"role":"assistant","content":"O"}]}`)
	if f.lastUser(3) != "" {
		t.Fatalf("another model of the provider lost its prefill: %v", f.bodies[3])
	}
}

// An Anthropic client's prefill is its own ask: the refusal reaches it as
// the vendor said it, and nothing is asked in its place.
func TestAnthropicPrefillToNoPrefillModel(t *testing.T) {
	f := &noPrefill{proto: provider.Anthropic, refuse: openCodeGoPrefillRefusal}
	post := prefillServer(t, f)
	code, body := post("/v1/messages", `{"model":"up/m","max_tokens":100,"messages":[
		{"role":"user","content":"Reply with exactly: OK"},{"role":"assistant","content":"O"}]}`)
	if code != 400 || !strings.Contains(body, "does not support assistant message prefill") {
		t.Fatalf("status %d, want the refusal: %s", code, body)
	}
	if f.calls() != 1 {
		t.Fatalf("calls = %d, want the client's own request alone", f.calls())
	}
}

// Volcengine's Agent Plan refuses Codex's resumed reply on Responses,
// relayed as it is (#1447): asked again with a user turn after it, and so
// from then on. Chat's relay likewise.
func TestTrailingAssistantRelayedToNoPrefillModel(t *testing.T) {
	for _, c := range []struct {
		proto      provider.Protocol
		path, body string
	}{
		{provider.Responses, "/v1/responses", `{"model":"up/m","input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Reply with exactly: OK"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"O"}]}]}`},
		{provider.Chat, "/v1/chat/completions", `{"model":"up/m","messages":[
			{"role":"user","content":"Reply with exactly: OK"},{"role":"assistant","content":"O"}]}`},
	} {
		t.Run(string(c.proto), func(t *testing.T) {
			f := &noPrefill{proto: c.proto, refuse: volcenginePrefillRefusal}
			post := prefillServer(t, f)
			code, body := post(c.path, c.body)
			if code != 200 || !strings.Contains(body, `"OK"`) {
				t.Fatalf("status %d, want the model's answer: %s", code, body)
			}
			if f.calls() != 2 || f.lastUser(1) != continueText {
				t.Fatalf("calls %d, retry's last user turn %q", f.calls(), f.lastUser(1))
			}
			if code, _ := post(c.path, c.body); code != 200 || f.calls() != 3 || f.lastUser(2) != continueText {
				t.Fatalf("status %d, calls %d: the refusal wasn't remembered", code, f.calls())
			}
		})
	}
}

// uni-switch's own continuation of a cut reply, to a model that takes no
// prefill, ends with what cut it, not with the refusal of an ask the
// client never made; the next cut on that model isn't asked to go on.
func TestStreamCutOnNoPrefillModel(t *testing.T) {
	f := &noPrefill{proto: provider.Anthropic, refuse: openCodeGoPrefillRefusal, cut: true}
	post := prefillServer(t, f)
	ask := `{"model":"up/m","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	code, body := post("/v1/chat/completions", ask)
	if code != 200 || f.calls() != 2 {
		t.Fatalf("status %d, calls %d, want the cut asked to go on once: %s", code, f.calls(), body)
	}
	if got := chatTextOf(body); got != "hel" {
		t.Fatalf("client's text = %q, want %q", got, "hel")
	}
	if strings.Contains(body, "prefill") || !strings.Contains(body, "UP") {
		t.Fatalf("the reply didn't end with what cut it: %s", body)
	}
	f.cut, f.bodies = true, nil
	post("/v1/chat/completions", ask)
	if f.calls() != 1 {
		t.Fatalf("calls = %d: a model that refused the prefill was asked to go on again", f.calls())
	}
}

// userLastBody adds the user turn only after the assistant's message, not
// after a call or a tool's output.
func TestUserLastBody(t *testing.T) {
	for _, c := range []struct {
		chat bool
		body string
		want bool
	}{
		{true, `{"messages":[{"role":"user","content":"a"},{"role":"assistant","content":"b"}]}`, true},
		{true, `{"messages":[{"role":"user","content":"a"}]}`, false},
		{true, `{"messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"c"}]}]}`, false},
		{false, `{"input":[{"role":"assistant","content":"b"}]}`, true},
		{false, `{"input":[{"type":"function_call","call_id":"c","name":"x","arguments":"{}"}]}`, false},
		{false, `{"input":"hi"}`, false},
	} {
		out, ok := userLastBody(c.chat, []byte(c.body))
		if ok != c.want {
			t.Errorf("userLastBody(%s) = %v, want %v", c.body, ok, c.want)
		}
		if ok && !strings.Contains(string(out), continueText) {
			t.Errorf("userLastBody(%s) = %s, no user turn", c.body, out)
		}
	}
}
