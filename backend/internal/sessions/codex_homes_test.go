package sessions

import (
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// A second ChatGPT account is Codex run with its own CODEX_HOME
// (~/.codex-work), which writes that account's rollouts there
// (codex-rs rollout/src/lib.rs: $CODEX_HOME/sessions). phantom3792 on X:
// only the default folder's sessions were counted. Both folders' sessions
// are listed, and both's calls go into the usage ledger.
func TestCodexOtherHomeSessionsCounted(t *testing.T) {
	d := setupCalls(t)
	home := filepath.Join(filepath.Dir(d.codex), "home")
	work := filepath.Join(home, ".codex-work")
	writeLines(t, filepath.Join(work, "auth.json"), `{"auth_mode":"chatgpt","tokens":{}}`)
	for i, dir := range []string{d.codex, work} {
		name := []string{"rollout-2026-10-20T10-00-00-0190aaaa-1111-7222-8333-444455556666.jsonl",
			"rollout-2026-10-21T11-00-00-0190bbbb-1111-7222-8333-444455556666.jsonl"}[i]
		writeLines(t, filepath.Join(dir, "sessions", "2026", "10", "2"+string(rune('0'+i)), name),
			append(cxTurnLines(1, "turn-1", "gpt-6-astra", "low"),
				tokenCountLine(3, cxUse(1000, 0, 0, 50, 0), cxUse(1000, 0, 0, 50, 0)))...)
	}
	// a folder named like one that Codex never ran in is no Codex folder
	writeLines(t, filepath.Join(home, ".codex-notes", "todo.txt"), "x")
	Reset()
	n := 0
	for _, s := range List(0) {
		if s.Agent == "codex" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("codex sessions listed: %d, want 2 (~/.codex and ~/.codex-work)", n)
	}
	if calls := Calls(time.Time{}); len(calls) != 2 {
		t.Fatalf("codex calls counted: %d, want 2", len(calls))
	}
	if ds := Dirs(); !slices.Contains(ds, work) || slices.Contains(ds, filepath.Join(home, ".codex-notes")) {
		t.Fatalf("dirs %v", ds)
	}
}
