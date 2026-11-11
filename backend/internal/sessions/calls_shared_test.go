package sessions

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The Sessions page's requests arriving together each read every session
// file's calls, and with 68k files the app grew to 85 GB (#2278). Callers
// asking at once share one read, and each is handed a copy of its own.
func TestConcurrentCallsShareOneRead(t *testing.T) {
	d := setupCalls(t)
	writeLines(t, filepath.Join(d.claude, "projects", "-work-app", "sess1.jsonl"),
		claudeMsg("m1", "claude-opus-5-5", 100, 1, 5000, 1000, 1),
		claudeMsg("m2", "claude-opus-5-5", 7, 8, 0, 0, 4))
	const callers = 8
	var reads atomic.Int32
	release := make(chan struct{})
	readingCalls = func() {
		if reads.Add(1) == 1 {
			<-release
		}
	}
	t.Cleanup(func() { readingCalls = nil })
	results := make([][]Call, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = Calls(time.Time{})
		}()
	}
	// release the first read once every other caller waits on it
	deadline := time.Now().Add(10 * time.Second)
	for {
		callsFlights.Lock()
		waiting := 0
		for _, fl := range callsFlights.on {
			waiting += fl.waiters
		}
		callsFlights.Unlock()
		if waiting == callers-1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	wg.Wait()
	if n := reads.Load(); n != 1 {
		t.Fatalf("%d callers asking at once read the files %d times, want 1", callers, n)
	}
	for i, cs := range results {
		if len(cs) != 2 || cs[0].RequestID != "req_m2" {
			t.Fatalf("caller %d got %+v", i, cs)
		}
	}
	results[0][0].Model = "changed"
	for i := 1; i < callers; i++ {
		if results[i][0].Model != "claude-opus-5-5" {
			t.Fatalf("caller %d sees another caller's change: %+v", i, results[i][0])
		}
	}
}

// AgentCalls reads one agent's files only, and its calls are the ones Calls
// gives for that agent.
func TestAgentCallsReadsOneAgent(t *testing.T) {
	d := setupCalls(t)
	writeLines(t, filepath.Join(d.claude, "projects", "-work-app", "sess1.jsonl"),
		claudeMsg("m1", "claude-opus-5-5", 100, 1, 5000, 1000, 1))
	name := "rollout-2026-09-20T10-00-00-0190aaaa-1111-7222-8333-444455556666.jsonl"
	lines := []string{`{"timestamp":"` + stamp(0) + `","type":"session_meta","payload":{"id":"thread-id","session_id":"sess-x","cwd":"/work/start"}}`}
	lines = append(lines, cxTurnLines(1, "t1", "gpt-6-astra", "high")...)
	lines = append(lines, tokenCountLine(2, cxUse(1000, 600, 0, 50, 20), cxUse(1000, 600, 0, 50, 20)), cxDoneLine(5, "t1", 5000, 700))
	writeLines(t, filepath.Join(d.codex, "sessions", "2026", "09", "20", name), lines...)

	codex := AgentCalls(time.Time{}, "codex")
	var want []Call
	for _, c := range Calls(time.Time{}) {
		if c.Agent == "codex" {
			want = append(want, c)
		}
	}
	if len(codex) != 1 || len(want) != 1 || bare(codex[0]) != bare(want[0]) {
		t.Fatalf("codex calls %+v, want %+v", codex, want)
	}
}
