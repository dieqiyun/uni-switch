package library

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// cmdExe is found before a test's sandbox takes PATH away.
var cmdExe, _ = exec.LookPath("cmd")

// folderLink makes path a link to the folder target: a junction on
// Windows, which Go lists as irregular rather than as a folder or a
// symlink, and a symlink elsewhere.
func folderLink(t *testing.T, target, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if out, err := exec.Command(cmdExe, "/c", "mklink", "/J", path, target).CombinedOutput(); err != nil {
			t.Fatalf("mklink /J: %v %s", err, out)
		}
		return
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

// A Claude Desktop org's or account's skills-plugin folder that is a link
// (a junction on Windows) is still one of Desktop's skill roots (#2268).
func TestClaudeDesktopSkillRootsBehindALink(t *testing.T) {
	sandbox(t)
	dir := t.TempDir()
	base := filepath.Join(dir, "local-agent-mode-sessions", "skills-plugin")
	org := desktopAccount(t, dir, "org1", "acct1", `{"skills": []}`)
	acct := desktopAccount(t, dir, "org2", "acct2", `{"skills": []}`)
	if got := desktopSkillRoots(dir); !slices.Contains(got, org) || !slices.Contains(got, acct) {
		t.Fatalf("roots %v, want %s and %s", got, org, acct)
	}
	for _, live := range []string{filepath.Join(base, "org1"), acct} {
		elsewhere := filepath.Join(t.TempDir(), filepath.Base(live))
		if err := os.Rename(live, elsewhere); err != nil {
			t.Fatal(err)
		}
		folderLink(t, elsewhere, live)
	}
	if got := desktopSkillRoots(dir); !slices.Contains(got, org) || !slices.Contains(got, acct) {
		t.Fatalf("roots behind links %v, want %s and %s", got, org, acct)
	}
}
