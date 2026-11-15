package gateway

import (
	"encoding/json"

	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"

	"github.com/tidwall/gjson"
)

// relayNeedsCacheKey answers as CyberCaius's AnyRouter did a request
// without a prompt_cache_key.
func relayNeedsCacheKey(body []byte) (int, string) {
	if gjson.GetBytes(body, "prompt_cache_key").String() == "" {
		return 400, `{"error":{"message":"Arouter: prompt_cache_key is required (request id: x)","type":"invalid_request_error"},"type":"error"}`
	}
	return 0, ""
}

// desktopProbe knows Desktop's check alone, not a title or a turn.
func TestDesktopProbe(t *testing.T) {
	for body, want := range map[string]bool{
		`{"model":"uni-switch-0958175816.anthropic.claude-haiku-5-5[1m]","max_tokens":1,"messages":[{"role":"user","content":"."}]}`: true,
		`{"model":"x","max_tokens":200,"messages":[{"role":"user","content":"."}]}`:                                                  false,
		`{"model":"x","max_tokens":1,"system":"You write short session titles.","messages":[{"role":"user","content":"."}]}`:         false,
		`{"model":"x","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`:                                                   false,
		`{"model":"x","max_tokens":1,"tools":[{"name":"a"}],"messages":[{"role":"user","content":"."}]}`:                             false,
	} {
		if desktopProbe([]byte(body)) != want {
			t.Errorf("desktopProbe(%s) = %v", body, !want)
		}
	}
}

// uni-switch's own cache key goes only to OpenAI's API and a provider the user
// added by its address; a vendor's preset, a signed-in account and a
// plugin get none. A relay that turns the field away is asked again
// without it, once.
func TestOwnCacheKey(t *testing.T) {
	for _, c := range []struct {
		p    provider.Provider
		want bool
	}{
		{provider.Provider{ID: "relay", Chat: "https://relay.example/v1"}, true},
		{provider.Provider{ID: "openai", Preset: "openai", Responses: "https://api.openai.com/v1"}, true},
		{provider.Provider{ID: "deepseek", Preset: "deepseek", Chat: "https://api.deepseek.com/v1"}, false},
		{provider.Provider{ID: "codex", Account: &provider.Account{}, Responses: "https://chatgpt.com/backend-api/codex"}, false},
		{provider.Provider{ID: "relay-own", Chat: "https://relay.example/v1", ExtraBody: provider.ExtraBody{"prompt_cache_key": json.RawMessage(`"${sessionId}"`)}}, false},
		{provider.Provider{ID: "relay-forced", Chat: "https://relay.example/v1", ExtraBody: provider.ExtraBody{"!prompt_cache_key": json.RawMessage(`"k"`)}}, false},
	} {
		if got := ownCacheKey(c.p); got != c.want {
			t.Errorf("ownCacheKey(%s) = %v", c.p.ID, got)
		}
	}

	refusing := func(body []byte) (int, string) {
		if gjson.GetBytes(body, "prompt_cache_key").Exists() {
			return 400, `{"error":{"message":"Unrecognized request argument supplied: prompt_cache_key"}}`
		}
		return 0, ""
	}
	f := &fake{t: t, reply: deepseekReply, refuse: refusing}
	setup(t, provider.Chat, f)
	srv := New()
	for turn := range 2 {
		calls := f.calls
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"fake/m1","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("x-api-key", Token)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("turn %d: %d %s", turn+1, rec.Code, rec.Body)
		}
		if n, want := f.calls-calls, 2-turn; n != want {
			t.Errorf("turn %d: %d upstream calls, want %d", turn+1, n, want)
		}
	}
}

// ctyunRefusal is the body Desktop showed (cybercaius, v0.1.1862) as
// uni-switch's message after "天翼云: ": a relay's error whose message is
// its own upstream's refusal. The 天翼云 relay wraps that whole body as
// its message in turn, so uni-switch's reading of it said it word for word.
const ctyunRefusal = `{"error":{"message":"{\"code\":\"400\",\"message\":\"thinking.type must be \\\"enabled\\\" (type: invalid_request_error)\",\"type\":\"invalid_request_error\"} (request id: 20261028170149123456789)","type":"upstream_error","code":"Invalid_request"}}`

