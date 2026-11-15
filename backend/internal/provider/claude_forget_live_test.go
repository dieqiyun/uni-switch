package provider

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// StringKe (#4532): removing the account Claude Code is signed in to was
// refused while Codex's switched first. Claude Code is signed in to the
// next account first now, and the removed one's sign-in is left nowhere:
// not in Claude Code, and not in another record.
func TestForgetLiveClaudeSwitchesFirst(t *testing.T) {
	_, _ = claudeThree(t)
	live, ok := liveLogin("claude")
	if !ok || claudeEmail(live.User) != "pro@x.com" {
		t.Fatalf("live %+v", live)
	}
	gone := live
	if err := ForgetLogin("claude", live.User); err != nil {
		t.Fatal(err)
	}
	forgetAccountCaches()
	now, ok := liveLogin("claude")
	if !ok || claudeEmail(now.User) == "pro@x.com" {
		t.Fatalf("Claude Code still on %+v", now)
	}
	if c, ok := parseClaudeCredentials(now.Auth); !ok || c.OAuth.RefreshToken != "r-"+claudeEmail(now.User) {
		t.Fatalf("Claude Code's sign-in isn't %s's: %+v", now.User, c)
	}
	var users []string
	for _, l := range Logins("claude") {
		users = append(users, l.User)
	}
	if len(users) != 2 || strings.Contains(strings.Join(users, ","), "pro@x.com") {
		t.Fatalf("saved %v", users)
	}
	if _, others := claudeHolders(readLogins(), gone); len(others) != 0 {
		t.Fatalf("the removed sign-in is kept in %+v", others)
	}
}

// Claude Code's account in use is never signed out by a removal, even its
// only one: its sign-in may be in the keychain, and uni-switch serves its
// saved accounts while it is signed out (claudeStandIn). The refusal says
// it is the only one, not "sign it in to another first", and an account
// added by setup-token, which Claude Code can't be signed in to, doesn't
// count as another.
func TestForgetOnlyClaudeIsKept(t *testing.T) {
	home := claudeHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	shellFakes(t)
	creds := claudeSignIn(t, home, time.Now().Add(time.Hour))
	writeFile(t, home+"/.claude.json", map[string]any{"oauthAccount": map[string]any{"emailAddress": "cc@example.com"}})
	rememberLogins(true)
	live, ok := liveLogin("claude")
	if !ok {
		t.Fatal("no Claude Code sign-in")
	}
	loginsMu.Lock()
	err := writeLogins(upsertLogin(readLogins(), savedLogin{Agent: "claude", User: "ci@x.com", On: true, Auth: []byte(`{"setupToken":"sk-ant-oat01-x"}`)}))
	loginsMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	var signedIn *SignedInError
	err = ForgetLogin("claude", live.User)
	if !errors.As(err, &signedIn) || !signedIn.Only || !strings.Contains(err.Error(), "only account") {
		t.Fatalf("claude in use: %v", err)
	}
	if _, err := os.Stat(creds); err != nil {
		t.Fatalf("credentials went: %v", err)
	}
	if now, ok := liveLogin("claude"); !ok || now.User != live.User {
		t.Fatalf("Claude Code moved to %+v", now)
	}
	found := false
	for _, l := range Logins("claude") {
		found = found || l.User == live.User
	}
	if !found {
		t.Fatal("the account was forgotten")
	}
}
