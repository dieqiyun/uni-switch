package gateway

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// giveRSS has every Claude Code run read as holding rss (over the budget
// when over), what its real process holds left out.
func giveRSS(t *testing.T, over bool, share int64) {
	t.Helper()
	old := claudeRunRSS
	claudeRunRSS = func(*proc.Tree) (int64, bool) {
		budget, _ := claudeRunBudget()
		budget = budget * share / 2
		if over {
			return budget + 1, true
		}
		return budget, true
	}
	t.Cleanup(func() { claudeRunRSS = old })
}

// A run that holds more than claudeRunBudget once its turn is whole is let
// go, not kept for the next turn: its session is shelved, and that turn
// resumes it in a new Claude Code, told only the new message (#3256: one
// kept for days reached 8 GiB and took the machine down).
func TestClaudeOversizedRunIsLetGoAndResumes(t *testing.T) {
	h := newScriptHarness(t, "ok", "")
	freshRoom(t, 4)
	giveRSS(t, true, 2)
	h.ask("rules A", "a1")
	var pidA string
	for pid := range h.runs() {
		pidA = pid
	}
	eventually(t, "the oversized run to end", func() bool {
		h.s.subscription.mu.Lock()
		defer h.s.subscription.mu.Unlock()
		return len(h.s.subscription.runs) == 0 && len(h.s.subscription.idle) == 0
	})
	h.ask("rules A", "a1", "a2")
	runs := h.runs()
	if len(runs) != 2 {
		t.Fatalf("runs: %v", runs)
	}
	resumed := false
	for pid, l := range runs {
		if pid != pidA && strings.Contains(l, "--resume s"+pidA) {
			resumed = true
			for _, line := range strings.Split(l, "\n") {
				if strings.HasPrefix(line, "told ") && strings.Contains(line, "a1") {
					t.Fatalf("the resumed run was told the conversation again:\n%s", l)
				}
			}
		}
	}
	if !resumed {
		t.Fatalf("the next turn didn't resume the oversized run's session s%s: %v", pidA, runs)
	}
}

// One within the budget is kept, and its next turn goes to the same
// Claude Code.
func TestClaudeRunWithinBudgetIsKept(t *testing.T) {
	h := newScriptHarness(t, "ok", "")
	freshRoom(t, 4)
	giveRSS(t, false, 2)
	h.ask("rules A", "a1")
	h.ask("rules A", "a1", "a2")
	if runs := h.runs(); len(runs) != 1 {
		t.Fatalf("a run within its budget wasn't kept: %v", runs)
	}
}

// A run waiting on its caller's tool results is let go past half as much
// again as the budget, and kept up to it.
func TestClaudeOversizedParkedRunIsLetGo(t *testing.T) {
	fakeClaudeCalling(t)
	s := New()
	t.Cleanup(s.subscription.abortAll)
	giveRSS(t, false, 3)
	kept := askForCall(t, s, context.Background(), "read a file")
	giveRSS(t, true, 3)
	over := askForCall(t, s, context.Background(), "read another file")
	if t.Failed() {
		return
	}
	if n := leftover([]int{over}, 3*time.Second); n != 0 {
		t.Fatal("a run over its budget was kept waiting on its tool calls")
	}
	if !alive(kept) {
		t.Fatal("a run within its budget was let go")
	}
}

// The budget is an eighth of the memory uni-switch may use, a floor under it.
func TestClaudeRunBudgetScalesWithMemory(t *testing.T) {
	root := t.TempDir()
	putMeminfo(t, root, "MemTotal:       32505856 kB\n")
	oldGOOS, oldRoot := memGOOS, memRoot
	memGOOS, memRoot = "linux", root
	t.Cleanup(func() { memGOOS, memRoot = oldGOOS, oldRoot })
	budget := func() int64 {
		runBudget.Once = sync.Once{}
		n, _ := claudeRunBudget()
		return n
	}
	t.Cleanup(func() { runBudget.Once = sync.Once{} })
	if n := budget(); n != 32505856<<10/8 {
		t.Fatalf("31 GiB holds a run of %d MiB", n>>20)
	}
	putMeminfo(t, root, "MemTotal:       2097152 kB\n")
	if n := budget(); n != claudeRunFloor {
		t.Fatalf("2 GiB holds a run of %d MiB, not the floor", n>>20)
	}
}

func putMeminfo(t *testing.T, root, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "proc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "proc", "meminfo"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
