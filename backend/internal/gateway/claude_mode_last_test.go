package gateway

import (
	"testing"
)

// The provider editor's row says how the last run went: the interactive
// mode, or -p with claudeTUI's code. A helper that couldn't start the
// interactive mode doesn't become -p, and says nothing that reads as a -p
// run.
func TestClaudeModeLast(t *testing.T) {
	claudeLast.Lock()
	before := claudeLast.run
	claudeLast.Unlock()
	t.Cleanup(func() { claudeLast.Lock(); claudeLast.run = before; claudeLast.Unlock() })

	noteClaudeRun("off")
	if m := ClaudeModeNow(); m.Last == nil || m.Last.TUI || m.Last.Why != "off" || m.Last.At.IsZero() {
		t.Fatalf("after a -p run: %+v", m.Last)
	}
	noteClaudeRun("")
	if m := ClaudeModeNow(); m.Last == nil || !m.Last.TUI || m.Last.Why != "" {
		t.Fatalf("after an interactive run: %+v", m.Last)
	}
	// the helper failing to start is no -p run
	w := &lockedWriter{run: &subscriptionRun{}}
	_, _ = w.Write([]byte("claude-tui: Claude Code's interactive mode didn't run: Claude Code ended before it showed its prompt (exit status 1): B\n"))
	if m := ClaudeModeNow(); m.Last == nil || !m.Last.TUI {
		t.Fatalf("after the helper failed: %+v", m.Last)
	}
}

// claudeTUI's code names what sent a run to -p, as a word the GUI
// translates.
func TestClaudeTUICodes(t *testing.T) {
	was := claudeTUIRuns
	claudeTUIRuns = true
	t.Cleanup(func() { claudeTUIRuns = was })
	req := &Request{Messages: []Message{{Role: "user", Parts: []Part{{Kind: Image, URL: "https://example.com/a.png"}}}}}
	if tui, code := claudeTUI(req, claudeCLI{path: "/usr/local/bin/claude"}); tui || code != "image" {
		t.Fatalf("an image by its URL: %v %q", tui, code)
	}
	if tui, code := claudeTUI(req, claudeCLI{}); tui || code != "missing" {
		t.Fatalf("no Claude Code: %v %q", tui, code)
	}
}
