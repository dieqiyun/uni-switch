package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func codexSignIn(t *testing.T, home, email, refresh string) {
	t.Helper()
	exp := float64(time.Now().Add(time.Hour).Unix())
	writeFile(t, filepath.Join(home, ".codex", "auth.json"), map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]any{
			"id_token":      fakeJWT(map[string]any{"email": email, "https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "plus"}}),
			"access_token":  fakeJWT(map[string]any{"exp": exp}),
			"refresh_token": refresh, "account_id": "acct-" + email,
		},
	})
}

func loginUsers(ls []Login) (users []string, active string) {
	for _, l := range ls {
		users = append(users, l.User)
		if l.Active {
			active = l.User
		}
	}
	return
}

func TestCodexLogins(t *testing.T) {
	home := signIn(t)
	rememberLogins(true)
	codexSignIn(t, home, "work@example.com", "r-work")
	rememberLogins(true)

	users, active := loginUsers(Logins("codex"))
	if strings.Join(users, ",") != "me@example.com,work@example.com" || active != "work@example.com" {
		t.Fatalf("logins %v, active %q", users, active)
	}
	if fi, err := os.Stat(loginsPath()); err != nil || runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("logins.json mode: %v %v", fi, err)
	}
	// the agent refreshes the active login in place; a switch keeps that
	codexSignIn(t, home, "work@example.com", "r-work-2")

	if err := SwitchLogin("codex", "ME@example.com"); err != nil {
		t.Fatal(err)
	}
	var a codexAuth
	readJSON(filepath.Join(home, ".codex", "auth.json"), &a)
	if a.Tokens.RefreshToken != "r" {
		t.Fatalf("auth.json not switched: %q", a.Tokens.RefreshToken)
	}
	if _, active := loginUsers(Logins("codex")); active != "me@example.com" {
		t.Fatalf("active after switch: %q", active)
	}
	for _, l := range readLogins() {
		if l.User == "work@example.com" && !strings.Contains(string(l.Auth), "r-work-2") {
			t.Fatalf("the replaced login lost its refreshed token: %s", l.Auth)
		}
	}

	// the active one forgotten: Codex is signed in to the other first
	if err := ForgetLogin("codex", "me@example.com"); err != nil {
		t.Fatal(err)
	}
	if users, active := loginUsers(Logins("codex")); strings.Join(users, ",") != "work@example.com" || active != "work@example.com" {
		t.Fatalf("after forget: %v, active %q", users, active)
	}
	if err := SwitchLogin("codex", "nobody@example.com"); err == nil {
		t.Fatal("switched to an unknown login")
	}
}

func TestCodexDuplicateLoginsCollapse(t *testing.T) {
	home := signIn(t)
	codexSignIn(t, home, "me@example.com", "r")
	auth, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := writeLogins([]savedLogin{
		{Agent: "codex", User: "me@example.com", Plan: "plus", Seen: now.Add(-time.Hour), On: true, First: true, Auth: auth},
		{Agent: "codex", User: "me@example.com", Plan: "plus", Seen: now, Auth: auth},
	}); err != nil {
		t.Fatal(err)
	}
	if got := readLogins(); len(got) != 1 || !got[0].Seen.Equal(now) || !got[0].On || !got[0].First {
		t.Fatalf("saved logins were not deduped: %+v", got)
	}
	raw, err := os.ReadFile(loginsPath())
	if err != nil {
		t.Fatal(err)
	}
	var saved []savedLogin
	if err := json.Unmarshal(raw, &saved); err != nil || len(saved) != 2 {
		t.Fatalf("reading logins changed the saved records: %v, %d records", err, len(saved))
	}
	if got := dedupeLogins([]savedLogin{saved[1], saved[0]}); len(got) != 1 || !got[0].Seen.Equal(now) || !got[0].On || !got[0].First {
		t.Fatalf("reverse-order logins were not deduped: %+v", got)
	}
	if got := Logins("codex"); len(got) != 1 || !got[0].On || !got[0].Active {
		t.Fatalf("deduped logins: %+v", got)
	}
	dupe := savedLogin{Agent: "codex", User: "me@example.com", Plan: "plus", Seen: now.Add(time.Minute), Auth: auth}
	if got := upsertLogin(readLogins(), dupe); len(got) != 1 || !got[0].Seen.Equal(dupe.Seen) {
		t.Fatalf("upsert left duplicate logins: %+v", got)
	}
}

