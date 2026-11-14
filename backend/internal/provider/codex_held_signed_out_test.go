package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// charliepu on Discord (Windows, 10-31): a ChatGPT Pro account Codex is
// signed in to in another of its folders (C:\Users\charl\.codex-db-backup-
// 20260803) was refused its refresh. Its row had no Sign in again, and its
// allowance line said "run CODEX_HOME=… codex login". Now the row says
// signed out, nothing tells the user to run a command, and uni-switch's own
// sign-in writes the new tokens back to that folder, the same account.
func TestCodexHeldInOtherFolderSignedOutThenSignedInAgain(t *testing.T) {
	home := signIn(t)
	t.Setenv("CODEX_HOME", "")
	resetLoginUsage(t)
	resetRevoked()
	t.Cleanup(resetRevoked)
	backup := filepath.Join(home, ".codex-db-backup-20260803")
	soon := float64(time.Now().Add(time.Minute).Unix())
	claims := map[string]any{"email": "pro@example.com",
		"https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "pro", "chatgpt_account_id": "acct-pro"}}
	idTok := fakeJWT(claims)
	writeFile(t, filepath.Join(backup, "auth.json"), map[string]any{
		"auth_mode": "chatgpt", "OPENAI_API_KEY": nil, "last_refresh": "2026-08-03T00:00:00Z",
		"tokens": map[string]any{"id_token": idTok, "access_token": fakeJWT(map[string]any{"exp": soon}),
			"refresh_token": "r-dead", "account_id": "acct-pro"},
	})
	mainAuth, _ := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))

	fresh := fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix()), "n": "again"})
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/backend-api/wham/usage" {
			if r.Header.Get("Authorization") != "Bearer "+fresh {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			io.WriteString(w, `{"plan_type":"pro","rate_limit":{"allowed":true,"primary_window":{"used_percent":5,"limit_window_seconds":18000,"reset_after_seconds":600}}}`)
			return
		}
		// every refresh refused for good, as OpenAI refused charliepu's
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"invalid_grant"}`)
	}))
	defer fake.Close()
	oldTok, oldBase := codexTokenURL, CodexBase
	t.Cleanup(func() { codexTokenURL, CodexBase = oldTok, oldBase })
	codexTokenURL, CodexBase = fake.URL, fake.URL+"/backend-api/codex"

	ForgetAccounts()
	q := LoginUsage(context.Background(), "codex")["pro@example.com"]
	if q.Error == "" {
		t.Fatalf("the refused account read: %+v", q)
	}
	for _, cmd := range []string{"codex login", "CODEX_HOME"} {
		if strings.Contains(q.Error, cmd) {
			t.Errorf("the allowance line tells the user to run a command: %q", q.Error)
		}
	}
	lapsed := codexLapsed(t)["pro@example.com"]
	if !strings.Contains(lapsed, "sign in again") {
		t.Fatalf("the row isn't signed out, so it has no Sign in again: %q (read %q)", lapsed, q.Error)
	}

	// Sign in again: uni-switch's own sign-in (the callback's addLogin)
	again, err := codexLogin(idTok, fresh, "r-again", "acct-pro")
	if err != nil {
		t.Fatal(err)
	}
	if using, err := addLogin(again); err != nil || using {
		t.Fatalf("signed in again: using %v, %v", using, err)
	}
	b, _ := os.ReadFile(filepath.Join(backup, "auth.json"))
	var a codexAuth
	if json.Unmarshal(b, &a) != nil || a.Tokens.RefreshToken != "r-again" || a.Tokens.AccessToken != fresh {
		t.Fatalf("the new sign-in didn't reach the account's Codex folder: %s", b)
	}
	if now, _ := os.ReadFile(filepath.Join(home, ".codex", "auth.json")); string(now) != string(mainAuth) {
		t.Errorf("~/.codex/auth.json was rewritten: %s", now)
	}
	// the next look reads the folder again, and keeps the new sign-in
	ForgetAccounts()
	rememberLogins(true)
	users, _ := loginUsers(Logins("codex"))
	if strings.Join(users, ",") != "me@example.com,pro@example.com" {
		t.Fatalf("after signing in again: %v", users)
	}
	if l := codexLapsed(t)["pro@example.com"]; l != "" {
		t.Fatalf("signed in again, still said signed out: %q", l)
	}
	freshUsage()
	if q := LoginUsage(context.Background(), "codex")["pro@example.com"]; q.Error != "" || len(q.Windows) != 1 {
		t.Fatalf("read after signing in again: %+v", q)
	}
}

// A ChatGPT API account OpenAI no longer refreshes says so on its row,
// which then offers Sign in again: the lapse was kept and listed nowhere.
func TestSIWCLapsedListedOnItsRow(t *testing.T) {
	signIn(t)
	c := siwcCreds{ClientID: "c", HostID: "h", Email: "api@example.com", Access: "a", Refresh: "r", Expires: time.Now().Add(time.Hour)}
	auth, _ := json.Marshal(c)
	msg := "api@example.com's ChatGPT sign-in has expired — sign in again"
	if err := writeLogins([]savedLogin{{Agent: ChatGPTAPIID, User: "api@example.com", Auth: auth, Lapsed: msg}}); err != nil {
		t.Fatal(err)
	}
	ls := Logins(ChatGPTAPIID)
	if len(ls) != 1 || ls[0].Lapsed != msg {
		t.Fatalf("ChatGPT API rows: %+v", ls)
	}
}
