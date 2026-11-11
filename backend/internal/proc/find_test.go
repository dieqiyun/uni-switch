package proc

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// A tool off PATH is found in the folders it is installed in, by its
// Windows name too (#839: ~/.local/bin/claude.exe was looked for as
// ~/.local/bin/claude, so Windows' Claude sign-in said none was installed).
func TestToolIn(t *testing.T) {
	d := t.TempDir()
	empty, bin, npm := filepath.Join(d, "empty"), filepath.Join(d, "bin"), filepath.Join(d, "npm")
	for _, x := range []string{empty, bin, npm} {
		os.MkdirAll(x, 0o755)
	}
	os.WriteFile(filepath.Join(bin, "claude.exe"), nil, 0o755)
	os.WriteFile(filepath.Join(npm, "codex.cmd"), nil, 0o755)
	os.MkdirAll(filepath.Join(npm, "claude"), 0o755) // a folder isn't the tool
	if got := toolIn("claude", "windows", []string{"", empty, npm, bin}); got != filepath.Join(bin, "claude.exe") {
		t.Errorf("windows claude: %q", got)
	}
	if got := toolIn("codex", "windows", []string{bin, npm}); got != filepath.Join(npm, "codex.cmd") {
		t.Errorf("windows codex: %q", got)
	}
	if got := toolIn("claude", "darwin", []string{bin, npm}); got != "" {
		t.Errorf("darwin: %q", got)
	}
	os.WriteFile(filepath.Join(bin, "claude"), nil, 0o755)
	if got := toolIn("claude", "linux", []string{empty, bin}); got != filepath.Join(bin, "claude") {
		t.Errorf("linux: %q", got)
	}
}

// A test that empties PATH means a machine without the tool: the folders
// of the machine's own, which the host's Homebrew fills, lie under the
// test's home, and a tool put there is still found (#1525: the host's
// /opt/homebrew/bin/claude was found in tests of a machine without one).
func TestSystemDirsLieInTheTestsHome(t *testing.T) {
	if sandboxVar != testenv.Marker {
		t.Fatalf("sandboxVar %q isn't testenv.Marker %q", sandboxVar, testenv.Marker)
	}
	home := os.Getenv(testenv.Marker)
	t.Setenv("PATH", t.TempDir())
	for _, n := range []string{"claude", "codex", "gemini", "npm", "node", "opencode", "kiro-cli", "devin", "cursor-agent"} {
		if p := FindTool(n); p != "" {
			t.Errorf("with an empty PATH, FindTool(%q) = %q", n, p)
		}
	}
	brew := SystemDirs("/opt/homebrew/bin")[0]
	if rel, err := filepath.Rel(home, brew); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("SystemDirs(/opt/homebrew/bin) = %q, outside the test home %q", brew, home)
	}
	if err := os.MkdirAll(brew, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Join(home, "system")) })
	want := filepath.Join(brew, "claude")
	os.WriteFile(want, nil, 0o755)
	if runtime.GOOS != "windows" {
		if got := FindTool("claude"); got != want {
			t.Errorf("Homebrew's claude in the test's home: FindTool = %q, want %q", got, want)
		}
	}

	t.Setenv(sandboxVar, "") // outside a test: the machine's own folders
	if got := SystemDirs("/opt/homebrew/bin", "", "/usr/local/bin"); !slices.Equal(got, []string{"/opt/homebrew/bin", "", "/usr/local/bin"}) {
		t.Errorf("SystemDirs outside a test = %q", got)
	}
}
