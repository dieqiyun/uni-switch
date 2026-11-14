package provider

import (
	"os"
	"path/filepath"
	"testing"
)

// A relay Claude Code was pointed at in its own settings.json is offered
// as a provider — also while Claude Code goes through uni-switch and the relay
// waits in the stash.
func TestImportClaudeSettings(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)                                  // Windows
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming")) // Windows
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	settings := filepath.Join(home, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(settings), 0o700)

	os.WriteFile(settings, []byte(`{"env":{"ANTHROPIC_BASE_URL":"https://api.relay.example.com/","ANTHROPIC_AUTH_TOKEN":"sk-r"},"model":"claude-sonnet-5"}`), 0o600)
	it := itemsOf(t, "claude-code")["settings"]
	if it.Skip != "" || it.Status != "new" || it.Provider.Anthropic != "https://api.relay.example.com" || it.Provider.Key != "sk-r" || it.Provider.Name != "api.relay.example.com" {
		t.Fatalf("relay: %+v", it)
	}

	os.WriteFile(settings, []byte(`{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:3425","ANTHROPIC_AUTH_TOKEN":"uni-switch"}}`), 0o600)
	os.MkdirAll(filepath.Dir(Path()), 0o700)
	os.WriteFile(filepath.Join(filepath.Dir(Path()), "stash.json"), []byte(`{"claude.base_url":"https://api.relay.example.com","claude.auth_token":"sk-r"}`), 0o600)
	if it := itemsOf(t, "claude-code")["settings"]; it.Skip != "" || it.Provider.Key != "sk-r" {
		t.Fatalf("stashed relay: %+v", it)
	}

	os.WriteFile(settings, []byte(`{"model":"opus"}`), 0o600)
	if items := itemsOf(t, "claude-code"); len(items) != 0 {
		t.Fatalf("Anthropic's own endpoint: %+v", items)
	}
}
