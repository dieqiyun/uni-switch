package provider

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// With uni-switch wired into Claude Code, `claude auth status` tells of uni-switch's
// token: signed in, no email. The account is named from ~/.claude.json as
// its saved login is, so it is the one Active and not also served beside
// itself; the env uni-switch runs in is not handed to the CLI (#177).
func TestClaudeAccountNamedAsItsLogin(t *testing.T) {
	shellFakes(t)
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "uni-switch")
	far := time.Now().Add(24 * time.Hour).UnixMilli()
	oauth := func(tok string) map[string]any {
		return map[string]any{"claudeAiOauth": map[string]any{"accessToken": tok, "refreshToken": "r-" + tok, "expiresAt": far, "subscriptionType": "max"}}
	}
	writeFile(t, filepath.Join(home, ".claude", ".credentials.json"), oauth("tok-me"))
	writeFile(t, filepath.Join(home, ".claude.json"), map[string]any{"oauthAccount": map[string]any{"emailAddress": "me@example.com"}})
	writeFile(t, filepath.Join(filepath.Dir(Path()), "logins.json"), []map[string]any{
		{"agent": "claude", "user": "me@example.com", "plan": "max", "on": true, "auth": oauth("tok-me-old")},
		{"agent": "claude", "user": "spare@example.com", "plan": "max", "on": true, "auth": oauth("tok-spare")},
	})
	dir := t.TempDir()
	exe, env := filepath.Join(dir, "claude"), filepath.Join(dir, "env")
	testenv.Program(t, exe, "#!/bin/sh\necho \"token=$ANTHROPIC_AUTH_TOKEN\" > "+env+"\necho '{\"loggedIn\":true,\"authMethod\":\"oauth_token\",\"apiProvider\":\"firstParty\"}'\n")
	claudeExecutable = func() string { return exe }
	loginsMu.Lock()
	loginsSeenAt.Store(0)
	loginsMu.Unlock()

	p, ok := claudeAccount()
	if !ok || p.Account.User != "me@example.com" {
		t.Fatalf("account %v %+v", ok, p.Account)
	}
	if b, _ := os.ReadFile(env); string(b) != "token=\n" {
		t.Fatalf("auth status ran with %q", b)
	}
	var also []string
	for _, q := range p.AlsoOn() {
		also = append(also, q.Account.User)
	}
	if len(also) != 1 || also[0] != "spare@example.com" {
		t.Fatalf("also on: %v", also)
	}
}

// a second look at Claude Code's sign-in serves the last answer at once and
// asks the CLI again behind it (#123)
func TestClaudeIdentityServedWhileAsked(t *testing.T) {
	shellFakes(t)
	dir := t.TempDir()
	exe, who := filepath.Join(dir, "claude"), filepath.Join(dir, "who")
	// the CLI answers only once let exists: until then it is held, and a
	// look that waited for it could not come back (#1617)
	let, reached := filepath.Join(dir, "let"), filepath.Join(dir, "reached")
	os.WriteFile(who, []byte("a@example.com"), 0o600)
	os.WriteFile(let, nil, 0o600)
	testenv.Program(t, exe, "#!/bin/sh\n: > "+reached+"\nwhile [ ! -e "+let+" ]; do sleep 0.02; done\n"+
		"printf '{\"loggedIn\":true,\"email\":\"%s\",\"subscriptionType\":\"max\"}' \"$(cat "+who+")\"\n")
	old := claudeExecutable
	claudeExecutable = func() string { return exe }
	t.Cleanup(func() { claudeExecutable = old; forgetClaudeStatus() })
	forgetClaudeStatus()

	if u, p, out := claudeIdentity(); u != "a@example.com" || p != "max" || out {
		t.Fatalf("first: %q %q %v", u, p, out)
	}
	os.WriteFile(who, []byte("b@example.com"), 0o600)
	os.Remove(let)
	os.Remove(reached)
	// the ask behind the stale look ends with the test
	t.Cleanup(func() {
		os.WriteFile(let, nil, 0o600)
		for range 500 {
			claudeStatusMu.Lock()
			busy := claudeStatusBusy
			claudeStatusMu.Unlock()
			if !busy {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
	claudeStatusMu.Lock()
	claudeStatusAt = time.Now().Add(-time.Minute)
	claudeStatusMu.Unlock()
	looked := make(chan string, 1)
	go func() { u, _, _ := claudeIdentity(); looked <- u }()
	// Nothing is timed: the CLI is held, so a look that comes back came back
	// without waiting for it. The deadline only turns a look that waited into
	// a FAIL, and is short of askClaudeStatus' own 5s, after which a held
	// CLI is killed and a look that waited would come back too.
	select {
	case u := <-looked:
		if u != "a@example.com" {
			t.Fatalf("stale look: %q", u)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("a stale look waited for the held CLI")
	}
	claudeStatusMu.Lock()
	busy := claudeStatusBusy
	claudeStatusMu.Unlock()
	if !busy {
		t.Fatal("the stale look came back with no ask behind it")
	}
	// and the CLI was asked again behind it
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(reached); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the stale look asked nobody")
		}
	}
	os.WriteFile(let, nil, 0o600)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if u, _, _ := claudeIdentity(); u == "b@example.com" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the answer behind the last one never came")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
