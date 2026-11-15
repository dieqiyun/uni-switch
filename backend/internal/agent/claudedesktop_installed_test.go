package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/desktopdir"
)

// Claude Desktop installed on Windows and not yet opened has none of its
// folders, so it wasn't listed (#1482): Squirrel's setup has put
// %LOCALAPPDATA%\AnthropicClaude\claude.exe there, the MSIX its package
// folder. Either is Desktop installed; a leftover AnthropicClaude folder
// without its claude.exe is not.
func TestClaudeDesktopInstalledNotOpened(t *testing.T) {
	old := desktopdir.OS
	desktopdir.OS = "windows"
	t.Cleanup(func() { desktopdir.OS = old })
	for _, tc := range []struct {
		name string
		make []string
		dir  bool
		want bool
	}{
		{"nothing", nil, false, false},
		{"Squirrel's setup", []string{"AnthropicClaude", "claude.exe"}, false, true},
		{"an uninstall's leftover", []string{"AnthropicClaude", "packages"}, true, false},
		{"the MSIX", []string{"Packages", "Claude_pzs8sxrjxfjjc"}, true, true},
		{"the Store's MSIX", []string{"Packages", "AnthropicPBC.Claude_fnn82j28hfe8t"}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, _ := desktopSandbox(t)
			local := filepath.Join(home, "AppData", "Local")
			if tc.make != nil {
				p := filepath.Join(append([]string{local}, tc.make...)...)
				if tc.dir {
					os.MkdirAll(p, 0o755)
				} else {
					desktopWrite(t, p, "")
				}
			}
			if got := claudeDesktop(home).Detected(); got != tc.want {
				t.Fatalf("Claude Desktop listed: %v, want %v", got, tc.want)
			}
		})
	}
}
