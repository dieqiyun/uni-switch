package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// codexWorkspaceSignIn signs Codex in to a seat of email's in a Team
// workspace: two of them read alike, "email · Team".
func codexWorkspaceSignIn(t *testing.T, home, email, workspace, refresh string) {
	t.Helper()
	exp := float64(time.Now().Add(time.Hour).Unix())
	writeFile(t, filepath.Join(home, ".codex", "auth.json"), map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]any{
			"id_token": fakeJWT(map[string]any{"email": email, "https://api.openai.com/auth": map[string]any{
				"chatgpt_plan_type": "team", "chatgpt_account_id": workspace}}),
			"access_token":  fakeJWT(map[string]any{"exp": exp}),
			"refresh_token": refresh, "account_id": workspace,
		},
	})
}

// workspaceOf is the workspace a saved Codex account's credentials are
// for, and their refresh token.
func workspaceOf(t *testing.T, user string) (workspace, refresh string) {
	t.Helper()
	for _, l := range readLogins() {
		if l.Agent == "codex" && l.User == user {
			var a codexAuth
			json.Unmarshal(l.Auth, &a)
			return a.Tokens.AccountID, a.Tokens.RefreshToken
		}
	}
	t.Fatalf("%q isn't saved: %+v", user, readLogins())
	return "", ""
}

// Two Codex accounts of one email in two Team workspaces are two accounts
// in every way (vincentzhang on Discord: removing one, or codex logout on
// one, had the other ask to be signed in again). They read alike, and
// everything that went by the name took both: removing the one not in use
// signed Codex out of the other (it looked like the last), and a refresh
// of one was written over the other's credentials too.
func TestCodexWorkspacesOfOneEmail(t *testing.T) {
	home := signIn(t)
	auth := filepath.Join(home, ".codex", "auth.json")
	codexWorkspaceSignIn(t, home, "me@example.com", "ws-one", "r-one")
	rememberLogins(true)
	codexWorkspaceSignIn(t, home, "me@example.com", "ws-two", "r-two")
	rememberLogins(true)

	users, active := loginUsers(Logins("codex"))
	if len(users) != 2 {
		t.Fatalf("accounts: %v", users)
	}
	seats := []string{}
	for _, u := range users {
		if strings.HasPrefix(u, "me@example.com · Team") {
			seats = append(seats, u)
		}
	}
	if len(seats) != 2 || strings.EqualFold(seats[0], seats[1]) {
		t.Fatalf("the two seats read alike: %v", users)
	}
	actives := 0
	for _, l := range Logins("codex") {
		if l.Active {
			actives++
		}
	}
	if actives != 1 {
		t.Fatalf("%d accounts in use: %+v", actives, Logins("codex"))
	}
	if ws, _ := workspaceOf(t, active); ws != "ws-two" {
		t.Fatalf("in use: %q is %s", active, ws)
	}
	other := seats[0]
	if other == active {
		other = seats[1]
	}
	if ws, rt := workspaceOf(t, other); ws != "ws-one" || rt != "r-one" {
		t.Fatalf("the other: %s %s", ws, rt)
	}

	// a refresh of the one not in use is its own, not the other's too
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		json.NewEncoder(w).Encode(map[string]any{"access_token": fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())}),
			"refresh_token": body["refresh_token"] + "-2"})
	}))
	defer fake.Close()
	old := codexTokenURL
	codexTokenURL = fake.URL
	t.Cleanup(func() { codexTokenURL = old })
	if _, _, err := renewSavedLogin(context.Background(), "codex", other, true); err != nil {
		t.Fatal(err)
	}
	if ws, rt := workspaceOf(t, other); ws != "ws-one" || rt != "r-one-2" {
		t.Fatalf("the one refreshed: %s %s", ws, rt)
	}
	if ws, rt := workspaceOf(t, active); ws != "ws-two" || rt != "r-two" {
		t.Fatalf("the one in use was written over: %s %s", ws, rt)
	}

	// removing the one not in use leaves Codex signed in to the other
	if err := ForgetLogin("codex", other); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(auth); err != nil {
		t.Fatalf("Codex was signed out of %s: %v", active, err)
	}
	if ws, rt := workspaceOf(t, active); ws != "ws-two" || rt != "r-two" {
		t.Fatalf("the one in use: %s %s", ws, rt)
	}
	users, now := loginUsers(Logins("codex"))
	if len(users) != 1 || now != active {
		t.Fatalf("after removing %s: %v, in use %q", other, users, now)
	}
}

// Two saved by one name before this are told apart when read, and the one
// Codex isn't on can be removed without signing it out of the other.
func TestCodexWorkspacesSavedAlike(t *testing.T) {
	home := signIn(t)
	auth := filepath.Join(home, ".codex", "auth.json")
	var saved []savedLogin
	for _, ws := range []string{"ws-one", "ws-two"} {
		codexWorkspaceSignIn(t, home, "me@example.com", ws, "r-"+ws)
		b, err := os.ReadFile(auth)
		if err != nil {
			t.Fatal(err)
		}
		saved = append(saved, savedLogin{Agent: "codex", User: "me@example.com · Team", Plan: "team", Auth: b})
	}
	if err := writeLogins(saved); err != nil {
		t.Fatal(err)
	}
	ls := Logins("codex")
	users, active := loginUsers(ls)
	if len(users) != 2 || strings.EqualFold(users[0], users[1]) {
		t.Fatalf("accounts: %v", users)
	}
	if ws, _ := workspaceOf(t, active); ws != "ws-two" {
		t.Fatalf("in use: %q is %s", active, ws)
	}
	other := users[0]
	if other == active {
		other = users[1]
	}
	if err := ForgetLogin("codex", other); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(auth); err != nil {
		t.Fatalf("Codex was signed out of %s: %v", active, err)
	}
	if users, now := loginUsers(Logins("codex")); len(users) != 1 || now != active {
		t.Fatalf("after removing %s: %v, in use %q", other, users, now)
	}
}

// #1424: a session of the Team seat Codex is on is told as that seat's,
// by the name it is saved under, not the other seat's of the email; and
// it is listed once, saved or not.
func TestCodexWorkspaceSessionIdentities(t *testing.T) {
	home := signIn(t)
	codexWorkspaceSignIn(t, home, "me@example.com", "ws-one", "r-one")
	rememberLogins(true)
	codexWorkspaceSignIn(t, home, "me@example.com", "ws-two", "r-two")
	rememberLogins(true)
	_, active := loginUsers(Logins("codex"))
	if active == "me@example.com · Team" || !strings.HasPrefix(active, "me@example.com · Team · ") {
		t.Fatalf("the second seat is saved as %q", active)
	}
	byWorkspace := map[string][]string{}
	for _, id := range SessionIdentities(filepath.Join(home, ".codex")) {
		if id.Agent == "codex" {
			byWorkspace[id.AccountID] = append(byWorkspace[id.AccountID], id.User)
		}
	}
	if got := byWorkspace["ws-two"]; len(got) != 1 || got[0] != active {
		t.Fatalf("the seat Codex is on: %v, want [%s]", got, active)
	}
	if got := byWorkspace["ws-one"]; len(got) != 1 || got[0] != "me@example.com · Team" {
		t.Fatalf("the other seat: %v", got)
	}
}
