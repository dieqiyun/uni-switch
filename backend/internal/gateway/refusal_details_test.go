package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Anthropic's refusal, its stop_details as the docs give them (#1768): the
// category and explanation the classifier gave, in message_delta's delta.
var anthropicRefusalCyber = sse(`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"m1","role":"assistant","model":"m","content":[],"usage":{"input_tokens":3}}}`,
	`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"refusal","stop_sequence":null,"stop_details":{"type":"refusal","category":"cyber","explanation":"This request was declined because it could enable cyber harm."}},"usage":{"output_tokens":0}}`,
	`event: message_stop`+"\n"+`data: {"type":"message_stop"}`)

// #1768: a refusal's note keeps the category and explanation Anthropic's
// classifier gave, streamed or not, and invents none when it gave none.
func TestRefusalKeepsStopDetails(t *testing.T) {
	for _, c := range []struct {
		name, ev, want string
	}{
		{"category and explanation", `event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber","explanation":"This request was declined because it could enable cyber harm."}}}`,
			"safety filter: refusal (category cyber: This request was declined because it could enable cyber harm.)"},
		{"a category unknown today", `data: {"type":"message_delta","delta":{"stop_reason":"refusal","stop_details":{"type":"refusal","category":"frontier_llm_v2","explanation":null}}}`,
			"safety filter: refusal (category frontier_llm_v2)"},
		{"nulls", `data: {"type":"message_delta","delta":{"stop_reason":"refusal","stop_details":{"type":"refusal","category":null,"explanation":null}}}`,
			"safety filter: refusal"},
		{"none", `data: {"type":"message_delta","delta":{"stop_reason":"refusal","stop_sequence":null}}`,
			"safety filter: refusal"},
	} {
		kind, status, msg := streamEvent([]byte(c.ev))
		if kind != eventRefusal || status != refusedStatus || msg != c.want {
			t.Errorf("%s: %d %d %q, want %q", c.name, kind, status, msg, c.want)
		}
	}
	whole := `{"id":"m1","type":"message","role":"assistant","content":[],"stop_reason":"refusal","stop_details":{"type":"refusal","category":"bio","explanation":null}}`
	if msg, ok := refusedReply([]byte(whole)); !ok || msg != "safety filter: refusal (category bio)" {
		t.Errorf("whole reply: %v %q", ok, msg)
	}
	if msg, ok := refusedReply([]byte(`{"content":[],"stop_reason":"refusal"}`)); !ok || msg != "safety filter: refusal" {
		t.Errorf("whole reply without details: %v %q", ok, msg)
	}
	// an explanation is the vendor's own words, kept to a line
	long := `data: {"type":"message_delta","delta":{"stop_reason":"refusal","stop_details":{"category":"general_harms","explanation":"` + strings.Repeat("x", 1000) + `"}}}`
	if _, _, msg := streamEvent([]byte(long)); len(msg) > 400 || !strings.Contains(msg, "category general_harms: xxx") {
		t.Errorf("long explanation: %d %q", len(msg), msg)
	}
}

// The category reaches the Routing page's try that was refused, though the
// next key answered (#1768: a refusal followed by a fallback was hidden).
func TestRefusalCategoryOnTheTry(t *testing.T) {
	fresh(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		if strings.Contains(r.Header.Get("x-api-key")+r.Header.Get("Authorization"), "k1") {
			io.WriteString(w, anthropicRefusalCyber)
			return
		}
		io.WriteString(w, anthropicAnswer)
	}))
	defer up.Close()
	if err := provider.Save(provider.Provider{ID: "a", Name: "A", Models: []string{"m"}, Anthropic: up.URL, Routing: provider.Ordered,
		Key: "k1", Keys: []provider.KeyAccount{{Key: "k2"}}}); err != nil {
		t.Fatal(err)
	}
	s := New()
	code, body := sendTo(s, "/v1/messages", `{"model":"a/m","stream":true,"max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 || !strings.Contains(body, "from b") {
		t.Fatalf("status %d: %s", code, body)
	}
	r := lastRoute(s)
	if len(r.Tries) != 2 || r.Tries[0].Fail != failRefused || !strings.Contains(r.Tries[0].Error, "category cyber") {
		t.Fatalf("tries: %+v", r.Tries)
	}
}
