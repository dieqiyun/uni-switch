package settings

import (
	"os"
	"testing"
	"time"
)

// A clear of usage from the Usage page (CrazyMouse_TT on X) keeps one mark a
// model: a later clear of the same model, or of everything, takes the
// place of an earlier one, and a clear of a provider names its model.
func TestClearUsageKeepsOneMarkAModel(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	if err := ClearUsage("ollama", "qwen3", at); err != nil {
		t.Fatal(err)
	}
	if err := ClearUsage("ollama", "llama4", at); err != nil {
		t.Fatal(err)
	}
	// the same model again, later: one mark, the later
	if err := ClearUsage(" ollama ", "qwen3 ", at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got := UsageClearedNow()
	if len(got) != 2 || got[0].Model != "llama4" || got[1].Model != "qwen3" || got[1].At != at.Add(time.Hour).UnixMilli() {
		t.Fatalf("after three clears of two models: %+v", got)
	}
	if s := Load(); len(s.UsageCleared) != 2 {
		t.Fatalf("Load: %+v", s.UsageCleared)
	}
	if err := ClearUsage("ollama", "", at); err == nil {
		t.Fatal("a clear of a provider with no model was taken")
	}
	if err := ClearUsage("", "", time.Time{}); err == nil {
		t.Fatal("a clear with no time was taken")
	}
	// everything, at the latest: the models' marks it covers go
	if err := ClearUsage("", "", at.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := UsageClearedNow(); len(got) != 1 || got[0].Provider != "" || got[0].Model != "" {
		t.Fatalf("after clearing everything: %+v", got)
	}
	// a model cleared after that is kept beside it
	if err := ClearUsage("ollama", "qwen3", at.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := UsageClearedNow(); len(got) != 2 {
		t.Fatalf("a model cleared after everything: %+v", got)
	}
}