func TestSideLoginsNotCollapsed(t *testing.T) {
	signIn(t)
	now := time.Now().UTC()
	if err := writeLogins([]savedLogin{
		{Agent: "grok", User: "me@example.com", Seen: now.Add(-time.Hour), Home: "/grok/me", First: true},
		{Agent: "grok", User: "me@example.com", Seen: now},
	}); err != nil {
		t.Fatal(err)
	}
	if got := readLogins(); len(got) != 2 || got[0].Home != "/grok/me" {
		t.Fatalf("a Grok account uni-switch signed in was folded into the CLI's own: %+v", got)
	}
}

func TestClaudeLogins(t *testing.T) {
	home := claudeHome(t)
	cred := claudeSignIn(t, home, time.Now().Add(time.Hour))
	profile := filepath.Join(home, ".claude.json")
	writeFile(t, profile, map[string]any{
		"numStartups":  1234567890123,
		"oauthAccount": map[string]any{"emailAddress": "a@example.com", "accountUuid": "u-a"},
	})
	rememberLogins(true)

	// signing in to another account in Claude Code
	writeFile(t, cred, map[string]any{
		"claudeAiOauth": map[string]any{"accessToken": "sk-ant-oat01-b", "refreshToken": "sk-ant-ort01-b",
			"expiresAt": time.Now().Add(time.Hour).UnixMilli(), "subscriptionType": "pro"},
		"mcpOAuth": map[string]any{"keep": true},
	})
	writeFile(t, profile, map[string]any{
		"numStartups":  1234567890123,
		"oauthAccount": map[string]any{"emailAddress": "b@example.com", "accountUuid": "u-b"},
	})
	forgetClaudeCredential()
	rememberLogins(true)

	users, active := loginUsers(Logins("claude"))
	if strings.Join(users, ",") != "a@example.com,b@example.com" || active != "b@example.com" {
		t.Fatalf("logins %v, active %q", users, active)
	}
	if err := SwitchLogin("claude", "a@example.com"); err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	readJSON(cred, &c)
	if o, _ := c["claudeAiOauth"].(map[string]any); o["refreshToken"] != "sk-ant-ort01-old" {
		t.Fatalf("credentials not switched: %v", o)
	}
	b, _ := os.ReadFile(profile)
	var p map[string]json.RawMessage
	json.Unmarshal(b, &p)
	if string(p["numStartups"]) != "1234567890123" || !strings.Contains(string(p["oauthAccount"]), "a@example.com") {
		t.Fatalf(".claude.json after switch: %s", b)
	}
	if _, active := loginUsers(Logins("claude")); active != "a@example.com" {
		t.Fatalf("active after switch: %q", active)
	}
}

func TestRemovedAccountIsNotRemembered(t *testing.T) {
	home := signIn(t)
	if err := Delete("codex"); err != nil {
		t.Fatal(err)
	}
	codexSignIn(t, home, "work@example.com", "r-work")
	rememberLogins(true)
	for _, l := range readLogins() {
		if l.Agent == "codex" && l.User == "work@example.com" {
			t.Fatal("a sign-in of a removed account was saved")
		}
	}
	if err := ShowAccount("codex"); err != nil {
		t.Fatal(err)
	}
	rememberLogins(true)
	saved := false
	for _, l := range readLogins() {
		saved = saved || l.Agent == "codex" && l.User == "work@example.com"
	}
	if !saved {
		t.Fatal("bringing the account back should save its sign-in again")
	}
}
