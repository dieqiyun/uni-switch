//go:build !nogui

package gui

import "testing"

// GTK makes the program name the window's Wayland app_id and X11 WM_CLASS.
// Left unset, both were the executable's name (uni-switch-linux-amd64 run as
// downloaded), which no desktop entry names, and the dock drew a gear (#2626).
func TestLinuxWindowNamedAsItsEntry(t *testing.T) {
	o := linuxOptions()
	if o.ProgramName != programName || programName != "uni-switch" {
		t.Fatalf("ProgramName = %q (programName %q), want the entry's StartupWMClass, uni-switch", o.ProgramName, programName)
	}
	if !o.DisableQuitOnLastWindowClosed {
		t.Fatal("uni-switch quits when its last window closes; the tray should stay")
	}
}
