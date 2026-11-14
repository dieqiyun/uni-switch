package agent

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func saveDeepseek(t *testing.T) {
	t.Helper()
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro", "flash"}}); err != nil {
		t.Fatal(err)
	}
}

// Claude Code running through uni-switch can keep its claude.ai sign-in: its
// sign-in field at claudeai leaves ANTHROPIC_AUTH_TOKEN empty, which Claude
// Code reads as no key, so it stays signed in to claude.ai and sends that
// sign-in to uni-switch. Every later write — a model, a tier, the subagents,
// a /model pick followed, Reapply — keeps it so, and a key put there by
// hand is said, not taken for uni-switch's.
func TestClaudeKeepsItsClaudeAISignIn(t *testing.T) {
	t.Setenv("UNI_SWITCH_ADDR", "")
	home, path := claudeSettings(t, `{"theme":"dark"}`)
	saveDeepseek(t)
	env := func(k string) (string, bool) { return edit.GetJSON(path, "env."+k) }
	a := claude(home)
	login := a.Field("login")
	if login == nil {
		t.Fatal("Claude Code has no sign-in field")
	}
	if o := login.Options(a.Values()); len(o) != 0 {
		t.Fatalf("sign-ins offered before Claude Code runs through uni-switch: %v", o)
	}
	if err := login.Set("claudeai"); err == nil {
		t.Fatal("a sign-in kept before Claude Code runs through uni-switch")
	}
	if err := a.Apply("model", "deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if tok, _ := env("ANTHROPIC_AUTH_TOKEN"); tok != gateway.Token {
		t.Fatalf("uni-switch's key by default, got %q", tok)
	}
	if o := login.Options(a.Values()); len(o) != 2 {
		t.Fatalf("sign-ins %v", o)
	}
	if err := login.Set("bogus"); err == nil {
		t.Fatal("an unknown sign-in taken")
	}
	if err := a.Apply("login", "claudeai"); err != nil {
		t.Fatal(err)
	}
	kept := func(when string) {
		t.Helper()
		if tok, has := env("ANTHROPIC_AUTH_TOKEN"); !has || tok != "" {
			t.Fatalf("%s: ANTHROPIC_AUTH_TOKEN %q (set %v), want empty", when, tok, has)
		}
		if u, _ := env("ANTHROPIC_BASE_URL"); u != gateway.URL() {
			t.Fatalf("%s: base URL %q", when, u)
		}
		if v := a.Values()["login"]; v != "claudeai" {
			t.Fatalf("%s: sign-in %q", when, v)
		}
		if !a.Wired() {
			t.Fatalf("%s: not wired", when)
		}
		if d := a.Drift(); d != nil {
			t.Fatalf("%s: drift %+v", when, d)
		}
	}
	kept("set")
	for _, s := range [][2]string{{"model", "deepseek/flash"}, {"haiku", "deepseek/pro"}, {"subagent", "deepseek/flash"}, {"model", "deepseek/pro"}} {
		if err := a.Apply(s[0], s[1]); err != nil {
			t.Fatal(err)
		}
		kept(s[0] + " " + s[1])
	}
	// a pick in Claude Code's /model, followed
	if err := edit.SetJSON(path, edit.KV{Path: "model", Value: "deepseek/flash"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Follow(); err != nil {
		t.Fatal(err)
	}
	kept("/model followed")
	if err := a.Reapply(); err != nil {
		t.Fatal(err)
	}
	kept("reapplied")

	// a key put there takes the sign-in's place: said, and Reapply empties it
	if err := edit.SetJSON(path, edit.KV{Path: "env.ANTHROPIC_AUTH_TOKEN", Value: "sk-other"}); err != nil {
		t.Fatal(err)
	}
	if c := a.Check(); !strings.Contains(c, "sends uni-switch that key, not its claude.ai sign-in") {
		t.Fatalf("a key beside the sign-in kept: %q", c)
	}
	if err := a.Reapply(); err != nil {
		t.Fatal(err)
	}
	kept("reapplied over a key")

	// back to uni-switch's key
	if err := a.Apply("login", ""); err != nil {
		t.Fatal(err)
	}
	if tok, _ := env("ANTHROPIC_AUTH_TOKEN"); tok != gateway.Token || a.Values()["login"] != "" || a.Drift() != nil {
		t.Fatalf("uni-switch's key again: token %q, values %v, drift %+v", tok, a.Values(), a.Drift())
	}
	// uni-switch's key emptied or taken out by hand: said, with the way to keep it so
	if err := edit.SetJSON(path, edit.KV{Path: "env.ANTHROPIC_AUTH_TOKEN", Value: ""}); err != nil {
		t.Fatal(err)
	}
	if c := a.Check(); !strings.Contains(c, "is empty") || !strings.Contains(c, "set its sign-in to claude.ai") {
		t.Fatalf("uni-switch's key emptied: %q", c)
	}
	if err := edit.DelJSON(path, "env.ANTHROPIC_AUTH_TOKEN"); err != nil {
		t.Fatal(err)
	}
	if c := a.Check(); !strings.Contains(c, "is gone") {
		t.Fatalf("uni-switch's key gone: %q", c)
	}
	if v, _ := edit.GetJSON(path, "theme"); v != "dark" {
		t.Fatal("theme lost")
	}
}

// Switched off, Claude Code gets back its own endpoint and key; switched
// on again, it keeps its claude.ai sign-in as it did.
func TestClaudeSignInThroughDisconnect(t *testing.T) {
	t.Setenv("UNI_SWITCH_ADDR", "")
	home, path := claudeSettings(t, `{"theme":"dark","env":{"ANTHROPIC_BASE_URL":"https://relay.example","ANTHROPIC_AUTH_TOKEN":"sk-relay"}}`)
	saveDeepseek(t)
	env := func(k string) string { v, _ := edit.GetJSON(path, "env."+k); return v }
	a := claude(home)
	if err := a.Apply("model", "deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply("login", "claudeai"); err != nil {
		t.Fatal(err)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if env("ANTHROPIC_BASE_URL") != "https://relay.example" || env("ANTHROPIC_AUTH_TOKEN") != "sk-relay" || a.Wired() {
		t.Fatalf("switched off:\n%s", readFile(path))
	}
	if v := a.Values()["login"]; v != "" {
		t.Fatalf("sign-in %q off uni-switch", v)
	}
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if tok, has := edit.GetJSON(path, "env.ANTHROPIC_AUTH_TOKEN"); !has || tok != "" || env("ANTHROPIC_BASE_URL") != gateway.URL() || a.Values()["login"] != "claudeai" {
		t.Fatalf("switched on again:\n%s\nvalues %v", readFile(path), a.Values())
	}
}

// An empty key at an endpoint the user set after uni-switch stepped out is
// theirs, though Claude Code kept its claude.ai sign-in through uni-switch
// before: its model default leaves it (yetone on #1218).
func TestClaudeOwnEndpointWithAnEmptyKey(t *testing.T) {
	t.Setenv("UNI_SWITCH_ADDR", "")
	home, path := claudeSettings(t, `{"theme":"dark","env":{"ANTHROPIC_BASE_URL":"https://relay.example","ANTHROPIC_AUTH_TOKEN":"sk-relay"}}`)
	saveDeepseek(t)
	env := func(k string) (string, bool) { return edit.GetJSON(path, "env."+k) }
	a := claude(home)
	if err := a.Apply("model", "deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply("login", "claudeai"); err != nil {
		t.Fatal(err)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if err := edit.SetJSON(path, edit.KV{Path: "env.ANTHROPIC_BASE_URL", Value: "https://corp.example"}, edit.KV{Path: "env.ANTHROPIC_AUTH_TOKEN", Value: ""}); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply("model", ""); err != nil {
		t.Fatal(err)
	}
	u, _ := env("ANTHROPIC_BASE_URL")
	if tok, has := env("ANTHROPIC_AUTH_TOKEN"); u != "https://corp.example" || !has || tok != "" {
		t.Fatalf("the user's endpoint and empty key went:\n%s", readFile(path))
	}
}

// Moved to another port, Claude Code keeps its sign-in; left at an old
// one, its model default takes out uni-switch's empty key with the address.
func TestClaudeSignInAcrossPorts(t *testing.T) {
	t.Setenv("UNI_SWITCH_ADDR", "")
	home, path := claudeSettings(t, `{"theme":"dark"}`)
	saveDeepseek(t)
	setPort(t, 3591)
	a := claude(home)
	if err := a.Apply("model", "deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply("login", "claudeai"); err != nil {
		t.Fatal(err)
	}
	on := OnGateway()
	setPort(t, 3592)
	if _, err := Rewire(on); err != nil {
		t.Fatal(err)
	}
	u, _ := edit.GetJSON(path, "env.ANTHROPIC_BASE_URL")
	tok, has := edit.GetJSON(path, "env.ANTHROPIC_AUTH_TOKEN")
	if u != "http://127.0.0.1:3592" || !has || tok != "" || a.Values()["login"] != "claudeai" {
		t.Fatalf("moved:\n%s", readFile(path))
	}
	setPort(t, 3593)
	if err := a.Field("model").Set(""); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN"} {
		if _, has := edit.GetJSON(path, "env."+k); has {
			t.Fatalf("%s left at the old port:\n%s", k, readFile(path))
		}
	}
}

// Wired in, then the gateway moved without uni-switch following (it wasn't
// running): a uni-switch model picked at the older address takes nothing of
// uni-switch's there for the user's, with its key or its claude.ai sign-in.
// Switched off, Claude Code gets its own endpoint and key back; on its own
// model, no gateway address is left.
func TestClaudeAtAnOlderGatewayAddress(t *testing.T) {
	t.Setenv("UNI_SWITCH_ADDR", "")
	for _, login := range []string{"", "claudeai"} {
		for _, off := range []string{"disconnect", "default"} {
			home, path := claudeSettings(t, `{"theme":"dark","env":{"ANTHROPIC_BASE_URL":"https://relay.example","ANTHROPIC_AUTH_TOKEN":"sk-relay"}}`)
			saveDeepseek(t)
			setPort(t, 3591)
			a := claude(home)
			if err := a.Apply("model", "deepseek/pro"); err != nil {
				t.Fatal(err)
			}
			if err := a.Apply("login", login); err != nil {
				t.Fatal(err)
			}
			setPort(t, 3592)
			if err := a.Apply("model", "deepseek/flash"); err != nil {
				t.Fatal(err)
			}
			if u, _ := edit.GetJSON(path, "env.ANTHROPIC_BASE_URL"); u != "http://127.0.0.1:3592" {
				t.Fatalf("%q %s: not at the new address:\n%s", login, off, readFile(path))
			}
			var err error
			if off == "disconnect" {
				err = a.Disconnect()
			} else {
				err = a.Field("model").Set("")
			}
			if err != nil {
				t.Fatal(err)
			}
			want := `{"ANTHROPIC_AUTH_TOKEN":"sk-relay","ANTHROPIC_BASE_URL":"https://relay.example"}`
			if got := readFile(path); !strings.Contains(strings.Join(strings.Fields(got), ""), `"env":`+want) {
				t.Fatalf("%q %s: want the relay back alone, got\n%s", login, off, got)
			}
		}
	}
}

// A stopped distro's Claude Code offers a tier's effort, and the
// subagents', the levels of its model as a running one does, not
// uni-switch's models: picking one of those as an effort is refused (cybercaius).
func TestWSLClaudeStoppedEfforts(t *testing.T) {
	root, _ := claudeDistroHome(t, `{}`)
	saveDeepseek(t)
	d := distro{Name: "Stopped", Root: root, Home: "/home/me", Has: map[string]bool{"dir:.claude": true}, Gateway: "172.20.0.1"}
	k := wslKindOf("claude")
	a := asleep(wslAgent(k, d), k, d)
	cur := map[string]string{"model": "deepseek/pro"}
	for _, key := range []string{"opus_effort", "sonnet_effort", "haiku_effort", "subagent_effort"} {
		f := a.Field(key)
		if f == nil {
			t.Fatalf("no %s", key)
		}
		for _, o := range f.Options(cur) {
			if !contains(provider.MemberEfforts, o.Value) {
				t.Fatalf("%s offers %q, not an effort: %v", key, o.Value, f.Options(cur))
			}
		}
	}
	if f := a.Field("ultracode"); f != nil {
		for _, o := range f.Options(cur) {
			if o.Value != "on" {
				t.Fatalf("ultracode offers %q", o.Value)
			}
		}
	}
	// a tier still offers uni-switch's models
	if len(a.Field("opus").Options(cur)) == 0 {
		t.Fatal("opus offers no model")
	}
}
