package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// A page's sums are kept per chunk and reused while the chunk's rows and
// what pairing gave them are the same. A changed identity or file must be
// summed again, not read from the last page.
func TestRequestCellsFollowIdentityAndFiles(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local))
	writeCodexIdentity(t, "main-a", "native-main", "vscode")
	childPath := filepath.Join(sessions.CodexDir(), "sessions", "2026", "09", "30", "rollout-2026-09-30T10-00-00-child-b.jsonl")
	writeCodexCall(t, childPath, "child-b", "native-main", `{"subagent":{"thread_spawn":{"parent_thread_id":"main-a"}}}`, "resp-local", "m", "low", now.Add(-time.Minute), 40, 4)
	Append(Record{Time: now.Add(-2 * time.Hour), Agent: "claude", Provider: "openai", Model: "m", Input: 7, Output: 1, Status: 200})

	sums := func(f Filter) (int, int) {
		page := QueryPage(All, f, 0, 100)
		return page.Total, page.Sum.Input
	}
	for _, offset := range []int{0, 1, 0} {
		QueryPage(All, Filter{}, offset, 1)
	}
	if n, in := sums(Filter{Query: "main-a"}); n != 1 || in != 40 {
		t.Fatalf("subagent's call under its root: %d rows, %d input", n, in)
	}

	// Only the parent's file changes: the child's chunk is the same, and
	// its root is now the parent's parent.
	for _, f := range []Filter{{Query: "top-z"}, {Query: "main-a"}} {
		QueryPage(All, f, 0, 100)
	}
	writeCodexIdentity(t, "main-a", "native-main", map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{"parent_thread_id": "top-z"}}})
	writeCodexIdentity(t, "top-z", "native-main", "vscode")
	if n, in := sums(Filter{Query: "top-z"}); n != 1 || in != 40 {
		t.Fatalf("a cached part kept the old root: %d rows, %d input", n, in)
	}

	// The child is its own main thread now.
	writeCodexCall(t, childPath, "child-b", "native-main", `"vscode"`, "resp-local", "m", "low", now.Add(-time.Minute), 40, 4)
	if err := os.Chtimes(childPath, now, now); err != nil {
		t.Fatal(err)
	}
	if n, _ := sums(Filter{Query: "main-a"}); n != 0 {
		t.Fatalf("a changed file kept its old root: %d rows", n)
	}
	if n, in := sums(Filter{Query: "child-b"}); n != 1 || in != 40 {
		t.Fatalf("new main thread: %d rows, %d input", n, in)
	}

	// The file's call changes.
	writeCodexCall(t, childPath, "child-b", "native-main", `"vscode"`, "resp-local", "m", "low", now.Add(-time.Minute), 90, 4)
	if err := os.Chtimes(childPath, now.Add(time.Second), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if n, in := sums(Filter{}); n != 2 || in != 97 {
		t.Fatalf("changed file: %d rows, %d input", n, in)
	}
	Append(Record{Time: now.Add(-time.Hour), Agent: "claude", Provider: "openai", Model: "m", Input: 3, Output: 1, Status: 200})
	equalPage(t, QueryPage(All, Filter{}, 0, 100), pageFromLedger(All, Filter{}, 0, 100, LedgerOf(All, Filter{})))
	equalPage(t, QueryPage(Week, Filter{}, 0, 100), pageFromLedger(Week, Filter{}, 0, 100, LedgerOf(Week, Filter{})))
}
