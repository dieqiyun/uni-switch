package agent

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// raclen (#4548): the "Show {agent}'s own models" switch left Claude Code's
// own rows in /model untouched: it filters uni-switch's pickers alone
// (agent.Picks, #1642), and nothing uni-switch writes for Claude Code looked at
// it. Claude Code's /model is fed by uni-switch all the same — modelPicker's
// rows are uni-switch's (writePicker) — and Claude Code drops its built-in
// lineup rows when a modelPicker says replaceBuiltInOptions, so writePicker
// sets that while the switch is off, and takes it back out when on. The
// Default row stays either way: Claude Code keeps it as its own fallback,
// it is no model of its lineup.
func TestClaudePickerOwnModelsSwitch(t *testing.T) {
	home := syncHome(t)
	path := filepath.Join(home, ".claude", "settings.json")
	writeFile(t, path, `{"theme":"dark"}`)

	flag := func() (hasPicker, replace bool) {
		t.Helper()
		raw, has := edit.GetJSON(path, "modelPicker")
		if !has {
			return false, false
		}
		var p struct {
			Replace *bool `json:"replaceBuiltInOptions"`
		}
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatalf("modelPicker: %v\n%s", err, readFile(path))
		}
		return true, p.Replace != nil && *p.Replace
	}
	sync := func() {
		t.Helper()
		if err := claude(home).Apply("model", "relay/glm-4.6"); err != nil {
			t.Fatal(err)
		}
	}

	// the switch on: Claude Code's built-in lineup stays, nothing asked for
	sync()
	if has, replace := flag(); !has || replace {
		t.Fatalf("with own models shown: picker has=%v, replaceBuiltInOptions=%v, want has, no flag\n%s", has, replace, readFile(path))
	}

	// the switch off: the picker says so, and /model's own rows go
	if err := provider.SetOwnModelsHidden("claude", true); err != nil {
		t.Fatal(err)
	}
	sync()
	if has, replace := flag(); !has || !replace {
		t.Fatalf("with own models hidden: picker has=%v, replaceBuiltInOptions=%v, want both\n%s", has, replace, readFile(path))
	}

	// the switch back on: the flag goes again
	if err := provider.SetOwnModelsHidden("claude", false); err != nil {
		t.Fatal(err)
	}
	sync()
	if has, replace := flag(); !has || replace {
		t.Fatalf("with own models shown again: picker has=%v, replaceBuiltInOptions=%v, want has, no flag\n%s", has, replace, readFile(path))
	}
}
