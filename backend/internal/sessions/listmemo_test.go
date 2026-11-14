package sessions

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// keyOf is the key a listing gives the file at path.
func keyOf(t *testing.T, fs []file, path string) string {
	t.Helper()
	for _, f := range fs {
		if f.path == path {
			return f.key
		}
	}
	t.Fatalf("%s not listed", path)
	return ""
}

// readCounter counts reads of each file a hook is told of.
type readCounter struct {
	mu sync.Mutex
	n  map[string]int
}

func (c *readCounter) saw(p string) {
	c.mu.Lock()
	c.n[p]++
	c.mu.Unlock()
}

func (c *readCounter) take() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.n
	c.n = map[string]int{}
	return n
}

// Droid's listing, run on every poll, opens a transcript for whose
// subagent it is only when the transcript changed; a transcript it could
// not read is tried again on the next listing, not taken as nobody's.
func TestDroidListingReadsOnlyChangedTranscripts(t *testing.T) {
	inZone(t, 0)
	home := setupAgents(t)
	putDroid(t, home)
	dir := filepath.Join(home, ".factory", "sessions", "-work-droid")
	sub := filepath.Join(dir, "d0000000-0000-4000-8000-0000000000aa.jsonl")
	start := `{"type":"session_start","id":"d0000000-0000-4000-8000-0000000000aa","title":"Sub","cwd":"/work","callingSessionId":"` + droidMain + `"}` + "\n"
	if err := os.WriteFile(sub, []byte(start), 0o644); err != nil {
		t.Fatal(err)
	}
	half := filepath.Join(dir, "d0000000-0000-4000-8000-0000000000bb.jsonl")
	if err := os.WriteFile(half, []byte(`{"type":"session_start","id":"d0000000-0000-4000-8000-0000000000bb","callingSessionId":"`+droidMain), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &readCounter{n: map[string]int{}}
	scanHook = func(p string, _ int64) { c.saw(p) }
	t.Cleanup(func() { scanHook = nil })

	fs := droidFiles()
	if n := c.take(); n[sub] != 1 || n[half] != 1 || len(n) < 3 {
		t.Fatalf("first listing read %v", n)
	}
	if k := keyOf(t, fs, sub); k != "droid:"+droidMain {
		t.Fatalf("subagent keyed %q", k)
	}
	fs = droidFiles()
	if n := c.take(); len(n) != 1 || n[half] != 1 {
		t.Fatalf("nothing changed: read %v, want only the half-written one again", n)
	}
	if k := keyOf(t, fs, sub); k != "droid:"+droidMain {
		t.Fatalf("subagent keyed %q from what was kept", k)
	}

	// appended to: read again, still the same caller
	appendText(t, sub, `{"type":"message","id":"m1","timestamp":"2026-09-27T08:00:00.000Z","message":{"role":"user","content":"go"}}`+"\n")
	fs = droidFiles()
	if n := c.take(); n[sub] != 1 || n[half] != 1 || len(n) != 2 {
		t.Fatalf("one appended to: read %v", n)
	}
	// the half-written line finished: read, and kept from then on
	appendText(t, half, `"}`+"\n")
	fs = droidFiles()
	if k := keyOf(t, fs, half); k != "droid:"+droidMain {
		t.Fatalf("finished line keyed %q", k)
	}
	c.take()
	droidFiles()
	if n := c.take(); len(n) != 0 {
		t.Fatalf("nothing changed: read %v", n)
	}
	// written over without a caller: its own session now
	if err := os.WriteFile(sub, []byte(`{"type":"session_start","id":"d0000000-0000-4000-8000-0000000000aa","title":"Own"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if k := keyOf(t, droidFiles(), sub); k != "droid:d0000000-0000-4000-8000-0000000000aa" {
		t.Fatalf("written over: keyed %q", k)
	}
}

// Grok Build's listing reads a session's summary.json, for whether it is
// a subagent's, only when the summary changed.
func TestGrokListingReadsOnlyChangedSummaries(t *testing.T) {
	inZone(t, 0)
	home := setupAgents(t)
	id := "0190aaaa-0000-7000-8000-0000000000dd"
	dir := filepath.Join(home, ".grok", "sessions", "%2Fwork%2Fgrok", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "updates.jsonl")
	if err := os.WriteFile(p, []byte(`{"timestamp":1790496001}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	summary := filepath.Join(dir, "summary.json")
	if err := os.WriteFile(summary, []byte(`{"info":{"id":"`+id+`"},"session_kind":"subagent","parent_session_id":"`+grokMain+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &readCounter{n: map[string]int{}}
	grokSummaryHook = c.saw
	t.Cleanup(func() { grokSummaryHook = nil })

	fs := grokFiles()
	if n := c.take(); n[summary] != 1 || len(n) < 2 {
		t.Fatalf("first listing read %v", n)
	}
	if k := keyOf(t, fs, p); k != "grok:"+grokMain {
		t.Fatalf("subagent keyed %q", k)
	}
	fs = grokFiles()
	if n := c.take(); len(n) != 0 {
		t.Fatalf("nothing changed: read %v", n)
	}
	if k := keyOf(t, fs, p); k != "grok:"+grokMain {
		t.Fatalf("subagent keyed %q from what was kept", k)
	}
	// the updates grew: the summary is not read for that
	appendText(t, p, `{"timestamp":1790496002}`+"\n")
	grokFiles()
	if n := c.take(); len(n) != 0 {
		t.Fatalf("updates grew: read %v", n)
	}
	// the summary written over, no longer a subagent's: read, its own now
	if err := os.WriteFile(summary, []byte(`{"info":{"id":"`+id+`"},"session_kind":"main","generated_title":"Own"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	fs = grokFiles()
	if n := c.take(); n[summary] != 1 || len(n) != 1 {
		t.Fatalf("summary changed: read %v", n)
	}
	if k := keyOf(t, fs, p); k != "grok:"+id {
		t.Fatalf("written over: keyed %q", k)
	}
}
