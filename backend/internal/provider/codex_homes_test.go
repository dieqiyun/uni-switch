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
	"sync"
	"testing"
	"time"
)

// Codex keeps one ChatGPT sign-in a folder (codex-rs login/src/auth/
// storage.rs: $CODEX_HOME/auth.json), so a second account is Codex run
// with CODEX_HOME=~/.codex-work. phantom3792 on X: uni-switch read only the
// default folder's account, and an account added from another folder read
// no quota. This is that layout: ~/.codex signed in to me@example.com (as
// signIn writes it), ~/.codex-work to work@example.com with an access
// token about to run out, and logins.json holding the copy an earlier add
// made, whose refresh token the vendor has since refused.
func TestCodexOtherHomeAccountReadsQuota(t *testing.T) {
	home := signIn(t)
	t.Setenv("CODEX_HOME", "")
	resetLoginUsage(t)
	work := filepath.Join(home, ".codex-work")
	soon := float64(time.Now().Add(time.Minute).Unix())
	idTok := fakeJWT(map[string]any{"email": "work@example.com",
		"https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "plus", "chatgpt_account_id": "acct-work"}})
	writeFile(t, filepath.Join(work, "auth.json"), map[string]any{
		"auth_mode": "chatgpt", "OPENAI_API_KEY": nil, "last_refresh": "2026-10-20T00:00:00Z",
		"tokens": map[string]any{"id_token": idTok, "access_token": fakeJWT(map[string]any{"exp": soon}),
			"refresh_token": "r-work", "account_id": "acct-work"},
	})
	// the copy an earlier add kept, its refresh token spent since
	dead, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt",
		"tokens": map[string]any{"id_token": idTok, "access_token": fakeJWT(map[string]any{"exp": soon}),
			"refresh_token": "r-dead", "account_id": "acct-work"}})
	if err := writeLogins([]savedLogin{{Agent: "codex", User: "work@example.com", Plan: "plus", Auth: dead}}); err != nil {
		t.Fatal(err)
	}
	mainAuth, _ := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))

	fresh := fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix()), "n": "work-2"})
	var mu sync.Mutex
	var refreshed []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/backend-api/wham/usage" {
			if r.Header.Get("Authorization") != "Bearer "+fresh {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			io.WriteString(w, `{"plan_type":"plus","rate_limit":{"allowed":true,"primary_window":{"used_percent":40,"limit_window_seconds":18000,"reset_after_seconds":600}}}`)
			return
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		refreshed = append(refreshed, body["refresh_token"])
		mu.Unlock()
		if body["refresh_token"] != "r-work" {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":"refresh_token_reused"}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"id_token": idTok, "access_token": fresh, "refresh_token": "r-work-2"})
	}))
	defer fake.Close()
	oldTok, oldBase := codexTokenURL, CodexBase
	t.Cleanup(func() { codexTokenURL, CodexBase = oldTok, oldBase })
	codexTokenURL, CodexBase = fake.URL, fake.URL+"/backend-api/codex"

	ForgetAccounts()
	var got *Login
	for _, l := range Logins("codex") {
		if l.User == "work@example.com" {
			l := l
			got = &l
		}
	}
	if got == nil || got.Active {
		t.Fatalf("the ~/.codex-work account isn't listed as a saved one: %+v", Logins("codex"))
	}
	q := LoginUsage(context.Background(), "codex")["work@example.com"]
	if q.Error != "" || len(q.Windows) == 0 {
		t.Fatalf("no quota for the ~/.codex-work account: %+v (refreshed with %v)", q, refreshed)
	}
	mu.Lock()
	for _, r := range refreshed {
		if r == "r-dead" {
			t.Errorf("refreshed with the spent copy's token: %v", refreshed)
		}
	}
	mu.Unlock()
	// renewed where Codex keeps it: Codex there goes on with the new one
	b, _ := os.ReadFile(filepath.Join(work, "auth.json"))
	var a codexAuth
	if json.Unmarshal(b, &a) != nil || a.Tokens.RefreshToken != "r-work-2" || a.Tokens.AccessToken != fresh {
		t.Errorf("~/.codex-work/auth.json not renewed in place: %s", b)
	}
	// never written into ~/.codex: one sign-in in two places signs one out
	if err := SwitchLogin("codex", "work@example.com"); err == nil || !strings.Contains(err.Error(), work) {
		t.Errorf("switch to the other folder's account: %v", err)
	}
	if now, _ := os.ReadFile(filepath.Join(home, ".codex", "auth.json")); string(now) != string(mainAuth) {
		t.Errorf("~/.codex/auth.json was rewritten: %s", now)
	}
	// removed from uni-switch, ~/.codex-work left as it is
	// (TestCodexHeldFolderAccountRemoved)
	if err := ForgetLogin("codex", "work@example.com"); err != nil {
		t.Errorf("remove the other folder's account: %v", err)
	}
}

// The same account signed in in ~/.codex and in another folder is
// ~/.codex's, listed once.
func TestCodexOtherHomeSameAccountListedOnce(t *testing.T) {
	home := signIn(t)
	t.Setenv("CODEX_HOME", "")
	b, _ := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err := os.MkdirAll(filepath.Join(home, ".codex-copy"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex-copy", "auth.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	ForgetAccounts()
	ls := Logins("codex")
	if len(ls) != 1 || !ls[0].Active || ls[0].home != "" {
		t.Fatalf("logins %+v", ls)
	}
}
