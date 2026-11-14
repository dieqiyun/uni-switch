package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// codexEffortChanged is a turn as Codex 0.162.1 sends it with its
// reasoning_effort_override feature on, after the thread, started at
// medium, was switched to xhigh (/model, Extra high, "s"): the top stays
// at medium and the change goes as a configuration_update after the
// user's message (captured from the real binary, its long developer
// messages and metadata left out; zzzkl3994 on Discord).
const codexEffortChanged = `{"model":"%s","stream":true,"tool_choice":"auto","parallel_tool_calls":false,"reasoning":{"effort":"medium","context":"all_turns"},"store":false,"include":["reasoning.encrypted_content"],"input":[` +
	`{"type":"message","role":"user","content":[{"type":"input_text","text":"say ok"}]},` +
	`{"type":"configuration_update","reasoning":{"effort":"medium"}},` +
	`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]},` +
	`{"type":"message","role":"user","content":[{"type":"input_text","text":"again"}]},` +
	`{"type":"configuration_update","reasoning":{"effort":"xhigh"}}]}`

func codexTurnAt(model string) string {
	return strings.Replace(codexEffortChanged, "%s", model, 1)
}

// The effort a thread was switched to is the one Routing shows, and the
// one a Responses vendor that doesn't read the items is asked: it was
// shown, and sent, at the thread's first.
func TestCodexEffortChangeShownAndSent(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"type":"response.created","response":{"id":"r1","model":"m1"}}`,
		`data: {"type":"response.completed","response":{"id":"r1","output":[],"usage":{"input_tokens":5,"output_tokens":2}}}`)}
	setup(t, provider.Responses, f)
	s := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(codexTurnAt("fake/m1")))
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	r := lastRoute(s)
	if r.Effort != "xhigh" || len(r.Tries) != 1 || r.Tries[0].Effort != "xhigh" {
		t.Errorf("route at %q, tries %+v; want xhigh", r.Effort, r.Tries)
	}
	var got struct {
		Reasoning map[string]any   `json:"reasoning"`
		Input     []map[string]any `json:"input"`
	}
	json.Unmarshal(f.got, &got)
	if got.Reasoning["effort"] != "xhigh" {
		t.Errorf("sent reasoning %v, want xhigh", got.Reasoning)
	}
	for _, it := range got.Input {
		if it["type"] == "configuration_update" {
			t.Errorf("an update reached the vendor: %s", f.got)
		}
	}
}

// Translated to Chat, the turn is asked at the effort it was switched to.
func TestCodexEffortChangeReachesChatVendor(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"id":"c1","model":"m1","choices":[{"delta":{"role":"assistant","content":"hi"}}]}`,
		`data: {"id":"c1","choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`)}
	setup(t, provider.Chat, f)
	if code, body := post(t, "/v1/responses", codexTurnAt("fake/m1")); code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	var got map[string]any
	json.Unmarshal(f.got, &got)
	if got["reasoning_effort"] != "xhigh" {
		t.Errorf("sent %s", f.got)
	}
}

// What reads and rewrites a Responses request's effort reads and rewrites
// the last update; OpenAI's own API and the ChatGPT backend get the items
// as Codex sent them.
func TestCodexEffortChangeReaders(t *testing.T) {
	body := []byte(codexTurnAt("gpt-6.1-sol"))
	if e := requestEffort(provider.Responses, body); e != "xhigh" {
		t.Errorf("requestEffort %q", e)
	}
	if e := requestEffort(provider.Responses, []byte(`{"reasoning":{"effort":"low"},"input":[{"type":"message","role":"user","content":"configuration_update"}]}`)); e != "low" {
		t.Errorf("a message naming the item: %q", e)
	}
	if q, err := parseResponses(body); err != nil || q.Effort != "xhigh" {
		t.Errorf("parsed at %q (%v)", q.Effort, err)
	}
	for _, p := range []provider.Provider{
		{Responses: "https://api.openai.com/v1"},
		{Responses: "https://chatgpt.com/backend-api/codex", Account: &provider.Account{Agent: "codex"}},
	} {
		if got := forVendor(p, body, false); string(got) != string(body) {
			t.Errorf("%s: %s", p.Responses, got)
		}
	}

	// uni-switch's own pick goes where the last change went; the top stays
	picked := withEffort(provider.Responses, body, "high")
	var q struct {
		Reasoning map[string]any   `json:"reasoning"`
		Input     []map[string]any `json:"input"`
	}
	json.Unmarshal(picked, &q)
	last, _ := q.Input[len(q.Input)-1]["reasoning"].(map[string]any)
	first, _ := q.Input[1]["reasoning"].(map[string]any)
	if q.Reasoning["effort"] != "medium" || last["effort"] != "high" || first["effort"] != "medium" || bodyEffort(provider.Responses, picked) != "high" {
		t.Errorf("picked high: %s", picked)
	}
	fixed := withFixedEffort(provider.Responses, body, "low")
	if e := requestEffort(provider.Responses, fixed); e != "low" {
		t.Errorf("fixed at low reads %q: %s", e, fixed)
	}
	if b := withoutAnyReasoning(provider.Responses, body); strings.Contains(string(b), "configuration_update") || strings.Contains(string(b), `"reasoning"`) {
		t.Errorf("reasonless: %s", b)
	}
}
