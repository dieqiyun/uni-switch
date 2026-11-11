package agent

import (
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #2367 (Dony2050): Codex keeping its ChatGPT sign-in in the macOS
// keychain has no auth.json. It is wired and told as signed in to ChatGPT
// all the same, from the item (a fake: no test reads the real keychain);
// an API key kept there is told as a key.
func TestCodexSignInInKeychain(t *testing.T) {
	home, _ := codexHome(t, "", "cli_auth_credentials_store = \"keyring\"\n")
	dir := filepath.Join(home, ".codex")
	item := ""
	defer provider.CodexKeychainForTest(func(service, account string) ([]byte, error) {
		if service != "Codex Auth" || item == "" {
			return nil, fs.ErrNotExist
		}
		return []byte(item), nil
	})()
	a := &Agent{ID: "codex", Path: filepath.Join(dir, "config.toml")}
	if codexChatGPT(dir) || a.Source() != "" {
		t.Fatalf("no item: signed in (%q)", a.Source())
	}
	item = `{"auth_mode":"chatgpt","tokens":{"access_token":"x","account_id":"acc-1"}}`
	provider.ForgetAccounts()
	if !codexChatGPT(dir) || a.Source() != "chatgpt" {
		t.Fatalf("the item's ChatGPT sign-in wasn't seen: %q", a.Source())
	}
	item = `{"auth_mode":"apikey","OPENAI_API_KEY":"sk-x"}`
	provider.ForgetAccounts()
	if a.Source() != "key" {
		t.Fatalf("the item's key wasn't seen: %q", a.Source())
	}
}
