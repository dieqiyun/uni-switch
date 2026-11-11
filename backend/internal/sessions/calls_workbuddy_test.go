package sessions

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// workbuddyCalls are the WorkBuddy calls Calls gives, by message id. Calls
// with no id (WorkBuddy's older history) are keyed by their time.
func workbuddyCalls(t *testing.T) map[string]Call {
	t.Helper()
	out := map[string]Call{}
	for _, c := range Calls(time.Time{}) {
		if c.Agent != "workbuddy" {
			continue
		}
		k := c.Msg
		if k == "" {
			k = c.Time.Format("15:04:05.000")
		}
		if _, dup := out[k]; dup {
			t.Fatalf("message %s counted twice", c.Msg)
		}
		out[k] = c
	}
	return out
}

// workbuddyOld are the older history's calls, which name no messageId: each
// reply is a call of its own, as the Sessions page counts them (workbuddyLine
// replaces the call before it only when the id is the same non-empty one).
func workbuddyOld(cs map[string]Call) []Call {
	var out []Call
	for _, c := range cs {
		if c.Model == "auto" && c.Msg == "" {
			out = append(out, c)
		}
	}
	return out
}

// WorkBuddy's calls reach the usage ledger too: the Sessions page read its
// session files all along, and the Usage page showed none of them.
func TestWorkBuddyCalls(t *testing.T) {
	setupAgents(t)
	cs := workbuddyCalls(t)
	// m1 (its assistant message and its last function call both carry the
	// usage, so it must count once), m2, the reply the user's own custom
	// provider answered, and three brought over from the older history. The
	// one uni-switch's gateway answered is the gateway's to count
	// (TestWorkBuddyCallsSkipsuni-switchsGateway), not read here as well.
	if len(cs) != 6 {
		t.Fatalf("want 6 WorkBuddy calls, got %d: %+v", len(cs), cs)
	}
	m, ok := cs["m1"]
	if !ok {
		t.Fatalf("no call for WorkBuddy's reply: %+v", cs)
	}
	// input with the cache read taken out of it, as its summary counts it:
	// 1200 - 1000
	if m.Model != "hy3" || m.Tokens != (Tokens{200, 80, 1000, 0, 0}) || !m.Time.Equal(ms(1790503205000)) {
		t.Fatalf("the reply: %+v", m)
	}
	// the second reply's own total is its last line's: 9999 − 7777 cached
	if n := cs["m2"]; n.Tokens != (Tokens{2222, 888, 7777, 0, 0}) {
		t.Fatalf("the second reply: %+v", n)
	}
	// three brought over from its older history: input_tokens, no cache, and
	// no messageId — each one a call of its own
	old := workbuddyOld(cs)
	if len(old) != 3 {
		t.Fatalf("want 3 older-history calls, got %d: %+v", len(old), old)
	}
	var in, out int
	for _, c := range old {
		in, out = in+c.Tokens.Input, out+c.Tokens.Output
		if c.Session != "0123456789abcdef0123456789abcdef" {
			t.Fatalf("an older-history reply's session: %+v", c)
		}
	}
	if in != 5500 || out != 130 {
		t.Fatalf("the older history's tokens: in %d out %d, want 5500 130 (%+v)", in, out, old)
	}
}

// A call WorkBuddy made to uni-switch's gateway is the gateway's record to keep,
// not this one's: the gateway logs it with the provider and account that
// really answered, and nothing here can pair the two, so reading it would
// count the call twice — once as the gateway's and once as a nameless local
// session's. A model the user pointed at another address in the same picker
// ("custom-local:…" as well) is still read: only a routing group's name, or
// uni-switch's own entry in models.json, says a call went to the gateway.
func TestWorkBuddyCallsSkipsUniSwitchsGateway(t *testing.T) {
	setupAgents(t)
	cs := workbuddyCalls(t)
	if _, dup := cs["wm1"]; dup {
		t.Fatalf("a call made to uni-switch's gateway was read from the file too: %+v", cs["wm1"])
	}
	m, ok := cs["wm2"]
	if !ok {
		t.Fatalf("no call for the user's own custom provider: %+v", cs)
	}
	if m.Model != "glm-5" || m.Tokens != (Tokens{1200, 80, 0, 0, 0}) {
		t.Fatalf("the user's own: %+v", m)
	}
}

