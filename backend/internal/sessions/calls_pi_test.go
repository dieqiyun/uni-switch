package sessions

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"
)

// #1951 (Elenoxe): Pi's and omp's calls on a provider of their own reach
// the usage ledger. The sessions are Pi 1.1.0's and omp 18.9.1's, written
// in a sandbox against a relay of their own (testdata/pi-calls,
// testdata/omp-calls; the system prompt's line left out, the sandbox's
// folder written /work/relay): two calls, a fork of that session with one
// more, and a session whose two calls failed (Connection error.) before it
// went on through uni-switch's entry, those lines as the owner's own Pi wrote
// them, the reply's text left out. The calls through uni-switch are the
// gateway's to count; a failed call is none; the fork's copy of its
// parent's calls are the parent's.
func TestPiCalls(t *testing.T) {
	claude, _ := setup(t)
	dir := filepath.Dir(claude)
	copyTree(t, "testdata/pi-calls", filepath.Join(dir, "pi"))
	copyTree(t, "testdata/omp-calls/agent", filepath.Join(dir, "home", ".omp", "agent"))
	const parent, fork = "01a1343b-2288-73d4-b1b4-6fb13abf0069", "01a1343b-9a0f-7603-b371-4124126b8b05"
	by := map[string][]Call{}
	for _, c := range Calls(time.Time{}) {
		by[c.Agent] = append(by[c.Agent], c)
		if c.Upstream == "uni-switch" || c.Upstream == "dead" {
			t.Errorf("a call through uni-switch or one that failed: %+v", c)
		}
	}
	want := []struct {
		session string
		at      string
		input   int
		millis  int64
	}{
		{parent, "2026-10-13T07:47:34.522Z", 201, 628},
		{parent, "2026-10-13T07:47:36.296Z", 202, 28},
		{fork, "2026-10-13T07:48:04.064Z", 203, 74},
	}
	got := by["pi"]
	if len(got) != len(want) {
		t.Fatalf("pi: %d calls, want %d: %+v", len(got), len(want), got)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].Time.Before(got[j].Time) })
	for i, w := range want {
		c := got[i]
		at, _ := time.Parse(time.RFC3339Nano, w.at)
		if c.Session != w.session || !c.Time.Equal(at) || c.Model != "deepseek-v4" || c.Upstream != "relay" || c.Cwd != "/work/relay" ||
			c.Tokens != (Tokens{Input: w.input, Output: 30, CacheRead: 1000}) || c.Millis != w.millis || c.Msg == "" {
			t.Errorf("pi call %d: %+v", i, c)
		}
	}
	// the fork's file alone holds its own call, whichever file Calls reads
	// first
	for _, f := range piFiles() {
		if f.key == "pi:"+fork {
			if st := readCalls(f); len(st.Calls) != 1 {
				t.Errorf("the fork's own calls: %+v", st.Calls)
			}
		}
	}
	o := by["omp"]
	if len(o) != 1 || o[0].Session != "01a1343c-ec54-759d-9aba-563bba5be0cf" || o[0].Upstream != "relay" ||
		o[0].Tokens != (Tokens{Input: 204, Output: 30, CacheRead: 1000}) || o[0].Millis != 21 || o[0].TTFT != 20 {
		t.Fatalf("omp: %+v", o)
	}
	// and a call holds what the session's summary holds for it: the
	// Sessions page and the Usage page tell the same tokens
	sum := map[string]Tokens{}
	for _, c := range got {
		s := sum[c.Session]
		s.add(c.Tokens)
		sum[c.Session] = s
	}
	for _, s := range List(0) {
		if s.Agent == "pi" && (s.ID == parent || s.ID == fork) && s.Tokens != sum[s.ID] {
			t.Errorf("session %s: %+v, its calls %+v", s.ID, s.Tokens, sum[s.ID])
		}
	}
}

// What a Pi session counts beside its replies is a call too: a tool's
// model work, a "usage" entry's call (a cache warm) on the provider it
// names; a compaction's on the model in use, here uni-switch's, is the
// gateway's (testdata/pi).
func TestPiCallsBesideReplies(t *testing.T) {
	_, _ = setupMore(t)
	got := map[string]Tokens{}
	for _, c := range Calls(time.Time{}) {
		if c.Agent == "pi" && c.Session == "0199aaaa-1111-7222-8333-444455556666" {
			got[c.Msg[:len("pi:e0000005")]] = c.Tokens
		}
	}
	want := map[string]Tokens{
		"pi:e0000005": {Input: 100, Output: 50, CacheRead: 1000, CacheWrite: 200},
		"pi:e0000006": {Input: 10, Output: 5},
		"pi:e0000008": {CacheRead: 300},
	}
	if len(got) != len(want) {
		t.Fatalf("calls %+v, want %+v", got, want)
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: %+v, want %+v", k, got[k], w)
		}
	}
}

