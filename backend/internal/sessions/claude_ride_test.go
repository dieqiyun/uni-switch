package sessions

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestClaudeSessionReadOnceForSessionsAndCalls: a first start parsed every
// Claude session for the Sessions page, then scanned each again for
// Requests' calls (#2278). A session read from its start now has its calls
// read on the parse's own pass, and what both pages get is what the two
// reads gave; one that grew is read on from where each was.
func TestClaudeSessionReadOnceForSessionsAndCalls(t *testing.T) {
	claude, _ := setup(t)
	path := filepath.Join(claude, "projects", "-work-night", "33333333-2222-3333-4444-555555555555.jsonl")
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, strings.TrimSuffix(ccMsg(fmt.Sprintf("msg_%02d", i), fmt.Sprintf("2026-09-21T10:%02d:00Z", i), 100+i, 20+i), "\n"))
	}
	writeLines(t, path, lines...)
	forget := func() {
		// Reset first: it writes a save still pending, which would put
		// back a cache file removed before it
		Reset()
		os.Remove(CachePath())
		os.RemoveAll(callCacheDir())
	}

	var mu sync.Mutex
	scans := map[string][]int64{}
	scanHook = func(p string, off int64) {
		if strings.HasPrefix(p, claude) && strings.HasSuffix(p, ".jsonl") {
			mu.Lock()
			scans[p] = append(scans[p], off)
			mu.Unlock()
		}
	}
	t.Cleanup(func() { scanHook = nil })
	counted := func() map[string][]int64 {
		mu.Lock()
		defer mu.Unlock()
		out := scans
		scans = map[string][]int64{}
		return out
	}
	readCallsAll := func() map[string][]Call {
		out := map[string][]Call{}
		for _, s := range CallSources() {
			if s.Agent == "claude" {
				out[s.Path] = ReadCallSource(s)
			}
		}
		return out
	}
	day := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	reference := func() (map[string][]Call, []Session, Stats) {
		forget()
		calls := readCallsAll()
		list, stats := List(0), StatsAt(0, day)
		counted()
		return calls, list, stats
	}

	// as before: calls read first, then the summaries, each on a scan of
	// its own
	wantCalls, wantList, wantStats := reference()
	calls := 0
	for _, cs := range wantCalls {
		calls += len(cs)
	}
	if calls < 40 {
		t.Fatalf("only %d calls read: the fixture tests nothing", calls)
	}

	// the warm-up's order with nothing kept: sessions, then calls
	forget()
	counted()
	list, stats := List(0), StatsAt(0, day)
	got := readCallsAll()
	n := 0
	for p, offs := range counted() {
		if len(offs) != 1 {
			t.Errorf("%s scanned %d times, want once", p, len(offs))
		}
		n++
	}
	if n < 2 {
		t.Fatalf("%d Claude sessions scanned: the fixture tests nothing", n)
	}
	if !reflect.DeepEqual(got, wantCalls) {
		t.Errorf("calls differ from their own read's")
	}
	if !reflect.DeepEqual(list, wantList) || !reflect.DeepEqual(stats, wantStats) {
		t.Errorf("sessions differ from a read without the calls")
	}

	// a session grows: each reader reads on from where it was, and both
	// pages get what a read from nothing gives
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(ccMsg("msg_99", "2026-09-21T11:30:00Z", 500, 50)); err != nil {
		t.Fatal(err)
	}
	f.Close()
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	list, stats = List(0), StatsAt(0, day)
	got = readCallsAll()
	for _, off := range counted()[path] {
		if off == 0 {
			t.Errorf("the grown session was read again from its start")
		}
	}
	wantCalls, wantList, wantStats = reference()
	if !reflect.DeepEqual(got, wantCalls) {
		t.Errorf("after growing: calls differ from a read from nothing")
	}
	if !reflect.DeepEqual(list, wantList) || !reflect.DeepEqual(stats, wantStats) {
		t.Errorf("after growing: sessions differ from a read from nothing")
	}
}