// workbuddyGateway reads models.json whichever shape WorkBuddy wrote it in,
// and names a routing group on its own: "group/<id>" is a name only a uni-switch
// answers to, so it needs no configuration behind it.
func TestWorkBuddyGateway(t *testing.T) {
	list := `[{"id":"hy3","vendor":"workbuddy"},{"id":"deepseek/v4","vendor":"uni-switch"}]`
	obj := `{"models":[{"id":"hy3","vendor":"workbuddy"},{"id":"deepseek/v4","vendor":"uni-switch"},{"id":"own-proxy/glm-5","vendor":"openai"}]}`
	for _, c := range []struct {
		shape, id string
		want      bool
	}{
		{"", "custom-local:group/auto-deepseek-v4-1-flash", true}, // the group's name, whatever models.json says
		{"", "custom-local:deepseek/v4", false},                   // no models.json: nothing else is uni-switch's
		{"[]", "custom-local:deepseek/v4", false},                 //
		{list, "custom-local:deepseek/v4", true},                  // uni-switch's entry in the picker
		{list, "deepseek/v4", false},                              // asked for without the picker's prefix
		{list, "custom-local:hy3", false},                         // another vendor's entry
		{list, "custom-local:own-proxy/glm-5", false},             // the user's own, pointed elsewhere
		{list, "", false},                                         // WorkBuddy's own models carry no prefix
		{obj, "custom-local:deepseek/v4", true},                   // an object with a "models" list
		{obj, "custom-local:own-proxy/glm-5", false},              //
	} {
		dir := t.TempDir()
		t.Setenv("WORKBUDDY_CONFIG_DIR", dir)
		if c.shape != "" {
			if err := os.WriteFile(filepath.Join(dir, "models.json"), []byte(c.shape), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if got := workbuddyGateway(c.id); got != c.want {
			t.Fatalf("workbuddyGateway(%q) with %s = %v, want %v", c.id, c.shape, got, c.want)
		}
	}
}

// What the models.json half of that judgement depends on: taking uni-switch's
// models out of WorkBuddy's own configuration stops the calls made while they
// were in being read as the gateway's, and they are counted as the agent's
// own beside the gateway's rows for the same calls. The routing group's name
// keeps being the gateway's whichever way models.json stands.
func TestWorkBuddyGatewayFollowsModelsJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WORKBUDDY_CONFIG_DIR", dir)
	with := filepath.Join(dir, "models.json")
	if err := os.WriteFile(with, []byte(`[{"id":"deepseek/v4","vendor":"uni-switch"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !workbuddyGateway("custom-local:deepseek/v4") {
		t.Fatal("uni-switch's entry in models.json is not the gateway's")
	}
	// uni-switch taken out of WorkBuddy's picker (agent.workbuddyWrite, off)
	if err := os.WriteFile(with, []byte(`[{"id":"hy3","vendor":"workbuddy"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if workbuddyGateway("custom-local:deepseek/v4") {
		t.Fatal("a call asked for by an id models.json no longer holds is still the gateway's")
	}
	if !workbuddyGateway("custom-local:group/auto-deepseek-v4-1-flash") {
		t.Fatal("a routing group's name stopped being the gateway's")
	}
}

// A reply whose first usage line spent nothing, before any call of the
// session's, is one call when its next line spends: there is no call
// before it to stand for, and reading it must not take one that isn't
// there.
func TestWorkBuddyCallsReplyStartingAtZero(t *testing.T) {
	home := setupAgents(t)
	p := filepath.Join(home, ".workbuddy", "projects", "Users-me-zero", "44444444-aaaa-4bbb-8ccc-000000000009.jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	line := func(msg string, in, out int) string {
		return `{"id":"a9","timestamp":1790503210000,"type":"message","role":"assistant","providerData":{"messageId":"` + msg + `","model":"hy3","requestModelId":"hy3","usage":{"inputTokens":` + strconv.Itoa(in) + `,"outputTokens":` + strconv.Itoa(out) + `}},"sessionId":"44444444-aaaa-4bbb-8ccc-000000000009","cwd":"/work/zero"}` + "\n"
	}
	read := func() []Call {
		resetCalls()
		for _, s := range CallSources() {
			if s.Path == p {
				return ReadCallSource(s)
			}
		}
		t.Fatal("no call source for the session")
		return nil
	}
	if err := os.WriteFile(p, []byte(line("m9", 0, 0)+line("m9", 300, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := read(); len(got) != 1 || got[0].Tokens != (Tokens{Input: 300, Output: 20}) {
		t.Fatalf("calls %+v, want the reply's one", got)
	}
	// after another reply's call: that call is not this reply's to replace
	if err := os.WriteFile(p, []byte(line("m8", 50, 5)+line("m9", 0, 0)+line("m9", 300, 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	got := read()
	if len(got) != 2 || got[0].Msg != "m8" || got[0].Tokens != (Tokens{Input: 50, Output: 5}) || got[1].Msg != "m9" || got[1].Tokens != (Tokens{Input: 300, Output: 20}) {
		t.Fatalf("calls %+v, want m8's and m9's", got)
	}
}
