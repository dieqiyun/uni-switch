//go:build !nogui

package gui

import "testing"

// TestPageToldHiddenOnTheMacOnly: off the Mac the panel is shown again by
// Wails' ToggleWindow, which never tells the page it is back, so a page told
// hidden there would stop its polling for good (#4293).
func TestPageToldHiddenOnTheMacOnly(t *testing.T) {
	if pageHiddenJSOn("darwin") != pageHiddenJS {
		t.Fatal("a Mac window's page isn't told it is out of sight")
	}
	for _, goos := range []string{"windows", "linux"} {
		if js := pageHiddenJSOn(goos); js != "" {
			t.Fatalf("%s: a page is told %q as its window hides", goos, js)
		}
	}
}
