package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/desktopdir"
)

// cybercaius (Discord, Windows): Claude Code and Desktop uninstalled, only
// Desktop installed again, and the Agents page still listed Claude Code by
// its ~\.claude with nothing saying why. Desktop includes a Claude Code
// for its Code tab (%APPDATA%\Claude\claude-code\<version>\claude.exe) that
// reads the same ~\.claude\settings.json, so with no Claude Code of its own
// found, the CLI says it is Desktop's, with that copy's version once
// Desktop has fetched it; one of its own found, it is that one.
func TestClaudeCodeInDesktop(t *testing.T) {
	old := desktopdir.OS
	desktopdir.OS = "windows"
	t.Cleanup(func() { desktopdir.OS = old })
	home, _ := desktopSandbox(t)
	t.Setenv("PATH", t.TempDir())
	versions, latests = memo{}, memo{}
	t.Cleanup(func() { versions, latests = memo{}, memo{} })
	file(t, filepath.Join(home, ".claude", "settings.json"), `{}`)
	a := claude(home)

	if c, ok := a.CLI(); ok || c.InDesktop {
		t.Fatalf("neither Claude Code nor Desktop here: %+v", c)
	}
	// Desktop installed (Squirrel's setup), its Code tab not used yet
	file(t, filepath.Join(home, "AppData", "Local", "AnthropicClaude", "claude.exe"), "MZ")
	if c, ok := a.CLI(); !ok || !c.InDesktop || c.DesktopVersion != "" || c.Version != "" {
		t.Fatalf("Desktop installed: %+v %v", c, ok)
	}
	// and the Claude Code it fetched, two versions, one in its 3p mode's folder
	file(t, filepath.Join(home, "AppData", "Roaming", "Claude", "claude-code", "2.1.300", "claude.exe"), "MZ")
	file(t, filepath.Join(home, "AppData", "Local", "Claude-3p", "claude-code", "2.1.310", "claude.exe"), "MZ")
	if c, ok := a.CLI(); !ok || !c.InDesktop || c.DesktopVersion != "2.1.310" {
		t.Fatalf("Desktop's Claude Code: %+v %v", c, ok)
	}
	// Claude Code's own on PATH: that one, not Desktop's
	bin := t.TempDir()
	file(t, filepath.Join(bin, exe("claude")), "")
	os.Chmod(filepath.Join(bin, exe("claude")), 0o755)
	t.Setenv("PATH", bin)
	if c, _ := a.CLI(); c.InDesktop || c.Path == "" {
		t.Fatalf("its own on PATH: %+v", c)
	}
	// in a WSL distro, never Desktop's
	w := claude(home)
	w.WSL = "Ubuntu"
	t.Setenv("PATH", t.TempDir())
	if c, _ := w.CLI(); c.InDesktop {
		t.Fatalf("WSL: %+v", c)
	}
}
