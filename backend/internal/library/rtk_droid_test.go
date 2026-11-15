package library

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// fakeRTKDroid acts as rtk 1.1.0's rtk init -g --agent droid does
// (src/hooks/init/droid.rs) where Droid has no hooks yet: it writes the
// root hooks.json in $FACTORY_HOME_OVERRIDE/.factory, else ~/.factory, with
// rtk's Execute group word for word. It notes the FACTORY_HOME_OVERRIDE it
// was given. Any other arguments (--auto-patch among them) are refused.
const fakeRTKDroid = `#!/bin/sh
case "$1" in
--version) echo "rtk $RTK_V"; exit 0 ;;
gain) exit 1 ;;
esac
shift 2
[ "$*" = "--agent droid" ] || { echo "unknown: $*" >&2; exit 2; }
echo "$FACTORY_HOME_OVERRIDE" > "$HOME/given"
d="${FACTORY_HOME_OVERRIDE:-$HOME}/.factory"
/bin/mkdir -p "$d"
/bin/cat > "$d/hooks.json" <<'J'
{
  "PreToolUse": [
    {
      "matcher": "Execute",
      "hooks": [
        {
          "type": "command",
          "command": "rtk hook droid"
        }
      ]
    }
  ]
}
J
echo "Factory Droid hook registered (global)."
`

func droidRow(t *testing.T, id string) *RTKAgent {
	t.Helper()
	for _, a := range ReadRTK().Agents {
		if a.ID == id {
			return &a
		}
	}
	return nil
}

// lml0664 on Discord: the RTK page offered a fixed few agents, though rtk
// 1.1.0 has a hook for Factory's Droid too. Droid is listed, switched on
// with rtk's own installer (told Droid's folder), and switched off by
// uni-switch taking rtk's group out of whichever of Droid's hook files has it,
// leaving the user's own hooks and settings; an rtk before 0.44, which has
// no Droid agent, isn't run for it.
func TestRTKDroid(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake rtk is a shell script")
	}
	h := sandbox(t)
	bin := filepath.Join(h, "bin")
	testenv.Program(t, filepath.Join(bin, "rtk"), fakeRTKDroid)
	holdCLI(t, h, "droid")
	t.Setenv("PATH", bin)
	for _, c := range []struct{ name, override string }{
		{"home", ""},
		// Droid's own FACTORY_HOME_OVERRIDE: its folder isn't ~/.factory
		{"override", filepath.Join(h, "elsewhere")},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("FACTORY_HOME_OVERRIDE", c.override)
			root := h
			if c.override != "" {
				root = c.override
			}
			dir := filepath.Join(root, ".factory")
			settings := filepath.Join(dir, "settings.json")
			mine := `{"sessionDefaultSettings": {"model": "x"}}`
			write(t, settings, mine)
			hooks := filepath.Join(dir, "hooks.json")

			t.Setenv("RTK_V", "0.43.0")
			a := droidRow(t, "droid")
			if a == nil {
				t.Fatal("Droid isn't on the RTK page")
			}
			if !strings.Contains(a.Blocked, "0.44") || a.On || a.NoHook {
				t.Fatalf("rtk 0.43: %+v", a)
			}
			if _, err := SetRTK("droid", true); err == nil || !strings.Contains(err.Error(), "RTK 0.44 or newer") {
				t.Fatalf("switched on with rtk 0.43: %v", err)
			}
			if exists(hooks) {
				t.Fatal("rtk 0.43's installer was run for Droid")
			}

			t.Setenv("RTK_V", "1.1.0")
			if a := droidRow(t, "droid"); a.Blocked != "" || a.On {
				t.Fatalf("rtk 1.1 before switching on: %+v", a)
			}
			if _, err := SetRTK("droid", true); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(read(t, filepath.Join(h, "given"))); got != root {
				t.Errorf("rtk was given FACTORY_HOME_OVERRIDE=%q, want %q", got, root)
			}
			if !contains(hooks, "rtk hook droid") {
				t.Fatalf("no hook in %s", hooks)
			}
			if a := droidRow(t, "droid"); !a.On {
				t.Fatal("Droid isn't seen with rtk on")
			}
			if id, err := RTKTakes("droid"); err != nil || id != "droid" {
				t.Fatalf("RTKTakes(droid) = %q, %v", id, err)
			}
			if _, err := SetRTK("droid", false); err != nil {
				t.Fatal(err)
			}
			if contains(hooks, "rtk hook droid") {
				t.Error("rtk's hook is still in hooks.json")
			}
			if a := droidRow(t, "droid"); a.On {
				t.Fatal("Droid still seen with rtk on")
			}
			if got := read(t, settings); got != mine {
				t.Errorf("settings.json = %q", got)
			}
		})
	}
}

// rtk puts its group into settings.json's hooks when that is where Droid's
// PreToolUse hooks already are; taking it out there leaves the user's own
// hook in the same Execute list, and their other settings.
func TestRTKDroidSettingsHooks(t *testing.T) {
	h := sandbox(t)
	holdCLI(t, h, "droid")
	t.Setenv("FACTORY_HOME_OVERRIDE", "")
	settings := filepath.Join(h, ".factory", "settings.json")
	write(t, settings, `{
  "sessionDefaultSettings": {"model": "x"},
  "hooks": {
    "PreToolUse": [
      {"matcher": "Execute", "hooks": [
        {"type": "command", "command": "my-guard"},
        {"type": "command", "command": "rtk hook droid"}
      ]}
    ]
  }
}`)
	if a := droidRow(t, "droid"); a == nil || !a.On {
		t.Fatalf("Droid with rtk's hook in settings.json: %+v", a)
	}
	if _, err := SetRTK("droid", false); err != nil {
		t.Fatal(err)
	}
	if contains(settings, "rtk hook droid") || !contains(settings, "my-guard") || !contains(settings, `"model"`) {
		t.Fatalf("settings.json after switching off: %s", read(t, settings))
	}
}

// Agents RTK has no global hook for are listed with their own reason, and
// never switched on: Qoder and Qoder CN (no Qoder agent in rtk 1.1.0,
// rtk-ai/rtk#1327), Cline (one project's .clinerules only, #3841) and Kimi
// Code (one project's AGENTS.md only; its hooks can't rewrite).
func TestRTKNoHookQoderClineKimi(t *testing.T) {
	h := sandbox(t)
	for _, b := range []string{"qodercli", "qoderclicn", "cline", "kimi"} {
		holdCLI(t, h, b)
	}
	for _, c := range []struct{ id, why string }{
		{"qoder", "no Qoder agent"},
		{"qoder-cn", "no Qoder agent"},
		{"cline", ".clinerules"},
		{"kimi", "AGENTS.md"},
	} {
		a := droidRow(t, c.id)
		if a == nil {
			t.Errorf("%s isn't listed", c.id)
			continue
		}
		if b, _ := json.Marshal(a); a.On || !a.NoHook || !strings.Contains(a.Blocked, c.why) {
			t.Errorf("%s listed as %s", c.id, b)
		}
		if _, err := SetRTK(c.id, true); err == nil || !strings.Contains(err.Error(), c.why) {
			t.Errorf("%s switched on: %v", c.id, err)
		}
		if _, err := RTKTakes(c.id); err == nil || !strings.Contains(err.Error(), c.why) {
			t.Errorf("uni-switch library rtk on %s: %v", c.id, err)
		}
	}
}
