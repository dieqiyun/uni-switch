package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// With CLAUDE_CONFIG_DIR set, Claude Code keeps everything there and never
// makes ~/.claude: it is listed on the Agents page by that folder, a model
// picked for it goes to $CLAUDE_CONFIG_DIR/settings.json, the file it
// reads, and uni-switch reads the pick back from there. The sessions, the
// library and the sign-in already read $CLAUDE_CONFIG_DIR while the
// Agents page looked for ~/.claude alone, so a Claude Code kept elsewhere
// wasn't found (#1482).
func TestClaudeConfigDirTakesThePick(t *testing.T) {
	home, _ := claudeDottedHome(t, provider.Provider{ID: "relay", Name: "Relay", Anthropic: "https://example.test", Key: "k",
		Models: []string{"claude-sonnet-4.5", "glm-4.6"}}, `{}`)
	if err := os.RemoveAll(filepath.Join(home, ".claude")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir()) // no claude on PATH: only its folder tells
	cd := filepath.Join(t.TempDir(), "claude-config")
	writeFile(t, filepath.Join(cd, "settings.json"), `{"theme": "dark"}`)
	t.Setenv("CLAUDE_CONFIG_DIR", cd)

	a := claude(home)
	if a.Path != filepath.Join(cd, "settings.json") || a.Dir != cd {
		t.Fatalf("Claude Code's files at %s (%s), want under %s", a.Path, a.Dir, cd)
	}
	if !a.Detected() {
		t.Fatal("Claude Code kept in CLAUDE_CONFIG_DIR isn't listed")
	}
	if err := a.Field("model").Set("relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cd, "settings.json")
	if v, _ := edit.GetJSON(path, "env.ANTHROPIC_BASE_URL"); v != gateway.URL() {
		b, _ := os.ReadFile(path)
		t.Fatalf("$CLAUDE_CONFIG_DIR/settings.json doesn't point at uni-switch:\n%s", b)
	}
	if v, _ := edit.GetJSON(path, "theme"); v != "dark" {
		t.Fatalf("the user's own theme is %q after the pick", v)
	}
	if got := claude(home).Field("model").Get(); got != "relay/glm-4.6" {
		t.Fatalf("uni-switch reads the pick back as %q", got)
	}
	if m := StandIn("claude", "claude-sonnet-4-5"); m == "" {
		t.Fatal("the gateway has no stand-in for Claude Code: read from somewhere else")
	}
	if err := TuneApply(home, "claude", TuneCache, "", "1h"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "promptCacheTtl") {
		t.Fatalf("tune didn't write $CLAUDE_CONFIG_DIR/settings.json:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("~/.claude written with CLAUDE_CONFIG_DIR set: %v", err)
	}
}
