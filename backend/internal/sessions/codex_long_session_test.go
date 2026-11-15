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

// longRollout is calls [from, to) of a Codex session: each a turn with the
// user's message, a shell call, its 4 KB output, the usage line and the
// turn's end, as a working rollout writes them.
func longRollout(from, to int) []string {
	var lines []string
	if from == 0 {
		lines = append(lines, `{"timestamp":"`+stamp(0)+`","type":"session_meta","payload":{"id":"0190aaaa-1111-7222-8333-444455556666","cwd":"/work/it"}}`)
	}
	out := strings.Repeat("x", 4000)
	for i := from; i < to; i++ {
		turn := fmt.Sprintf("t%d", i)
		lines = append(lines, cxTurnLines(i*3, turn, "gpt-6-astra", "high")...)
		lines = append(lines,
			fmt.Sprintf(`{"timestamp":"%s","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"step %d"}]}}`, stamp(i*3), i),
			fmt.Sprintf(`{"timestamp":"%s","type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{}","call_id":"c%d"}}`, stamp(i*3+1), i),
			fmt.Sprintf(`{"timestamp":"%s","type":"response_item","payload":{"type":"function_call_output","call_id":"c%d","output":"%s"}}`, stamp(i*3+1), i, out),
			tokenCountLine(i*3+2, cxUse(100*(i+1), 40*(i+1), 0, 10*(i+1), 2*(i+1)), cxUse(100, 40, 0, 10, 2)),
			cxDoneLine(i*3+2, turn, 1000, 100),
		)
	}
	return lines
}

// longSessionCalls is past the shared revision budget: about six index
// entries a call (yetone/uni-switch-releases#24's session was longer still).
const longSessionCalls = 2000

// TestLongCodexSessionAppendReadsOnlyTheAppend: a Codex session being
// written, longer than the shared revision budget, is read on from where
// the last read ended, by the Requests reader and the summary alike. Before
// #24 each append parsed the whole rollout again from its first line.
func TestLongCodexSessionAppendReadsOnlyTheAppend(t *testing.T) {
	d := setupCalls(t)
	path := filepath.Join(d.codex, "sessions", "rollout-2026-09-20T10-00-00-0190aaaa-1111-7222-8333-444455556666.jsonl")
	writeLines(t, path, longRollout(0, longSessionCalls)...)
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
	read := func() ([]Call, Session) {
		t.Helper()
		var calls []Call
		for _, s := range CallSources() {
			if s.Path == path {
				calls = ReadCallSource(s)
			}
		}
		ss := List(0)
		if len(ss) != 1 {
			t.Fatalf("%d sessions", len(ss))
		}
		return calls, ss[0]
	}
	if cs, _ := read(); len(cs) != longSessionCalls {
		t.Fatalf("%d calls", len(cs))
	}
	if c, ok := callContinuations[path]; ok && c.state.revisionWeight() <= revisionEntries {
		w := c.state.revisionWeight()
		t.Fatalf("index weight %d is inside the shared budget; the fixture is not long enough", w)
	}
	var calls []Call
	var summary Session
	for k := 0; k < 3; k++ {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		appendText(t, path, strings.Join(longRollout(longSessionCalls+k, longSessionCalls+k+1), "\n")+"\n")
		mu.Lock()
		starts = nil
		mu.Unlock()
		calls, summary = read()
		if len(calls) != longSessionCalls+k+1 {
			t.Fatalf("append %d: %d calls", k, len(calls))
		}
		mu.Lock()
		got := append([]int64(nil), starts...)
		mu.Unlock()
		if len(got) == 0 {
			t.Fatalf("append %d: not read", k)
		}
		for _, off := range got {
			if off != st.Size() {
				t.Fatalf("append %d: read from %d, not where the last read ended (%d): %v", k, off, st.Size(), got)
			}
		}
	}

	// What was read on is what a fresh read of the whole file gives.
	scanHook = nil
	Reset()
	if err := os.RemoveAll(filepath.Dir(CachePath())); err != nil {
		t.Fatal(err)
	}
	fresh, freshSummary := read()
	for i := range calls {
		if !reflect.DeepEqual(calls[i], fresh[i]) {
			t.Fatalf("call %d read on:\n%+v\nread fresh:\n%+v", i, calls[i], fresh[i])
		}
	}
	if len(calls) != len(fresh) {
		t.Fatalf("%d calls read on, %d fresh", len(calls), len(fresh))
	}
	if !reflect.DeepEqual(summary, freshSummary) {
		t.Fatalf("summary read on:\n%+v\nread fresh:\n%+v", summary, freshSummary)
	}
}

// BenchmarkLongCodexSessionAppend is one turn appended to a long Codex
// session, then read by the Requests reader and the summary, as a tray poll
// does while Codex runs (yetone/uni-switch-releases#24).
func BenchmarkLongCodexSessionAppend(b *testing.B) {
	d := setupCalls(b)
	path := filepath.Join(d.codex, "sessions", "rollout-2026-09-20T10-00-00-0190aaaa-1111-7222-8333-444455556666.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(longRollout(0, longSessionCalls), "\n")+"\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	read := func() {
		for _, s := range CallSources() {
			if s.Path == path {
				ReadCallSource(s)
			}
		}
		List(0)
	}
	read()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		if _, err := f.WriteString(strings.Join(longRollout(longSessionCalls+i, longSessionCalls+i+1), "\n") + "\n"); err != nil {
			b.Fatal(err)
		}
		// a later mtime, as a real append a moment after the last has
		at := time.Now().Add(time.Duration(i+1) * time.Millisecond)
		_ = os.Chtimes(path, at, at)
		b.StartTimer()
		read()
	}
}
