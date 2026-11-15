package provider

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Claude Code wired to uni-switch, its accounts served by uni-switch: the user logs
// out of Claude Code (it says to, over uni-switch's ANTHROPIC_AUTH_TOKEN), and
// the accounts saved in uni-switch are still served — the one it was signed in
// to first, in a config directory of its own, the others on behind it — and
// none of them is dropped from the list (StringKe on Discord: all six went).
func TestClaudeAccountsOutliveLogout(t *testing.T) {
	home := claudeHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	noAnthropic(t)
	cred := claudeSignIn(t, home, time.Now().Add(time.Hour))
	writeFile(t, filepath.Join(home, ".claude.json"), map[string]any{"oauthAccount": map[string]any{"emailAddress": "own@example.com"}})
	far := time.Now().Add(24 * time.Hour).UnixMilli()
	oauth := func(tok string) map[string]any {
		return map[string]any{"claudeAiOauth": map[string]any{"accessToken": tok, "refreshToken": "r-" + tok, "expiresAt": far, "subscriptionType": "max"}}
	}
	seen := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	writeFile(t, loginsPath(), []map[string]any{
		{"agent": "claude", "user": "on@example.com", "plan": "max", "on": true, "seen": seen, "auth": oauth("tok-on")},
		{"agent": "claude", "user": "off@example.com", "plan": "pro", "seen": seen, "auth": oauth("tok-off")},
		{"agent": "claude", "user": "two@example.com", "plan": "max", "on": true, "seen": seen.Add(-time.Hour), "auth": oauth("tok-two")},
	})
	loginsMu.Lock()
	loginsSeenAt.Store(0)
	loginsMu.Unlock()
	if p, ok := find(All(), "claude"); !ok || p.Account.User != "own@example.com" {
		t.Fatalf("signed in: %v %+v", ok, p.Account)
	}

	// /logout: Claude Code's sign-in is gone
	os.Remove(cred)
	os.Remove(filepath.Join(home, ".claude.json"))
	forgetClaudeCredential()
	loginsMu.Lock()
	loginsSeenAt.Store(0)
	loginsMu.Unlock()

	// the one Claude Code held went with its /logout, which revokes the
	// refresh token it holds — uni-switch's copy of it too (StringKe on
	// Discord: all six had to be signed in again). The others are sign-ins
	// of their own: one on stands in, never the revoked one.
	p, ok := find(All(), "claude")
	if !ok || p.Account == nil || p.Account.User != "on@example.com" {
		t.Fatalf("logged out: %v %+v", ok, p.Account)
	}
	dir, own, err := p.Account.Token(context.Background())
	if err != nil || !own || dir != claudeAccountDir("on@example.com") {
		t.Fatalf("dir %q %v %v", dir, own, err)
	}
	if c, ok := readClaudeDir(dir); !ok || c.OAuth.RefreshToken != "r-tok-on" {
		t.Fatalf("dir credentials %+v", c.OAuth)
	}
	if _, err := os.Stat(claudeAccountDir("own@example.com")); !os.IsNotExist(err) {
		t.Fatalf("the revoked sign-in was put to use: %v", err)
	}
	var also []string
	for _, q := range p.AlsoOn() {
		also = append(also, q.Account.User)
	}
	if len(also) != 1 || also[0] != "two@example.com" {
		t.Fatalf("also on: %v", also)
	}
	ls := Logins("claude")
	if len(ls) != 4 {
		t.Fatalf("logins: %+v", ls)
	}
	for _, l := range ls {
		revoked := l.User == "own@example.com"
		if l.Active || l.On != (l.User == "on@example.com" || l.User == "two@example.com") ||
			(l.Lapsed == claudeLogoutLapse) != revoked {
			t.Fatalf("login %+v", l)
		}
	}
	if u := InUseLogin("claude"); u != "on@example.com" {
		t.Fatalf("in use %q", u)
	}
	for _, x := range savedButSignedOut() {
		if x.Agent == "claude" {
			t.Fatalf("excluded: %+v", x)
		}
	}

	// the stand-in can be paused behind another on, as Claude Code's own could
	if err := SetLoginOn("claude", "on@example.com", false); err != nil {
		t.Fatal(err)
	}
	if p, _ = find(All(), "claude"); !p.OwnPaused() {
		t.Fatal("not paused")
	}
	if u := InUseLogin("claude"); u != "two@example.com" {
		t.Fatalf("in use %q", u)
	}

	// /login again in Claude Code, to the same account: a sign-in anew,
	// no longer lapsed
	writeFile(t, cred, oauth("tok-again"))
	writeFile(t, filepath.Join(home, ".claude.json"), map[string]any{"oauthAccount": map[string]any{"emailAddress": "own@example.com"}})
	ForgetAccounts()
	for _, l := range Logins("claude") {
		if l.User == "own@example.com" && (!l.Active || l.Lapsed != "") {
			t.Fatalf("signed in again: %+v", l)
		}
	}
}

