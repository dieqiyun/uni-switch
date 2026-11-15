package usage

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// effortFixture is one Codex thread's call in a sandbox home.
func effortFixture(t *testing.T) (now time.Time, rollout string) {
	t.Helper()
	pageHome(t)
	now = holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC))
	rollout = filepath.Join(sessions.CodexDir(), "sessions", "2026", "09", "30", "rollout-2026-09-30T10-00-00-main-a.jsonl")
	writeCodexCall(t, rollout, "main-a", "main-a", `"vscode"`, "resp-1", "m", "low", now.Add(-time.Hour), 20, 3)
	return now, rollout
}

// The Sessions page's overview and list ask for every Codex thread's
// consumption at once, each refresh. Each ask built its own ledger of all
// history, and with 68k session files the app grew to 85 GB (#2278).
// Asks arriving together are answered by one build.
func TestCodexModelEffortConcurrentAsksBuildOnce(t *testing.T) {
	effortFixture(t)
	var built atomic.Int32
	release := make(chan struct{})
	effortBuilt = func() {
		if built.Add(1) == 1 {
			<-release
		}
	}
	t.Cleanup(func() { effortBuilt = nil })
	const asks = 8
	results := make([]map[string][]sessions.ModelEffort, asks)
	var wg sync.WaitGroup
	for i := range asks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = CodexModelEffort(All)
		}()
	}
	// give the other asks time to start builds of their own, as they did
	deadline := time.Now().Add(time.Second)
	for built.Load() < asks && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	wg.Wait()
	if n := built.Load(); n != 1 {
		t.Fatalf("%d asks at once built the ledger %d times, want 1", asks, n)
	}
	for i, r := range results {
		if got, ok := findModelEffort(r["main-a"], "m", "low"); !ok || got.Input != 20 {
			t.Fatalf("ask %d got %+v", i, r)
		}
	}
}

// While the next answer is built, an ask for the same period is told the
// last one at once rather than building another ledger beside it.
func TestCodexModelEffortAnswersLastWhileBuilding(t *testing.T) {
	now, rollout := effortFixture(t)
	var built atomic.Int32
	effortBuilt = func() { built.Add(1) }
	t.Cleanup(func() { effortBuilt = nil })
	CodexModelEffort(All)

	writeCodexCall(t, rollout, "main-a", "main-a", `"vscode"`, "resp-1", "m", "low", now.Add(-time.Hour), 2000, 30)
	later := now.Add(time.Minute)
	if err := os.Chtimes(rollout, later, later); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	effortBuilt = func() {
		if built.Add(1) == 2 {
			close(entered)
			<-release
		}
	}
	done := make(chan map[string][]sessions.ModelEffort)
	go func() { done <- CodexModelEffort(All) }()
	<-entered
	meanwhile := CodexModelEffort(All)
	close(release)
	next := <-done
	if n := built.Load(); n != 2 {
		t.Fatalf("an ask during a build built the ledger again: %d builds, want 2", n)
	}
	if got, _ := findModelEffort(meanwhile["main-a"], "m", "low"); got.Input != 20 {
		t.Fatalf("an ask during the build got %+v, want the last answer", meanwhile)
	}
	if got, _ := findModelEffort(next["main-a"], "m", "low"); got.Input != 2000 {
		t.Fatalf("the build got %+v", next)
	}
	if got, _ := findModelEffort(CodexModelEffort(All)["main-a"], "m", "low"); built.Load() != 2 || got.Input != 2000 {
		t.Fatalf("the next ask: builds %d, %+v", built.Load(), got)
	}
}

// A history that changes between polls, as a working Codex's does, is
// built into the answer at most once a minute: an ask inside the minute is
// told the last answer without reading the ledger or the sources' state,
// and the first ask after it is told it too while the change is built
// behind it, for the next ask.
func TestCodexModelEffortHeldForAMinute(t *testing.T) {
	now, rollout := effortFixture(t)
	effortFresh, effortBehind = time.Hour, true
	t.Cleanup(func() { effortFresh, effortBehind = 0, false })
	var built atomic.Int32
	effortBuilt = func() { built.Add(1) }
	t.Cleanup(func() { effortBuilt = nil })
	CodexModelEffort(All)

	writeCodexCall(t, rollout, "main-a", "main-a", `"vscode"`, "resp-1", "m", "low", now.Add(-time.Hour), 2000, 30)
	later := now.Add(time.Minute)
	if err := os.Chtimes(rollout, later, later); err != nil {
		t.Fatal(err)
	}
	if got, _ := findModelEffort(CodexModelEffort(All)["main-a"], "m", "low"); built.Load() != 1 || got.Input != 20 {
		t.Fatalf("inside the minute: builds %d, %+v, want 1 and the last answer", built.Load(), got)
	}
	codexEffort.Lock()
	codexEffort.periods[All].at = time.Now().Add(-2 * time.Hour)
	codexEffort.Unlock()
	entered, release := make(chan struct{}), make(chan struct{})
	effortBuilt = func() {
		built.Add(1)
		close(entered)
		<-release
	}
	asked := make(chan map[string][]sessions.ModelEffort, 1)
	go func() { asked <- CodexModelEffort(All) }()
	<-entered
	select {
	case got := <-asked:
		if got, _ := findModelEffort(got["main-a"], "m", "low"); got.Input != 20 {
			t.Fatalf("after the minute the ask got %+v, want the last answer", got)
		}
	case <-time.After(30 * time.Second):
		close(release)
		t.Fatal("after the minute the ask waited for the build")
	}
	codexEffort.Lock()
	building := codexEffort.periods[All].building
	codexEffort.Unlock()
	if got, _ := findModelEffort(CodexModelEffort(All)["main-a"], "m", "low"); got.Input != 20 {
		t.Fatalf("an ask during the build got %+v, want the last answer", got)
	}
	close(release)
	<-building
	effortBuilt = func() { built.Add(1) }
	if got, _ := findModelEffort(CodexModelEffort(All)["main-a"], "m", "low"); built.Load() != 2 || got.Input != 2000 {
		t.Fatalf("after the build: builds %d, %+v, want 2 and the change", built.Load(), got)
	}

	// another home's answer is never held for this one, nor told while
	// its own is built
	effortFixture(t)
	if got := CodexModelEffort(All); len(got) == 0 || built.Load() != 3 {
		t.Fatalf("a new home was told the last one's answer: builds %d, %+v", built.Load(), got)
	}
	if n := built.Load(); n != 3 {
		t.Fatalf("a new home was told the last one's answer: builds %d, want 3", n)
	}
}

