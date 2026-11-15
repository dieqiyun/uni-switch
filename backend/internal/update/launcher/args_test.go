package main

import (
	"os"
	"path/filepath"
	"testing"
)

// #3737: uni-switch.com hands the app what was typed after `uni-switch` as it
// was typed, runs the program its trailer names, and leaves a command that
// opens the window to run on its own.
func TestArgsOf(t *testing.T) {
	for line, want := range map[string]string{
		`uni-switch`:                                  ``,
		`uni-switch plugin login`:                     `plugin login`,
		`uni-switch   version`:                        `version`,
		`"C:\Program Files\m\uni-switch.com" "a b" c`: `"a b" c`,
		`"C:\m\uni-switch" x`:                         `x`,
		`C:\m\uni-switch.com	-v`:                      `-v`,
		`"unclosed`:                                   ``,
	} {
		if got := argsOf(line); got != want {
			t.Errorf("argsOf(%q) = %q, want %q", line, got, want)
		}
	}
}

func TestOpensWindow(t *testing.T) {
	for _, a := range [][]string{nil, {"app"}, {"gui"}, {"tray"}, {"panel"}, {"-Embedding"}, {"UNI-SWITCH://open"}} {
		if !opensWindow(a) {
			t.Errorf("%q waited for as a terminal command", a)
		}
	}
	for _, a := range [][]string{{"version"}, {"plugin", "login"}, {"serve"}, {"backup", "app"}} {
		if opensWindow(a) {
			t.Errorf("%q not waited for", a)
		}
	}
}

func TestTargetName(t *testing.T) {
	dir := t.TempDir()
	com := filepath.Join(dir, "uni-switch.com")
	body := make([]byte, 4096)
	write := func(b []byte) {
		t.Helper()
		if err := os.WriteFile(com, b, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(append(append(append(body, 0), "uni-switch-windows-amd64 (1).exe"...), magic...))
	if got := targetName(com); got != "uni-switch-windows-amd64 (1).exe" {
		t.Fatalf("trailer's name: %q", got)
	}
	// no trailer, or one naming a path: its own name with .exe
	write(body)
	if got := targetName(com); got != "uni-switch.exe" {
		t.Fatalf("no trailer: %q", got)
	}
	write(append(append(append(body, 0), "../evil.exe"...), magic...))
	if got := targetName(com); got != "uni-switch.exe" {
		t.Fatalf("a path in the trailer: %q", got)
	}
}