// ctyunNeedsThinking answers as cybercaius's 天翼云 did a Messages request
// whose thinking isn't enabled.
func ctyunNeedsThinking(body []byte) (int, string) {
	if gjson.GetBytes(body, "thinking.type").String() != "enabled" {
		return ctyunRefused()
	}
	if b, m := gjson.GetBytes(body, "thinking.budget_tokens").Int(), gjson.GetBytes(body, "max_tokens").Int(); b < 1024 || m <= b {
		return 400, `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens must be greater than thinking.budget_tokens"}}`
	}
	return 0, ""
}

func ctyunRefused() (int, string) {
	out, _ := json.Marshal(map[string]any{"error": map[string]any{"message": ctyunRefusal, "type": "upstream_error", "code": "Invalid_request"}})
	return 400, string(out)
}

// withThinkingEnabled leaves a request that thinks as it is, and gives
// one that doesn't the least budget under a limit raised past it.
func TestWithThinkingEnabled(t *testing.T) {
	for in, want := range map[string]string{
		`{"max_tokens":16,"temperature":0}`:                    `{"max_tokens":1040,"thinking":{"budget_tokens":1024,"type":"enabled"}}`,
		`{"max_tokens":8000,"thinking":{"type":"disabled"}}`:   `{"max_tokens":8000,"thinking":{"budget_tokens":1024,"type":"enabled"}}`,
		`{"max_tokens":2,"thinking":{"type":"between_tools"}}`: `{"max_tokens":1026,"thinking":{"budget_tokens":1024,"type":"enabled"}}`,
	} {
		got, ok := withThinkingEnabled([]byte(in))
		var a, b any
		if !ok || json.Unmarshal(got, &a) != nil || json.Unmarshal([]byte(want), &b) != nil || !jsonEqual(a, b) {
			t.Errorf("withThinkingEnabled(%s) = %s %v, want %s", in, got, ok, want)
		}
	}
	for _, in := range []string{`{"thinking":{"type":"enabled","budget_tokens":2048}}`, `{"thinking":{"type":"adaptive"}}`, `not json`} {
		if _, ok := withThinkingEnabled([]byte(in)); ok {
			t.Errorf("withThinkingEnabled(%s) changed it", in)
		}
	}
}