// An answer is kept when there is no gateway log yet, and when the log grew
// while it was built: the next ask builds again only if the state did change.
func TestCodexModelEffortKeptWithoutLogAndAcrossAWrite(t *testing.T) {
	now, _ := effortFixture(t)
	var built atomic.Int32
	effortBuilt = func() { built.Add(1) }
	t.Cleanup(func() { effortBuilt = nil })
	CodexModelEffort(All)
	CodexModelEffort(All)
	if n := built.Load(); n != 1 {
		t.Fatalf("with no usage.jsonl the ledger was built %d times, want 1", n)
	}
	effortBuilt = func() {
		if built.Add(1) == 2 { // the gateway writes while the ledger is built
			Append(Record{Time: now.Add(-time.Minute), Agent: "codex", Provider: "openai", Model: "m", Input: 5, Output: 1, Status: 200, ResponseID: "resp-2"})
		}
	}
	Append(Record{Time: now.Add(-2 * time.Minute), Agent: "pi", Provider: "openai", Model: "m", Input: 1, Output: 1, Status: 200})
	CodexModelEffort(All) // built under the state before the write
	CodexModelEffort(All) // the write changed it
	CodexModelEffort(All)
	if n := built.Load(); n != 3 {
		t.Fatalf("builds = %d, want 3", n)
	}
}

// CodexModelEffort reads Codex's session files only; what it reports is what
// the whole ledger, every agent's files read, says of each Codex thread.
func TestCodexModelEffortIsTheWholeLedgers(t *testing.T) {
	now, _ := effortFixture(t)
	root := filepath.Join(sessions.CodexDir(), "sessions", "2026", "09", "30")
	writeCodexCall(t, filepath.Join(root, "rollout-2026-09-30T11-00-00-main-b.jsonl"), "main-b", "main-b", `"cli"`, "resp-b", "m", "high", now.Add(-30*time.Minute), 300, 30)
	Append(Record{Time: now.Add(-30 * time.Minute), Agent: "codex", Provider: "openai", Model: "m", Effort: "xhigh", Input: 300, Output: 30, Status: 200, ResponseID: "resp-b", Session: "main-b"})
	Append(Record{Time: now.Add(-20 * time.Minute), Agent: "codex", Provider: "openai", Model: "m", Input: 9, Output: 9, Status: 200, ResponseID: "resp-unpaired"})
	// a Claude Code session the gateway saw a call of
	claude := filepath.Join(sessions.ClaudeDir(), "projects", "-work-app", "sess1.jsonl")
	at := now.Add(-10 * time.Minute)
	line := fmt.Sprintf(`{"type":"assistant","uuid":"u1","timestamp":%q,"message":{"id":"msg1","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":40,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":4}},"requestId":"req_1","sessionId":"sess1","cwd":"/work/app","entrypoint":"cli"}`+"\n", at.Format(time.RFC3339Nano))
	if err := os.MkdirAll(filepath.Dir(claude), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claude, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	Append(Record{Time: at, Agent: "claude-code", Provider: "anthropic", Model: "claude-opus-5-5", Input: 40, Output: 4, Status: 200, RequestID: "req_1", Session: "sess1"})

	whole := codexModelEffort(LedgerOfAt(All, Filter{}, now).Rows)
	if len(whole) != 2 {
		t.Fatalf("fixture: the whole ledger has %d Codex threads: %+v", len(whole), whole)
	}
	// and only the chunks a Codex thread is known for are read for it:
	// the Claude Code session's isn't
	var read []sessions.CallSource
	visitingActors = func(c *rowChunk) { read = append(read, c.Source) }
	t.Cleanup(func() { visitingActors = nil })
	if got := CodexModelEffort(All); !reflect.DeepEqual(got, whole) {
		t.Fatalf("Codex-only ledger:\n got %+v\nwant %+v", got, whole)
	}
	if len(read) == 0 {
		t.Fatal("no chunk was read for Codex's effort")
	}
	for _, src := range read {
		if src.Path == claude {
			t.Fatalf("the Claude Code session's chunk was read for Codex's effort: %v", read)
		}
	}
}

// Asks within the minute are told the kept answer itself, not a copy of
// every thread's consumption each: over a large history the copies were
// ~230 MB every 80 s of an open Sessions page (#2278).
func TestCodexModelEffortAsksShareTheKeptAnswer(t *testing.T) {
	effortFixture(t)
	effortFresh = time.Hour
	t.Cleanup(func() { effortFresh = 0 })
	first := CodexModelEffort(All)
	again := CodexModelEffort(All)
	if len(first) == 0 {
		t.Fatal("no consumption")
	}
	if reflect.ValueOf(first).UnsafePointer() != reflect.ValueOf(again).UnsafePointer() {
		t.Fatal("an unchanged ask was told a copy of the kept answer")
	}
}