// A switch in uni-switch takes Claude Code off one account and puts it on
// another, saving the first's sign-in as it was: nothing revoked it, and it
// isn't taken for logged out.
func TestClaudeSwitchIsNoLogout(t *testing.T) {
	home := claudeHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	noAnthropic(t)
	claudeSignIn(t, home, time.Now().Add(time.Hour))
	writeFile(t, filepath.Join(home, ".claude.json"), map[string]any{"oauthAccount": map[string]any{"emailAddress": "own@example.com"}})
	far := time.Now().Add(24 * time.Hour).UnixMilli()
	writeFile(t, loginsPath(), []map[string]any{
		{"agent": "claude", "user": "other@example.com", "plan": "max", "seen": time.Now().Add(-time.Hour).UTC(),
			"profile": map[string]any{"emailAddress": "other@example.com"},
			"auth":    map[string]any{"claudeAiOauth": map[string]any{"accessToken": "tok-other", "refreshToken": "r-other", "expiresAt": far}}},
	})
	ForgetAccounts()
	if u := InUseLogin("claude"); u != "own@example.com" {
		t.Fatalf("in use %q", u)
	}
	if err := SwitchLogin("claude", "other@example.com"); err != nil {
		t.Fatal(err)
	}
	ForgetAccounts()
	for _, l := range Logins("claude") {
		if l.Lapsed != "" || l.Active != (l.User == "other@example.com") {
			t.Fatalf("login %+v", l)
		}
	}
}

// Logged out of Claude Code with accounts in uni-switch, an account signed in
// in uni-switch stays uni-switch's: Claude Code isn't signed in to it again, which
// would only bring back its warning over uni-switch's token.
func TestClaudeSignInWhileLoggedOut(t *testing.T) {
	home := claudeHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	noAnthropic(t)
	far := time.Now().Add(24 * time.Hour).UnixMilli()
	writeFile(t, loginsPath(), []map[string]any{
		{"agent": "claude", "user": "old@example.com", "plan": "max", "seen": time.Now().Add(-time.Hour).UTC(),
			"auth": map[string]any{"claudeAiOauth": map[string]any{"accessToken": "tok-old", "refreshToken": "r-old", "expiresAt": far}}},
	})
	fakeClaudeLogin(t, fakeClaudeAccount{email: "new@example.com", plan: "pro", refresh: "sk-ant-ort01-new"}, "", true)

	st, err := StartSignIn("claude")
	if err != nil {
		t.Fatal(err)
	}
	finishInBrowser(t, st, "the-code")
	if st = waitDone(t, st.ID); st.State != "done" || st.Using {
		t.Fatalf("state %+v", st)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", ".credentials.json")); !os.IsNotExist(err) {
		t.Fatalf("Claude Code signed in: %v", err)
	}
	forgetClaudeCredential()
	var users []string
	for _, l := range Logins("claude") {
		if l.On {
			users = append(users, l.User)
		}
	}
	if len(users) != 2 {
		t.Fatalf("on: %v", users)
	}
}

// Every saved Claude account's sign-in gone while Claude Code is signed out
// (LeeHom on Discord: two Claude Pro accounts vanished from Providers, and
// were taken for a ban): the one Claude Code held went with its sign-in,
// and the other had gone the same way before. Claude Code stays on the
// Providers list, signed out, each account on it saying to sign in again,
// and its token says so too; none is served as if it could be used.
func TestClaudeAllSignedOutStaysListed(t *testing.T) {
	home := claudeHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	noAnthropic(t)
	cred := claudeSignIn(t, home, time.Now().Add(time.Hour))
	writeFile(t, filepath.Join(home, ".claude.json"), map[string]any{"oauthAccount": map[string]any{"emailAddress": "own@example.com"}})
	far := time.Now().Add(24 * time.Hour).UnixMilli()
	seen := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	writeFile(t, loginsPath(), []map[string]any{
		{"agent": "claude", "user": "two@example.com", "plan": "pro", "on": true, "seen": seen, "lapsed": claudeLogoutLapse,
			"auth": map[string]any{"claudeAiOauth": map[string]any{"accessToken": "tok-two", "refreshToken": "r-two", "expiresAt": far, "subscriptionType": "pro"}}},
	})
	loginsMu.Lock()
	loginsSeenAt.Store(0)
	loginsMu.Unlock()
	if p, ok := find(All(), "claude"); !ok || p.Account.User != "own@example.com" {
		t.Fatalf("signed in: %v %+v", ok, p.Account)
	} else if p.SignedOut() {
		t.Fatal("a signed-in Claude Code reads as signed out")
	}

	// Claude Code lets go of its sign-in: Anthropic refused it, or /logout
	os.Remove(cred)
	os.Remove(filepath.Join(home, ".claude.json"))
	forgetClaudeCredential()
	loginsMu.Lock()
	loginsSeenAt.Store(0)
	loginsMu.Unlock()

	p, ok := find(All(), "claude")
	if !ok || p.Account == nil {
		t.Fatalf("Claude Code left the Providers list with every account signed out: %v", ok)
	}
	if p.Account.User != "own@example.com" {
		t.Fatalf("listed as %q; want the one seen last", p.Account.User)
	}
	if _, _, err := p.Account.Token(context.Background()); err == nil || err.Error() != claudeLogoutLapse {
		t.Fatalf("token of a signed-out account: %v", err)
	}
	ls := Logins("claude")
	if len(ls) != 2 {
		t.Fatalf("logins: %+v", ls)
	}
	listedOn := false
	for _, l := range ls {
		if l.Lapsed != claudeLogoutLapse {
			t.Fatalf("not signed out: %+v", l)
		}
		listedOn = listedOn || l.On && l.User == "own@example.com"
	}
	// the row reads as signed out: the account it names is the one on
	if !listedOn {
		t.Fatalf("the listed account isn't on: %+v", ls)
	}
	if u := InUseLogin("claude"); u != "own@example.com" && u != "" {
		t.Fatalf("in use %q", u)
	}
	// what uni-switch providers, the TUI and the app's row say of it
	if !p.SignedOut() {
		t.Fatal("listed on a signed-out account, the provider doesn't say it is signed out")
	}
}
