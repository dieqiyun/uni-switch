package library

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// fakeRTKAgy acts as rtk 0.51's rtk init -g --agent antigravity does
// (src/hooks/init/antigravity.rs): a plugin of its own in
// ~/.gemini/config/plugins/rtk, with the hooks.json it writes word for
// word. Any other arguments (--auto-patch among them) are refused.
const fakeRTKAgy = `#!/bin/sh
case "$1" in
--version) echo "rtk $RTK_V"; exit 0 ;;
gain) exit 1 ;;
esac
shift 2
[ "$*" = "--agent antigravity" ] || { echo "unknown: $*" >&2; exit 2; }
d="$HOME/.gemini/config/plugins/rtk"
/bin/mkdir -p "$d/rules"
echo "rtk rules" > "$d/rules/AGENTS.md"
/bin/cat > "$d/hooks.json" <<'J'
{
  "rtk-rewrite": {
    "enabled": true,
    "PreToolUse": [
      {
        "matcher": "run_command",
        "hooks": [
          {
            "type": "command",
            "command": "rtk hook antigravity",
            "timeout": 10
          }
        ]
      }
    ]
  }
}
J
echo '{"name": "rtk", "version": "1.0.0"}' > "$d/plugin.json"
`

// #2202 (Chichengmo): Antigravity CLI wasn't on the RTK page at all,
// though rtk 0.51 has an Antigravity agent. It is listed, switched on with
// rtk's installer, switched off by uni-switch taking rtk's plugin out (and only
// it), and an rtk before 0.51, which has no such agent, isn't run for it.
func TestRTKAntigravity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake rtk is a shell script")
	}
	h := sandbox(t)
	bin := filepath.Join(h, "bin")
	testenv.Program(t, filepath.Join(bin, "rtk"), fakeRTKAgy)
	t.Setenv("PATH", bin)
	// Antigravity CLI is here by its folder
	write(t, filepath.Join(h, ".gemini", "antigravity-cli", "settings.json"), "{}")
	// another plugin of the user's beside rtk's stays
	mine := filepath.Join(h, ".gemini", "config", "plugins", "mine", "plugin.json")
	write(t, mine, `{"name":"mine"}`)
	plugin := filepath.Join(h, ".gemini", "config", "plugins", "rtk")
	row := func() *RTKAgent {
		t.Helper()
		for _, a := range ReadRTK().Agents {
			if a.ID == "agy" {
				return &a
			}
		}
		t.Fatal("Antigravity CLI isn't on the RTK page")
		return nil
	}

	t.Setenv("RTK_V", "0.50.0")
	if a := row(); !strings.Contains(a.Blocked, "0.51") || a.On || a.NoHook {
		t.Fatalf("rtk 0.50: %+v", a)
	}
	if _, err := SetRTK("agy", true); err == nil || !strings.Contains(err.Error(), "RTK 0.51 or newer") {
		t.Fatalf("switched on with rtk 0.50: %v", err)
	}
	if exists(plugin) {
		t.Fatal("rtk 0.50's installer was run for Antigravity")
	}

	t.Setenv("RTK_V", "0.51.0")
	if a := row(); a.Blocked != "" || a.On {
		t.Fatalf("rtk 0.51 before switching on: %+v", a)
	}
	if _, err := SetRTK("agy", true); err != nil {
		t.Fatal(err)
	}
	if a := row(); !a.On {
		t.Fatal("Antigravity CLI isn't seen with rtk on")
	}
	if id, err := RTKTakes("agy"); err != nil || id != "agy" {
		t.Fatalf("RTKTakes(agy) = %q, %v", id, err)
	}
	if _, err := SetRTK("agy", false); err != nil {
		t.Fatal(err)
	}
	if exists(plugin) {
		t.Error("rtk's plugin is still in ~/.gemini/config/plugins")
	}
	if !exists(mine) {
		t.Error("the user's own plugin was taken out with rtk's")
	}
	if a := row(); a.On {
		t.Fatal("Antigravity CLI still seen with rtk on")
	}
	os.Remove(mine)
}
