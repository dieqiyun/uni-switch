package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A revoked sign-in is unavailable for routing, while its auth.json stays.
// OpenAI auth remains required even then; a new sign-in restores the account.
// An unreadable revocation record still means unknown.
func TestCodexGoneSignInIsNoChatGPT(t *testing.T) {
	home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x","refresh_token":"rt-dead"}}`, "")
	dir := filepath.Join(home, ".codex")
	if !codexChatGPT(dir) {
		t.Fatal("a sign-in nothing was refused for is one")
	}
	p := provider.NoteCodexSignInGoneForTest(dir, "rt-dead")
	cx := codex(home)
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	cfg := read()
	if !strings.Contains(cfg, `model_provider = "uni-switch"`) {
		t.Fatalf("revoked sign-in: uni-switch its provider, not beside it:\n%s", cfg)
	}
	if !strings.Contains(cfg, "requires_openai_auth = true") {
		t.Fatalf("revoked sign-in must not disable OpenAI auth:\n%s", cfg)
	}
	if c := cx.Check(); c != "" {
		t.Fatal(c)
	}
	if _, err := os.Stat(filepath.Join(dir, "auth.json")); err != nil {
		t.Fatalf("the user's auth.json kept: %v", err)
	}

	// a record that doesn't parse: unknown, not signed out
	saved, _ := os.ReadFile(p)
	os.WriteFile(p, []byte("{cut off"), 0o600)
	if _, known := codexChatGPTKnown(dir); known {
		t.Fatal("an unreadable record taken as known")
	}
	os.WriteFile(p, saved, 0o600)

	// signed in again: the ChatGPT account back in the app
	os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{"tokens":{"access_token":"y","id_token":"y.e30.y","refresh_token":"rt-new"}}`), 0o600)
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, "requires_openai_auth = true") {
		t.Fatalf("signed in again: the ChatGPT account back (requires_openai_auth):\n%s", cfg)
	}
}