// Claude Desktop's check goes to the model the user chose in its own
// model menu, kept in Claude-3p's cowork_account_settings.json
// (__model_selector_state), not to the first model listed: cybercaius
// on Discord (v0.1.2033) had DeepSeek in the Code tab and Cowork off,
// and the check as Desktop started went to another provider's model.
// The menu the last session ran on comes first; a choice uni-switch no
// longer serves, or no file, leaves it to the first listed.
func TestClaudeDesktopProbeToChosenModel(t *testing.T) {
	claude := &fake{t: t, reply: sse(
		`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"claude-haiku-5-5","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`,
		`event: message_stop`+"\n"+`data: {"type":"message_stop"}`)}
	ds := &fake{t: t, reply: deepseekReply}
	kimi := &fake{t: t, reply: deepseekReply}
	up := setup(t, provider.Anthropic, claude)
	du := httptest.NewServer(ds)
	t.Cleanup(du.Close)
	ku := httptest.NewServer(kimi)
	t.Cleanup(ku.Close)
	for _, p := range []provider.Provider{
		{ID: "leee", Name: "leee", Key: "k", Anthropic: up.URL, Models: []string{"claude-haiku-5-5"}},
		{ID: "deepseek", Name: "DeepSeek", Key: "k", Chat: du.URL + "/v1", Models: []string{"deepseek-v4-flash"}},
		{ID: "kimi", Name: "Kimi", Key: "k", Chat: ku.URL + "/v1", Models: []string{"kimi-k3"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	// leee's haiku is listed first: Desktop's default
	if err := provider.SetModelOrder("claude-desktop", []string{"leee/claude-haiku-5-5", "deepseek/deepseek-v4-flash", "kimi/kimi-k3"}); err != nil {
		t.Fatal(err)
	}
	idOf := func(id string) string {
		t.Helper()
		for _, e := range provider.Catalog() {
			if e.ID == id {
				return DesktopID(e)
			}
		}
		t.Fatalf("%s not in the catalog", id)
		return ""
	}
	haiku, dsID, kimiID := idOf("leee/claude-haiku-5-5"), idOf("deepseek/deepseek-v4-flash"), idOf("kimi/kimi-k3")
	root := t.TempDir()
	old := desktopSelectorRoots
	desktopSelectorRoots = func() []string { return []string{root} }
	t.Cleanup(func() { desktopSelectorRoots = old })
	dir := filepath.Join(root, "local-agent-mode-sessions", "c774b2e6", "00000000")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// the menus are written at the hour, a turn sent at a minute past or
	// before it: no two of them in one tick of the file system's clock
	hour := time.Now().Add(-time.Hour).Truncate(time.Second)
	file := filepath.Join(dir, "cowork_account_settings.json")
	choose := func(state string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(`{"__created_at":1,"__model_selector_state":`+state+`}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(file, hour, hour); err != nil {
			t.Fatal(err)
		}
	}
	pickAt := func(m string, at time.Time) {
		desktopPicked.Lock()
		desktopPicked.model, desktopPicked.from, desktopPicked.at = m, desktopPickedPath(), at
		desktopPicked.Unlock()
	}
	pick := func(m string) { pickAt(m, hour.Add(time.Minute)) }
	pick("")
	srv := New()
	probe := `{"model":"` + haiku + `","max_tokens":1,"messages":[{"role":"user","content":"."}]}`
	check := func(want *fake, wantModel, why string) {
		t.Helper()
		claude.got, ds.got, kimi.got = nil, nil, nil
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(probe))
		req.Header.Set("x-api-key", Token+"-claude-desktop")
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh) Claude/2.7032.0 Chrome/140.0 Electron/38.0")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s: probe %d %s", why, rec.Code, rec.Body.String())
		}
		for _, f := range []*fake{claude, ds, kimi} {
			if f != want && f.got != nil {
				t.Fatalf("%s: the check went elsewhere (%s)", why, f.got)
			}
		}
		if want == claude {
			if claude.got == nil {
				t.Fatalf("%s: the check didn't reach leee", why)
			}
			return
		}
		if modelOf(want.got) != wantModel {
			t.Fatalf("%s: the check reached %q, want %q", why, modelOf(want.got), wantModel)
		}
	}

	// no menu chosen yet: the first listed, as before
	check(claude, "", "no choice")
	// the reporter's: DeepSeek in the Code tab, Cowork left on its default
	choose(`{"code":{"model":"` + dsID + `"}}`)
	check(ds, "deepseek-v4-flash", "Code tab on DeepSeek")
	// by the id with Claude Code's "[1m]" too
	choose(`{"code":{"model":"` + dsID + `[1m]"},"chat":{"model":"` + haiku + `"}}`)
	check(ds, "deepseek-v4-flash", "Code tab on DeepSeek [1m]")
	// two menus: the Code tab's, unless the last session ran on the other
	choose(`{"cowork":{"model":"` + kimiID + `"},"code":{"model":"` + dsID + `"}}`)
	check(ds, "deepseek-v4-flash", "Code before Cowork")
	pick(kimiID)
	check(kimi, "kimi-k3", "the last session's menu first")
	// the reporter's (v0.1.2212): a chat on DeepSeek, then the Code tab
	// switched to Kimi while Cowork stays on DeepSeek; the check follows
	// the switch, not the turn sent before it
	pickAt(dsID, hour.Add(-time.Minute))
	choose(`{"code":{"model":"` + kimiID + `"},"cowork":{"model":"` + dsID + `"}}`)
	check(kimi, "kimi-k3", "Code tab switched off the last session's model")
	// a session switched to Kimi after the menu was set to DeepSeek: the
	// model the user is on now
	choose(`{"code":{"model":"` + dsID + `"}}`)
	pick(kimiID)
	check(kimi, "kimi-k3", "a turn after the menu was set")
	// such a turn on a model uni-switch no longer serves leaves it to the menu
	pick("anthropic/uni-switch-0000000001")
	check(ds, "deepseek-v4-flash", "a newer turn on an unserved model")
	pick("")
	// a choice uni-switch no longer serves leaves it to the first listed
	choose(`{"code":{"model":"anthropic/uni-switch-0000000001"}}`)
	check(claude, "", "unserved choice")
	// a half-written file is no choice
	choose(`{"code":{"model":"` + dsID)
	check(claude, "", "unparsed file")
}
