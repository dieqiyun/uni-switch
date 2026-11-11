package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Forced API login still determines routing and is left intact. It no
// longer disables the maintainer's required OpenAI auth provider flag.
func TestCodexForcedAPILoginIsNoChatGPT(t *testing.T) {
	home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, "forced_login_method = \"api\"\n")
	cx := codex(home)
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	cfg := read()
	if !strings.Contains(cfg, `model_provider = "uni-switch"`) {
		t.Fatalf("forced API login: uni-switch its provider, not beside a sign-in Codex won't use:\n%s", cfg)
	}
	if !strings.Contains(cfg, "requires_openai_auth = true") {
		t.Fatalf("forced API login must not disable OpenAI auth:\n%s", cfg)
	}
	if !strings.Contains(cfg, `forced_login_method = "api"`) {
		t.Fatalf("the user's forced_login_method kept:\n%s", cfg)
	}
	if c := cx.Check(); c != "" {
		t.Fatal(c)
	}

	// the user takes the line out to sign in to ChatGPT again
	p := filepath.Join(home, ".codex", "config.toml")
	if err := os.WriteFile(p, []byte(strings.Replace(cfg, "forced_login_method = \"api\"\n", "", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, "requires_openai_auth = true") {
		t.Fatalf("line gone: the ChatGPT account back in the app (requires_openai_auth):\n%s", cfg)
	}

	// "chatgpt" forced, or none: the sign-in counts as before
	for _, c := range []string{"forced_login_method = \"chatgpt\"\n", ""} {
		home, _ := codexHome(t, `{"tokens":{"access_token":"x"}}`, c)
		if !codexChatGPT(filepath.Join(home, ".codex")) {
			t.Fatalf("config %q: a ChatGPT sign-in", c)
		}
	}
}
