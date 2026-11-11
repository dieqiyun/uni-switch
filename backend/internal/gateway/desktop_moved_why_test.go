package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Neo on Discord: a Routing › Requests row read "chatflowsai/claude-opus-5-5"
// asked and AgentRouter's gpt-6.1-sol served, with nothing saying why, and
// read as Claude Desktop asking for another provider's model by itself. It
// was Desktop's own short request (a chat's title), which Desktop names by
// the only Claude-looking id it is shown and uni-switch keeps on the chat's
// model. The trace says so: Route.Moved names the model served and the rule
// that moved it, and a request served as asked has none.
func TestClaudeDesktopMoveSaysWhy(t *testing.T) {
	f := &fake{reply: sse(
		`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"m","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`,
		`event: message_stop`+"\n"+`data: {"type":"message_stop"}`)}
	up := setup(t, provider.Anthropic, f)
	for _, p := range []provider.Provider{
		{ID: "agentrouter", Name: "AgentRouter", Key: "k", Anthropic: up.URL, Models: []string{"gpt-6.1-sol"}},
		{ID: "chatflowsai", Name: "ChatFlowsAI", Key: "k", Anthropic: up.URL, Models: []string{"claude-opus-5-5"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	s := New()
	h := s.Handler()
	send := func(body string) *Route {
		t.Helper()
		f.got = nil
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
		req.Header.Set("x-api-key", Token+"-claude-desktop")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		s.trace.mu.Lock()
		defer s.trace.mu.Unlock()
		return s.trace.routes[len(s.trace.routes)-1]
	}

	// the chat turn, on AgentRouter's GPT: served as asked, nothing moved
	r := send(`{"model":"agentrouter/gpt-6.1-sol","max_tokens":32000,"stream":true,"tools":[{"name":"Bash","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"hi"}]}`)
	if r.Moved != nil {
		t.Fatalf("a turn served as asked says moved: %+v", r.Moved)
	}
	// Desktop's title, on the opus it picks by itself: served on the chat's
	// model, and the row says so
	r = send(`{"model":"chatflowsai/claude-opus-5-5","max_tokens":200,"stream":true,"system":"You write short session titles.","messages":[{"role":"user","content":"hi"}]}`)
	if got := modelOf(f.got); got != "gpt-6.1-sol" {
		t.Fatalf("title sent %q", got)
	}
	if r.Moved == nil || r.Moved.To != "agentrouter/gpt-6.1-sol" || r.Moved.Why != movedTitle {
		t.Fatalf("title's route moved %+v, want agentrouter/gpt-6.1-sol by %s", r.Moved, movedTitle)
	}
	b, _ := json.Marshal(r)
	if !strings.Contains(string(b), `"moved":{"to":"agentrouter/gpt-6.1-sol","why":"desktop-title"}`) {
		t.Fatalf("history JSON: %s", b)
	}
	// Desktop's connection check on that opus: served elsewhere, and says so
	r = send(`{"model":"chatflowsai/claude-opus-5-5","max_tokens":1,"messages":[{"role":"user","content":"."}]}`)
	if r.Moved == nil || (r.Moved.Why != movedCheck && r.Moved.Why != movedCheckFirst) || r.Moved.To == "chatflowsai/claude-opus-5-5" {
		t.Fatalf("check's route moved %+v", r.Moved)
	}
	// a turn on the opus itself is as asked
	r = send(`{"model":"chatflowsai/claude-opus-5-5","max_tokens":32000,"stream":true,"tools":[{"name":"Bash","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"hi"}]}`)
	if r.Moved != nil {
		t.Fatalf("a turn on opus says moved: %+v", r.Moved)
	}
}

// each way desktopMove takes a model elsewhere names its rule, and a
// request it leaves alone names none
func TestDesktopMoveReasons(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	desktopPicked.Lock()
	desktopPicked.model, desktopPicked.from = "", ""
	desktopPicked.Unlock()
	for _, p := range []provider.Provider{
		{ID: "agentrouter", Name: "AgentRouter", Key: "k", Anthropic: "http://127.0.0.1:1", Models: []string{"gpt-6.1-sol"}},
		{ID: "chatflowsai", Name: "ChatFlowsAI", Key: "k", Anthropic: "http://127.0.0.1:1", Models: []string{"claude-opus-5-5"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	turn := []byte(`{"max_tokens":32000,"tools":[{"name":"Bash"}],"messages":[{"role":"user","content":"hi"}]}`)
	title := []byte(`{"max_tokens":200,"system":"You write short session titles.","messages":[{"role":"user","content":"hi"}]}`)

	// no turn yet: a title goes to the first model listed
	if to, why := desktopMove("chatflowsai/claude-opus-5-5", title, false, false); why != movedTitleFirst && (why != "" || to != "chatflowsai/claude-opus-5-5") {
		t.Errorf("first title: %q by %q", to, why)
	}
	if to, why := desktopMove("agentrouter/gpt-6.1-sol", turn, false, false); to != "agentrouter/gpt-6.1-sol" || why != "" {
		t.Fatalf("turn: %q by %q", to, why)
	}
	if to, why := desktopMove("chatflowsai/claude-opus-5-5", title, false, false); to != "agentrouter/gpt-6.1-sol" || why != movedTitle {
		t.Errorf("title: %q by %q", to, why)
	}
	if to, why := desktopMove("claude-haiku-4-5-20251001", turn, false, false); to != "agentrouter/gpt-6.1-sol" || why != movedGone {
		t.Errorf("unserved: %q by %q", to, why)
	}
	if to, why := desktopMove("claude-haiku-5-5", title, true, false); to != "agentrouter/gpt-6.1-sol" || why == "" {
		t.Errorf("Claude Code's haiku: %q by %q", to, why)
	}
	if to, why := desktopMove("chatflowsai/claude-opus-5-5", turn, false, false); to != "chatflowsai/claude-opus-5-5" || why != "" {
		t.Errorf("opus turn: %q by %q", to, why)
	}
}
