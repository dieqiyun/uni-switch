package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Disconnected on the group's model name, Codex is taken off it as off the
// group's id: back to its default, not left asking OpenAI for gpt-6.1-sol.
func TestCodexGroupByItsModelNameDisconnect(t *testing.T) {
	home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, "")
	for _, id := range []string{"ra", "rb"} {
		if err := provider.Save(provider.Provider{ID: id, Name: id, Chat: "https://" + id + ".example/v1", Key: "k", Models: []string{"gpt-6.1-sol"}}); err != nil {
			t.Fatal(err)
		}
	}
	cx := codex(home)
	if err := cx.Apply("model", "group/auto-gpt-6-1-sol"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".codex", "config.toml")
	os.WriteFile(path, []byte(strings.Replace(read(), `model = "auto/gpt-6.1-sol"`, `model = "gpt-6.1-sol"`, 1)), 0o644)
	if err := cx.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); strings.Contains(cfg, "gpt-6.1-sol") || strings.Contains(cfg, "openai_base_url") || cx.Wired() {
		t.Fatalf("disconnected:\n%s", cfg)
	}
}
