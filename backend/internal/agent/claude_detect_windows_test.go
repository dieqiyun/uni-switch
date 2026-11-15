package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// claudeBinHome is a Windows home with no Claude Code folder, no
// CLAUDE_CONFIG_DIR and a PATH of uni-switch's own that has no claude on it:
// a uni-switch started before Claude Code was installed, or restarted by a
// self-update with the PATH it had then.
func claudeBinHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("PATH", t.TempDir())
	if a := claude(home); a.Detected() {
		t.Fatal("Claude Code listed with nothing of it here")
	}
	return home
}

// #1482 (edussss, Windows 11, uni-switch v0.1.1175): Claude Code wasn't listed
// on the Agents page. Detected asked uni-switch's own PATH only, which on
// Windows is the one uni-switch started with: Claude Code's installer (irm
// https://claude.ai/install.ps1 | iex) puts claude.exe in ~\.local\bin and
// adds that to the user's PATH in the registry, which a running uni-switch
// never sees, and npm's global folder is the same. The sign-in already
// found it there through proc.FindTool (#839); now the Agents page does
// too, with the CLI's path.
func TestClaudeFoundWhereWindowsInstallsPutIt(t *testing.T) {
	for _, tc := range []struct{ name, rel string }{
		{"the native installer's ~/.local/bin", `.local\bin\claude.exe`},
		{"npm's global folder", `AppData\Roaming\npm\claude.cmd`},
		{"Volta's", `AppData\Local\Volta\bin\claude.exe`},
		{"bun's", `.bun\bin\claude.exe`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := claudeBinHome(t)
			bin := filepath.Join(home, tc.rel)
			writeFile(t, bin, "")
			a := claude(home)
			if !a.Detected() {
				t.Fatalf("Claude Code at %s isn't listed", bin)
			}
			if got, _ := a.cliBin(); !sameFile(got, bin) {
				t.Fatalf("its CLI is %q, want %s", got, bin)
			}
		})
	}
	// installed after uni-switch started, in a folder the registry's PATH has
	// now (winget's Links, scoop's shims, an nvm-windows Node): a test's
	// registry folder lies under its sandbox (proc.SystemDirs)
	t.Run("the registry's PATH now", func(t *testing.T) {
		if os.Getenv(testenv.Marker) == "" {
			t.Skip("not in a sandbox: the registry's folders would be the machine's own")
		}
		home := claudeBinHome(t)
		dirs := proc.SystemDirs(proc.LoginPath()...)
		if len(dirs) == 0 {
			t.Skip("no PATH in this machine's registry")
		}
		bin := filepath.Join(dirs[len(dirs)-1], "claude.exe")
		writeFile(t, bin, "")
		t.Cleanup(func() { os.Remove(bin) })
		if !claude(home).Detected() {
			t.Fatalf("Claude Code at %s, on the registry's PATH, isn't listed", bin)
		}
	})
}

func sameFile(a, b string) bool {
	x, err1 := os.Stat(a)
	y, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(x, y)
}
