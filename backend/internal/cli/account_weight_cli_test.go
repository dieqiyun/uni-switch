package cli

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// uni-switch provider account-weight gives a subscription account its share
// of the requests under routing=weight (rubbzha on X: a Max account 5, a
// Pro one 1), default takes it back to 1; a weight out of 1–1000, an
// account the subscription hasn't, or a provider with keys is an error.
func TestAccountWeightCmd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("DSH_HOME", "")
	jwt := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
	}
	auth, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
		"id_token":      jwt(map[string]any{"email": "me@example.com"}),
		"access_token":  jwt(map[string]any{"exp": time.Now().Add(time.Hour).Unix()}),
		"refresh_token": "r", "account_id": "acct-1"}})
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	if err := os.WriteFile(filepath.Join(home, ".codex", "auth.json"), auth, 0o600); err != nil {
		t.Fatal(err)
	}
	provider.ForgetAccounts()
	t.Cleanup(provider.ForgetAccounts)
	weight := func() int {
		p, err := provider.Find("codex")
		if err != nil {
			t.Fatal(err)
		}
		return p.AccountWeight("me@example.com")
	}

	if err := providerCmd([]string{"provider", "account-weight", "codex", "Me@Example.com", "5"}); err != nil {
		t.Fatal(err)
	}
	if w := weight(); w != 5 {
		t.Fatalf("weight %d", w)
	}
	for _, args := range [][]string{{"provider", "account-weight", "codex"}, {"provider", "account-weight", "codex", "me@example.com"}} {
		if err := providerCmd(args); err != nil {
			t.Fatal(err)
		}
	}
	for _, bad := range []string{"1.5", "1001", "-2", "lots"} {
		if err := providerCmd([]string{"provider", "account-weight", "codex", "me@example.com", bad}); err == nil {
			t.Fatalf("%s taken", bad)
		}
	}
	if w := weight(); w != 5 {
		t.Fatalf("a refused weight changed it to %d", w)
	}
	if err := providerCmd([]string{"provider", "account-weight", "codex", "nobody@example.com", "2"}); err == nil {
		t.Fatal("an account it hasn't was weighed")
	}
	if err := providerCmd([]string{"provider", "account-weight", "codex", "me@example.com", "default"}); err != nil {
		t.Fatal(err)
	}
	if w := weight(); w != 1 {
		t.Fatalf("default left %d", w)
	}
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k1", Chat: "http://127.0.0.1:1/v1"}); err != nil {
		t.Fatal(err)
	}
	if err := providerCmd([]string{"provider", "account-weight", "relay", "k1", "3"}); err == nil {
		t.Fatal("a key provider's account was weighed")
	}
}