// A Pi session file that grew is read on from where the last read left
// off, with what its earlier lines said (its cwd, a fork's start, the
// model in use) carried on, in memory and in the disk shard; a half
// line waits for the rest, and a file cut shorter or written over is
// read whole again.
func TestPiCallsReadOnlyWhatWasAppended(t *testing.T) {
	claude, _ := setup(t)
	dir := filepath.Join(filepath.Dir(claude), "pi", "sessions", "--work-grow--")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "2026-10-13T08-00-00-000Z_01a1343b-0000-7000-8000-000000000077.jsonl")
	t0 := time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)
	// a fork: what was written before its own time is its parent's
	head := `{"type":"session","version":3,"id":"01a1343b-0000-7000-8000-000000000077","timestamp":"2026-10-13T08:00:00.000Z","cwd":"/work/grow","parentSession":"/p.jsonl"}` + "\n" +
		`{"type":"model_change","id":"m0","timestamp":"2026-10-13T08:00:00.000Z","provider":"relay","modelId":"deepseek-v4"}` + "\n"
	// a compaction's usage is on the provider and model in use
	rec := func(i int) string {
		at := t0.Add(time.Duration(i) * time.Second).Format("2006-01-02T15:04:05.000Z")
		return `{"type":"compaction","id":"c` + strconv.Itoa(i+100) + `","timestamp":"` + at + `","usage":{"input":` + strconv.Itoa(20000+i) + `,"output":5,"cacheRead":0,"cacheWrite":0}}`
	}
	write := func(lines ...string) {
		t.Helper()
		b := head
		for _, l := range lines {
			b += l + "\n"
		}
		if err := os.WriteFile(path, []byte(b), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(rec(-5), rec(1), rec(2))
	var mu sync.Mutex
	var starts []int64
	scanHook = func(p string, off int64) {
		if p == path {
			mu.Lock()
			starts = append(starts, off)
			mu.Unlock()
		}
	}
	t.Cleanup(func() { scanHook = nil })
	read := func(what string, from int64, want ...int) {
		t.Helper()
		mu.Lock()
		starts = nil
		mu.Unlock()
		var calls []Call
		for _, s := range CallSources() {
			if s.Path == path {
				calls = ReadCallSource(s)
			}
		}
		mu.Lock()
		got := append([]int64(nil), starts...)
		mu.Unlock()
		if len(got) == 0 {
			t.Fatalf("%s: not read", what)
		}
		for _, off := range got {
			if off != from {
				t.Fatalf("%s: read from %d, want %d (%v)", what, off, from, got)
			}
		}
		var in []int
		for _, c := range calls {
			if c.Model != "deepseek-v4" || c.Upstream != "relay" || c.Cwd != "/work/grow" || c.Agent != "pi" {
				t.Fatalf("%s: call %+v lost what the earlier lines said", what, c)
			}
			in = append(in, c.Input-20000)
		}
		if !slices.Equal(in, want) {
			t.Fatalf("%s: calls %v, want %v", what, in, want)
		}
	}
	size := func() int64 {
		t.Helper()
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return st.Size()
	}
	read("first read", 0, 1, 2)

	end := size()
	appendText(t, path, rec(3)+"\n"+rec(-4)+"\n"+rec(5)[:40])
	read("appended, half a line after", end, 1, 2, 3)

	end += int64(len(rec(3)) + len(rec(-4)) + 2)
	appendText(t, path, rec(5)[40:]+"\n")
	read("the half line finished", end, 1, 2, 3, 5)

	// read again after the in-memory reads are gone: from the disk shard,
	// with what it carried
	resetCalls()
	end = size()
	appendText(t, path, rec(6)+"\n")
	read("from the disk shard", end, 1, 2, 3, 5, 6)

	// cut shorter: read whole again
	write(rec(7))
	read("cut shorter", 0, 7)

	// written over, longer than it was: read whole again
	write(rec(8), rec(9), rec(10))
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	read("written over", 0, 8, 9, 10)
}
