package sessions

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestPackedRolloutReadOnceForSessionsAndCalls: the start's warm-up parses
// every changed session file for the Sessions page, then reads every
// changed file's calls for Requests. A packed rollout is decompressed whole
// for each read, so when the Codex app repacked all of them (11,591 on
// 10-20) each was decompressed twice. Its calls are now read on the
// parse's own pass, and what both pages get is what the two reads gave.
func TestPackedRolloutReadOnceForSessionsAndCalls(t *testing.T) {
	_, codex := setup(t)
	writeLines(t, filepath.Join(codex, "sessions", "2026", "09", "21", "rollout-2026-09-21T10-00-00-0190aaaa-1111-7222-8333-444455556666.jsonl"), longRollout(0, 40)...)
	for _, p := range rollouts(t, codex) {
		pack(t, p, false)
	}
	// nothing kept: neither the summaries nor the calls' shards
	forget := func() {
		// Reset first: it writes a save still pending, which would put
		// back a cache file removed before it
		Reset()
		os.Remove(CachePath())
		os.RemoveAll(callCacheDir())
	}

	var mu sync.Mutex
	scans := map[string]int{}
	scanHook = func(p string, off int64) {
		if strings.HasSuffix(p, zstSuffix) {
			mu.Lock()
			scans[p]++
			mu.Unlock()
		}
	}
	t.Cleanup(func() { scanHook = nil })
	counted := func() map[string]int {
		mu.Lock()
		defer mu.Unlock()
		out := scans
		scans = map[string]int{}
		return out
	}
	readCallsAll := func() map[string][]Call {
		out := map[string][]Call{}
		for _, s := range CallSources() {
			if s.Agent == "codex" {
				out[s.Path] = ReadCallSource(s)
			}
		}
		return out
	}
	day := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)

	// as before: calls read first, then the summaries, each on a scan of
	// its own; the reference for what both pages get
	forget()
	counted()
	wantCalls := readCallsAll()
	wantList, wantStats := List(0), StatsAt(0, day)
	before := counted()
	n := 0
	for p, k := range before {
		if k != 2 {
			t.Fatalf("calls, then sessions: %s scanned %d times, want 2", p, k)
		}
		n++
	}
	if n != 3 {
		t.Fatalf("%d packed rollouts scanned, want 3: %v", n, before)
	}
	calls := 0
	for _, cs := range wantCalls {
		calls += len(cs)
	}
	if calls < 40 {
		t.Fatalf("only %d calls read: the fixture tests nothing", calls)
	}

	check := func(name string) {
		t.Helper()
		list, stats := List(0), StatsAt(0, day)
		got := readCallsAll()
		for p, k := range counted() {
			if k != 1 {
				t.Errorf("%s: %s scanned %d times, want once", name, p, k)
			}
		}
		if !reflect.DeepEqual(got, wantCalls) {
			t.Errorf("%s: calls differ from their own read's", name)
		}
		if name == "first read" && (!reflect.DeepEqual(list, wantList) || !reflect.DeepEqual(stats, wantStats)) {
			t.Errorf("%s: sessions differ from a read without the calls", name)
		}
	}

	// the warm-up's order with nothing kept: sessions, then calls
	forget()
	counted()
	check("first read")

	// every rollout repacked: both pages' kept reads are stale
	later := time.Now().Add(-time.Hour)
	for p := range wantCalls {
		if err := os.Chtimes(p, later, later); err != nil {
			t.Fatal(err)
		}
	}
	Reset()
	counted()
	check("after repacking")
}
