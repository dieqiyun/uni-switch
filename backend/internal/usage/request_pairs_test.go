package usage

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// A page turned over the same chunks reuses the last pairing. It must give
// the page a fresh pairing gives: a gateway call its rollout stands for is
// counted once, and so is a call a forked file copies, on every page
// and in the sums.
func TestRequestPairsReusedAcrossPages(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local))
	dir := filepath.Join(sessions.CodexDir(), "sessions", "2026", "09", "30")
	writeCodexIdentity(t, "main-a", "native-main", "vscode")
	writeCodexCall(t, filepath.Join(dir, "rollout-2026-09-30T10-00-00-main-a.jsonl"), "main-a", "native-main", `"vscode"`, "resp-paired", "m", "low", now.Add(-time.Minute), 60, 6)
	writeCodexCall(t, filepath.Join(dir, "rollout-2026-09-30T10-00-01-solo-b.jsonl"), "solo-b", "native-solo", `"vscode"`, "resp-solo", "m", "low", now.Add(-2*time.Minute), 30, 3)
	// A fork copies solo-b's call; it is counted once.
	writeCodexCall(t, filepath.Join(dir, "rollout-2026-09-30T10-00-02-fork-c.jsonl"), "fork-c", "native-solo", `"vscode"`, "resp-solo", "m", "low", now.Add(-2*time.Minute), 30, 3)
	Append(Record{Time: now.Add(-time.Minute), Agent: "codex", Provider: "openai", Model: "m", Input: 60, Output: 6, Status: 200, ResponseID: "resp-paired"})
	Append(Record{Time: now.Add(-3 * time.Hour), Agent: "claude", Provider: "openai", Model: "m", Input: 7, Output: 1, Status: 200})

	for _, p := range []Period{All, Today, Week} {
		want := pageFromLedger(p, Filter{}, 0, 100, LedgerOf(p, Filter{}))
		if want.Total != 3 || want.Sum.Input != 97 {
			t.Fatalf("%v: fixture pairs to %d rows, %d input", p, want.Total, want.Sum.Input)
		}
		for offset := 0; offset < 3; offset++ {
			got := QueryPage(p, Filter{}, offset, 1)
			if got.Total != 3 || got.Sum.Input != 97 || len(got.Rows) != 1 {
				t.Fatalf("%v page %d: %d rows, %d input, %d shown", p, offset, got.Total, got.Sum.Input, len(got.Rows))
			}
			if got.Rows[0].ResponseID != want.Rows[offset].ResponseID || got.Rows[0].Input != want.Rows[offset].Input {
				t.Fatalf("%v page %d: %+v, want %+v", p, offset, got.Rows[0].Record, want.Rows[offset].Record)
			}
		}
		equalPage(t, QueryPage(p, Filter{}, 0, 100), want)
		equalPage(t, QueryPage(p, Filter{Query: "main-a"}, 0, 100), pageFromLedger(p, Filter{Query: "main-a"}, 0, 100, LedgerOf(p, Filter{Query: "main-a"})))
	}
}

// Codex's effort over a period whose page was just built pairs no row
// again: the warm-up's page of All paired every session file's calls, and
// the effort read after it paired them all over (#3493). It is the effort
// a pairing of its own gives.
func TestCodexEffortReusesPagePairing(t *testing.T) {
	pairAtOnce(t)
	pageHome(t)
	now := holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local))
	dir := filepath.Join(sessions.CodexDir(), "sessions", "2026", "09", "30")
	writeCodexIdentity(t, "main-a", "native-main", "vscode")
	writeCodexCall(t, filepath.Join(dir, "rollout-2026-09-30T10-00-00-main-a.jsonl"), "main-a", "native-main", `"vscode"`, "resp-paired", "m", "low", now.Add(-time.Minute), 60, 6)
	writeCodexCall(t, filepath.Join(dir, "rollout-2026-09-30T10-00-01-solo-b.jsonl"), "solo-b", "native-solo", `"vscode"`, "resp-solo", "m", "high", now.Add(-2*time.Minute), 30, 3)
	writeCodexCall(t, filepath.Join(dir, "rollout-2026-09-30T10-00-02-fork-c.jsonl"), "fork-c", "native-solo", `"vscode"`, "resp-solo", "m", "high", now.Add(-2*time.Minute), 30, 3)
	Append(Record{Time: now.Add(-time.Minute), Agent: "codex", Provider: "openai", Model: "m", Input: 60, Output: 6, Status: 200, ResponseID: "resp-paired"})
	pairings := 0
	pairing = func() { pairings++ }
	t.Cleanup(func() { pairing = nil })

	QueryPage(All, Filter{}, 0, 50)
	if pairings != 1 {
		t.Fatalf("the page paired %d times, want 1", pairings)
	}
	got := CodexModelEffort(All)
	if pairings != 1 {
		t.Fatalf("the effort after the page paired again (%d pairings)", pairings)
	}
	if rows := got["main-a"]; len(rows) != 1 || rows[0].Input != 60 {
		t.Fatalf("effort = %+v", got)
	}
	requestCache.Lock()
	requestCache.pairs = nil
	requestCache.Unlock()
	if want := codexEffortOfIndex(readRequestState(now), All, now); pairings != 2 || !reflect.DeepEqual(got, want) {
		t.Fatalf("effort through the page's pairing %+v, a pairing of its own %+v (%d pairings)", got, want, pairings)
	}
}
