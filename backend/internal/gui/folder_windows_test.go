//go:build windows && !nogui

package gui

import (
	"os/exec"
	"testing"
)

// Explorer is found by its own path when PATH has no Windows folder (as one user had:
// exec: "explorer.exe": executable file not found in %PATH%).
func TestExplorerWithoutPath(t *testing.T) {
	t.Setenv("PATH", `C:\nowhere`)
	if _, err := exec.LookPath("explorer.exe"); err == nil {
		t.Fatal("explorer.exe still found on PATH")
	}
	p := explorer()
	if !fileExists(p) {
		t.Fatalf("explorer() = %s, not there", p)
	}
}
