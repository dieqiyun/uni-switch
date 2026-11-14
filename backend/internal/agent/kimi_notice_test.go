package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Kimi Code 2.x reloads config.toml as it runs, and only its web and
// desktop app's model picker needs Settings › Providers opened again
// (jiabubu: the stale picker's pick was "not configured in config.toml"),
// so it isn't told to restart its sessions; kimi-cli, which reads the file
// at start-up, still is. On this machine and in a WSL distro.
func TestKimiNoticeByKind(t *testing.T) {
	was := Running
	Running = func(...string) bool { return true }
	t.Cleanup(func() { Running = was })
	t.Setenv("KIMI_SHARE_DIR", "")
	t.Setenv("KIMI_CODE_HOME", "")
	check := func(t *testing.T, where string, legacy bool, n string) {
		t.Helper()
		restart := strings.Contains(n, "restart open kimi sessions")
		picker := strings.Contains(n, "Settings › Providers")
		if legacy && (!restart || picker) || !legacy && (restart || !picker) {
			t.Errorf("%s, kimi-cli %v: %q", where, legacy, n)
		}
	}
	for _, c := range []struct {
		dir    string
		legacy bool
	}{{".kimi-code", false}, {".kimi", true}} {
		home := syncHome(t)
		os.MkdirAll(filepath.Join(home, c.dir), 0o755)
		check(t, "here", c.legacy, kimi(home).Notice())

		root := t.TempDir()
		os.MkdirAll(filepath.Join(root, "home", "me", c.dir), 0o755)
		k := wslKindOf("kimi")
		_, has, versions := wslFinds(k)
		d := distro{Name: "Ubuntu", Root: root, Home: "/home/me", Has: has, Versions: versions,
			Gateway: "172.20.0.1", Mirrored: true, Running: true}
		n := wslAgent(k, d).Notice()
		if !strings.HasPrefix(n, "Kimi Code in WSL Ubuntu ") {
			t.Errorf("WSL: %q", n)
		}
		check(t, "WSL", c.legacy, n)
	}
}
