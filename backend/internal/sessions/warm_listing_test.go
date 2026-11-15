package sessions

import (
	"path/filepath"
	"testing"
)

// The warm-up's reads list the rollouts once between them (#3493), and only
// they do: a page asking meanwhile, and every read after the warm-up, sees
// a rollout written since.
func TestWarmUpListsRolloutsOnce(t *testing.T) {
	d := setupCalls(t)
	day := filepath.Join(d.codex, "sessions", "2026", "09", "20")
	first := filepath.Join(day, "rollout-2026-09-20T10-00-00-0190aaaa-1111-7222-8333-444455556666.jsonl")
	second := filepath.Join(day, "rollout-2026-09-20T11-00-00-0190bbbb-1111-7222-8333-444455556666.jsonl")
	writeLines(t, first, cxTurnLines(1, "turn-1", "gpt-6-astra", "low")...)
	Warm(func() {
		if n := len(codexFiles()); n != 1 {
			t.Fatalf("warm-up's first listing: %d rollouts", n)
		}
		writeLines(t, second, cxTurnLines(1, "turn-1", "gpt-6-astra", "low")...)
		fs := codexFiles()
		if len(fs) != 1 {
			t.Fatalf("warm-up listed again: %d rollouts", len(fs))
		}
		// the kept listing is shared, not copied for each read (#2278), and
		// clipped: an append to one read's is made elsewhere
		_ = append(fs, file{})
		if again := codexFiles(); len(again) != 1 || &again[0] != &fs[0] || cap(again) != 1 {
			t.Fatalf("the kept listing was copied, or an append reached it: %+v", again)
		}
		done := Ask()
		if n := len(codexFiles()); n != 2 {
			t.Fatalf("a page asking got the warm-up's listing: %d rollouts", n)
		}
		done()
	})
	if n := len(codexFiles()); n != 2 {
		t.Fatalf("after the warm-up: %d rollouts", n)
	}
}
