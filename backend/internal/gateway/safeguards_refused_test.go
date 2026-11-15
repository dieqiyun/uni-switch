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

// #1756: Claude Code in auto mode through uni-switch to Copilot's
// claude-opus-5.5 got 400 invalid_request_error "safeguards: Extra inputs
// are not permitted" on every turn. uni-switch asks again at once without the
// safeguards, and sends that provider none from then on; the rest of the
// request goes as it was.
func TestSafeguardsRefusedLeftOut(t *testing.T) {
	fresh(t)
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		json.Unmarshal(b, &m)
		mu.Lock()
		bodies = append(bodies, m)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if _, ok := m["safeguards"]; ok {
			w.WriteHeader(400)
			io.WriteString(w, `{"error":{"message":"safeguards: Extra inputs are not permitted","type":"invalid_request_error"}}`)
			return
		}
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5.5","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	t.Cleanup(srv.Close)
	if err := provider.Save(provider.Provider{ID: "cop", Name: "Cop", Key: "k", Anthropic: srv.URL,
		Models: []string{"claude-opus-5.5"}}); err != nil {
		t.Fatal(err)
	}
	body := `{"model":"cop/claude-opus-5.5","max_tokens":64,"safeguards":{"dangerous_tool_use":{"enabled":true}},` +
		`"messages":[{"role":"user","content":[{"type":"text","text":"say pong"}]}]}`
	h := New().Handler() // one gateway, which learns
	post := func() {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
	}
	all := func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), bodies...)
	}
	post()
	sent := all()
	if len(sent) != 2 {
		t.Fatalf("asked %d times, want again once after the 400", len(sent))
	}
	if _, ok := sent[1]["safeguards"]; ok {
		t.Fatalf("asked again with the safeguards: %v", sent[1])
	}
	if msgs, _ := sent[1]["messages"].([]any); len(msgs) != 1 {
		t.Fatalf("messages = %v", sent[1]["messages"])
	}

	// learned: the next request goes without them the first time
	post()
	if sent = all(); len(sent) != 3 {
		t.Fatalf("asked %d times in all, want once more", len(sent))
	}
	if _, ok := sent[2]["safeguards"]; ok {
		t.Fatalf("sent the safeguards again: %v", sent[2])
	}
}

// A provider that takes the safeguards (Anthropic's own API) keeps getting
// them: only a refusal naming them leaves them out.
func TestSafeguardsKeptWhereTaken(t *testing.T) {
	fresh(t)
	var got []map[string]any
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		json.Unmarshal(b, &m)
		mu.Lock()
		got = append(got, m)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	t.Cleanup(srv.Close)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Anthropic: srv.URL,
		Models: []string{"claude-opus-5-5"}}); err != nil {
		t.Fatal(err)
	}
	body := `{"model":"relay/claude-opus-5-5","max_tokens":64,"safeguards":{"dangerous_tool_use":{"enabled":true}},` +
		`"messages":[{"role":"user","content":"hi"}]}`
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("asked %d times", len(got))
	}
	if _, ok := got[0]["safeguards"]; !ok {
		t.Fatalf("safeguards left out where taken: %v", got[0])
	}
}
