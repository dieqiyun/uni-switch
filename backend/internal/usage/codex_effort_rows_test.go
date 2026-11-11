package usage

import (
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"testing"
	"time"
	"unsafe"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// threadRows are the rows of a whole ledger that carry a Codex thread.
func threadRows(l Ledgered) []Row {
	return slices.DeleteFunc(slices.Clone(l.Rows), func(r Row) bool { return r.codexActor == "" })
}

// CodexModelEffort's rows are the whole ledger's rows that carry a thread,
// row for row, though it keeps only the gateway calls a session file's call
// can be matched to: paired by response id, an ambiguous response id,
// matched by session and tokens, and unrelated calls around them (#2776).
func TestCodexEffortRowsAreTheWholeLedgers(t *testing.T) {
	now, _ := effortFixture(t)
	root := filepath.Join(sessions.CodexDir(), "sessions", "2026", "09", "30")
	at := func(d time.Duration) time.Time { return now.Add(-d) }
	writeCodexCall(t, filepath.Join(root, "rollout-2026-09-30T09-00-00-main-b.jsonl"), "main-b", "main-b", `"cli"`, "resp-b", "m", "high", at(30*time.Minute), 300, 30)
	writeCodexCall(t, filepath.Join(root, "rollout-2026-09-30T09-10-00-main-c.jsonl"), "main-c", "main-c", `"cli"`, "resp-c", "m2", "low", at(25*time.Minute), 70, 7)
	writeCodexCall(t, filepath.Join(root, "rollout-2026-09-30T09-20-00-main-d.jsonl"), "main-d", "main-d", `"cli"`, "resp-d", "m", "medium", at(20*time.Minute), 50, 5)
	writeCodexCall(t, filepath.Join(sessions.CodexDir(), "sessions", "2026", "09", "28", "rollout-2026-09-28T09-00-00-old-e.jsonl"), "old-e", "old-e", `"cli"`, "resp-e", "m", "low", now.Add(-50*time.Hour), 11, 1)
	// paired by its response id, which the gateway's call alone has
	Append(Record{Time: at(30 * time.Minute), Agent: "codex", Provider: "openai", Model: "m", Effort: "xhigh", Input: 300, Output: 30, Status: 200, ResponseID: "resp-b", Session: "main-b"})
	// two gateway calls with one response id: neither is paired
	Append(Record{Time: at(25 * time.Minute), Agent: "codex", Provider: "openai", Model: "m2", Input: 70, Output: 7, Status: 200, ResponseID: "resp-c"})
	Append(Record{Time: at(25 * time.Minute), Agent: "codex", Provider: "openai", Model: "m2", Input: 70, Output: 7, Status: 200, ResponseID: "resp-c"})
	// matched by session and tokens: the session file's call is the gateway's
	Append(Record{Time: at(20 * time.Minute), Agent: "codex", Provider: "openai", Model: "m", Input: 50, Output: 5, Status: 200, NativeSession: "main-d"})
	// paired two days ago
	Append(Record{Time: now.Add(-50 * time.Hour), Agent: "codex", Provider: "openai", Model: "m", Input: 11, Output: 1, Status: 200, ResponseID: "resp-e", Session: "old-e"})
	for i := range 40 {
		Append(Record{Time: at(time.Duration(i) * time.Minute), Agent: "claude-code", Provider: "anthropic", Model: "claude-opus-5-5", Input: 40 + i, Output: 4, Status: 200, RequestID: fmt.Sprintf("req_%d", i), Session: "sess1"})
		Append(Record{Time: at(time.Duration(i) * time.Minute), Agent: "codex", Provider: "openai", Model: "m", Input: 9, Output: 9, Status: 200, ResponseID: fmt.Sprintf("resp-other-%d", i), Session: "elsewhere"})
	}
	for _, p := range []Period{All, Today, Week} {
		for name, reader := range map[string]func(time.Time) []sessions.Call{"codex": codexCalls, "every agent": sessions.Calls} {
			whole := threadRows(ledgerOfAt(p, Filter{}, now, reader))
			if p == All && len(whole) < 3 {
				t.Fatalf("fixture: the whole ledger has %d thread rows: %+v", len(whole), whole)
			}
			if got := codexEffortRows(p, now, reader); !reflect.DeepEqual(got, whole) {
				t.Fatalf("%s, %s:\n got %+v\nwant %+v", p, name, got, whole)
			}
		}
		// read through the request index, as CodexModelEffort builds it
		// (#3493): the same sums as the whole ledger's thread rows
		want := codexModelEffort(threadRows(ledgerOfAt(p, Filter{}, now, sessions.Calls)))
		if got := codexEffortOfIndex(readRequestState(now), p, now); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s through the index:\n got %+v\nwant %+v", p, got, want)
		}
	}
}

// A build of CodexModelEffort takes no more for a gateway call no Codex
// session file's call can be matched to than a visit of the log does: it
// made every such call a record, a packed row and a priced row, and over
// ~720k calls that was most of the start's peak (#2776).
func TestCodexModelEffortLetsUnrelatedCallsGo(t *testing.T) {
	now, _ := effortFixture(t)
	Append(Record{Time: now.Add(-time.Hour), Agent: "codex", Provider: "openai", Model: "m", Input: 20, Output: 3, Status: 200, ResponseID: "resp-1", Session: "main-a"})
	const calls = 3000
	for i := range calls {
		Append(Record{Time: now.Add(-time.Duration(i) * time.Second), Agent: "claude-code", Provider: "anthropic", Model: "claude-opus-5-5", Input: 40 + i, Output: 4, Status: 200, RequestID: fmt.Sprintf("req_%d", i), Session: fmt.Sprintf("sess%d", i%50)})
	}
	measure := func(fn func()) uint64 {
		fn() // anything a first read sets up once
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		fn()
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}
	// the session files read by a reader of their own: a build each ask
	old := LogCalls
	LogCalls = codexCalls
	t.Cleanup(func() { LogCalls = old })
	if got := CodexModelEffort(All); len(got["main-a"]) != 1 {
		t.Fatalf("main-a's consumption: %+v", got)
	}
	// a packed row a call: the bar was a visit of the log, but a visit
	// decodes into a lent buffer now (#2278), and took 74 KB or 289 KB as
	// a pool kept it or not
	rows := uint64(calls+1) * uint64(unsafe.Sizeof(packedRow{}))
	build := measure(func() { CodexModelEffort(All) })
	t.Logf("a build allocated %d bytes, the log's rows are %d", build, rows)
	// the thread's own rows and the sets of its ids, less than a packed
	// row for each unrelated call
	if build > rows {
		t.Fatalf("a build over %d unrelated calls allocated %d bytes, a packed row each is %d", calls, build, rows)
	}
}
