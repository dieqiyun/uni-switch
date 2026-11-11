package gui

import (
	"os"
	"path/filepath"
	"testing"
)

// Pencil (pen.dev) is on the page once ~/.pencil is there, with uni-switch to
// put in its model picker, and not before (unless this Mac has the app).
func TestStateListsPencil(t *testing.T) {
	home := sandboxHome(t)
	find := func() *agentJSON {
		for _, a := range state().Agents {
			if a.ID == "pencil" {
				return &a
			}
		}
		return nil
	}
	_, app := os.Stat("/Applications/Pencil.app")
	_, app2 := os.Stat("/Applications/pen.dev.app")
	if a := find(); a != nil && app != nil && app2 != nil {
		t.Fatalf("listed with no ~/.pencil: %+v", a)
	}
	os.MkdirAll(filepath.Join(home, ".pencil"), 0o755)
	a := find()
	if a == nil {
		t.Fatal("Pencil not listed")
	}
	if a.Name != "Pencil" || a.Path != filepath.Join("~", ".pencil", "models.json") || len(a.Fields) != 1 || a.Fields[0].Key != "provider" ||
		len(a.Fields[0].Options) != 1 || a.Fields[0].Options[0].Value != "uni-switch" {
		t.Fatalf("Pencil: %+v", a)
	}
}
