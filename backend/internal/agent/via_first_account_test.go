package agent

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #1957 (parkavenue9639): Codex signed in to one account, another on in
// uni-switch. Claude Code's /model described every codex/gpt-* by the account
// Codex is signed in to, while uni-switch sent each request to the first in
// its own order: the other one when the signed-in one is paused, or when
// Codex is kept signed in to an account that isn't first. The note names
// the account a request goes to first.
func TestViaUniSwitchNamesTheAccountFirstInOrder(t *testing.T) {
	claims := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
	}
	auth := func(email, acct string) map[string]any {
		return map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
			"id_token":      claims(map[string]any{"email": email}),
			"access_token":  claims(map[string]any{"exp": time.Now().Add(time.Hour).Unix()}),
			"refresh_token": "r-" + acct, "account_id": acct}}
	}
	me, _ := json.Marshal(auth("me@example.com", "acct-1"))
	home, _ := codexHome(t, string(me), "model = \"gpt-5.5\"\n")
	os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"), []byte(`{"models":[{"slug":"gpt-5.5","display_name":"GPT-5.5","priority":1}]}`), 0o644)
	b, _ := json.Marshal([]map[string]any{{"agent": "codex", "user": "spare@example.com", "on": true,
		"seen": time.Now(), "auth": auth("spare@example.com", "acct-2")}})
	os.MkdirAll(filepath.Dir(provider.Path()), 0o755)
	os.WriteFile(filepath.Join(filepath.Dir(provider.Path()), "logins.json"), b, 0o600)
	provider.ForgetAccounts()
	t.Cleanup(provider.ForgetAccounts)
	note := func() string {
		t.Helper()
		for _, o := range viaUniSwitch("claude", "") {
			if o.Value == "codex/gpt-5.5" {
				return o.Note
			}
		}
		t.Fatal("no codex/gpt-5.5 among Claude Code's models via uni-switch")
		return ""
	}
	if got := note(); got != "me@example.com · via uni-switch" {
		t.Fatalf("signed in to the first: %q", got)
	}
	// kept signed in to spare, me still first in the order
	if err := provider.SetKeepLoginAs("codex", "spare@example.com"); err != nil {
		t.Fatal(err)
	}
	provider.ForgetAccounts()
	if got := note(); got != "me@example.com · via uni-switch" {
		t.Fatalf("kept signed in to spare, me first: %q", got)
	}
	// let go, Codex is on me again; me paused, spare takes its requests
	if err := provider.SetKeepLogin("codex", false); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetLoginOn("codex", "me@example.com", false); err != nil {
		t.Fatal(err)
	}
	provider.ForgetAccounts()
	if got := note(); got != "spare@example.com · via uni-switch" {
		t.Fatalf("signed in to me, paused: %q", got)
	}
}
