package gui

import (
	"os"
	"path/filepath"
	"testing"
)

// T3 Code is on the page once ~/.t3 is there, with uni-switch to add as a
// provider of its own, and not before (unless this Mac has the app).
func TestStateListsT3Code(t *testing.T) {
	home := sandboxHome(t)
	t.Setenv("T3CODE_HOME", "")
	find := func() *agentJSON {
		for _, a := range state().Agents {
			if a.ID == "t3code" {
				return &a
			}
		}
		return nil
	}
	_, app := os.Stat("/Applications/T3 Code (Alpha).app")
	_, app2 := os.Stat("/Applications/T3 Code.app")
	if a := find(); a != nil && app != nil && app2 != nil {
		t.Fatalf("listed with no ~/.t3: %+v", a)
	}
	os.MkdirAll(filepath.Join(home, ".t3"), 0o755)
	a := find()
	if a == nil {
		t.Fatal("T3 Code not listed")
	}
	if a.Name != "T3 Code" || a.Icon != "t3code" || a.Path != filepath.Join("~", ".t3", "userdata", "settings.json") || len(a.Fields) != 1 || a.Fields[0].Key != "provider" ||
		len(a.Fields[0].Options) != 1 || a.Fields[0].Options[0].Value != "uni-switch" {
		t.Fatalf("T3 Code: %+v", a)
	}
}
