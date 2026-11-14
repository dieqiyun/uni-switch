package usage

import (
	"slices"
	"testing"
	"time"
)

// CodexUsed is the models Codex asked for, the latest first, its own calls
// alone; a model Codex uses for the first time is in it at once, though
// the list is otherwise read again a minute on (codexcat.Fit puts these in
// the first 100 the Codex app reads).
func TestCodexUsed(t *testing.T) {
	pageHome(t)
	now := time.Now()
	Append(Record{Time: now.Add(-40 * 24 * time.Hour), Agent: "codex", Provider: "p", Requested: "p/too-old", Status: 200})
	Append(Record{Time: now.Add(-3 * time.Hour), Agent: "codex", Provider: "p", Requested: "p/a", Status: 200})
	Append(Record{Time: now.Add(-2 * time.Hour), Agent: "codex", Provider: "p", Requested: "p/b", Status: 500})
	Append(Record{Time: now.Add(-90 * time.Minute), Agent: "claude", Provider: "p", Requested: "p/claude-only", Status: 200})
	Append(Record{Time: now.Add(-80 * time.Minute), Agent: "codex", Provider: "p", Requested: "p/keyed", CallerKeyID: "k1", Status: 200})
	Append(Record{Time: now.Add(-time.Hour), Agent: "codex", Provider: "p", Requested: "p/a", Status: 200})
	if got, want := CodexUsed(), []string{"p/a", "p/keyed", "p/b"}; !slices.Equal(got, want) {
		t.Fatalf("CodexUsed %v, want %v", got, want)
	}
	if got := CodexUsedBy("k1"); !slices.Equal(got, []string{"p/keyed"}) {
		t.Errorf("CodexUsedBy(k1) %v", got)
	}
	if got := CodexUsedBy(""); !slices.Equal(got, []string{"p/a", "p/b"}) {
		t.Errorf("CodexUsedBy(\"\") %v", got)
	}
	Append(Record{Time: now.Add(-time.Minute), Agent: "codex", Provider: "p", Requested: "p/new", Status: 200})
	if got := CodexUsed(); !slices.Contains(got, "p/new") {
		t.Errorf("a model used for the first time is not in %v", got)
	}
}
