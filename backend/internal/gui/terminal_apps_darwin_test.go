//go:build darwin && cgo

package gui

import (
	"slices"
	"strings"
	"testing"
)

func TestDiscoverTerminals(t *testing.T) {
	found, err := discoverTerminals()
	if err != nil {
		t.Fatal(err)
	}
	terminalFound := false
	for _, app := range found.Apps {
		if app.ID == "com.apple.TextEdit" {
			t.Fatal("a text editor was offered as a session terminal")
		}
		if app.ID == terminalBundleID && app.Path != "" {
			terminalFound = true
		}
	}
	if !terminalFound {
		t.Fatalf("Terminal.app missing from .command handlers: %+v", found.Apps)
	}
}

// StringKe (#4565): Rex claims no .command file, so Launch Services never
// lists it; it is offered when its CLI is found, and opened through it.
func TestDiscoverTerminalsOffersRex(t *testing.T) {
	was := rexFound
	t.Cleanup(func() { rexFound = was })
	rexFound = func() string { return "/opt/rex/bin/rex" }
	found, err := discoverTerminals()
	if err != nil {
		t.Fatal(err)
	}
	rex := slices.IndexFunc(found.Apps, func(a terminalApp) bool { return a.ID == rexBundleID })
	if rex < 0 || found.Apps[rex].Path != "/opt/rex/bin/rex" || found.Apps[rex].Name != "Rex" {
		t.Fatalf("Rex not offered with its CLI found: %+v", found.Apps)
	}
	f := &fakeRex{t: t, sessions: []fakeRexSession{{id: "session:a1", label: "work"}, {id: "session:a2", label: "notes"}}}
	f.use(t)
	if err := openTerminal("cd '/tmp' && claude --resume x", "Fix the build", rexBundleID); err != nil {
		t.Fatal(err)
	}
	if f.bin != "/opt/rex/bin/rex" || len(f.sessions) != 3 || f.sessions[2].label != "uni-switch" || !slices.Equal(f.sessions[2].windows, []string{"Fix the build"}) {
		t.Fatalf("rex ran %q, sessions %+v: want a uni-switch session, its tab named for the session's title", f.ran, f.sessions)
	}
	ran := len(f.ran)
	rexFound = func() string { return "" }
	if err := openTerminal("claude", "", rexBundleID); err == nil || !strings.Contains(err.Error(), "no longer available") {
		t.Fatalf("Rex chosen with no CLI found: %v, want it said to be unavailable", err)
	}
	if len(f.ran) != ran {
		t.Fatalf("rex ran again with none found: %q", f.ran)
	}
}
