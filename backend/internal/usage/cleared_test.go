package usage

import (
	"slices"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// CrazyMouse_TT on X: a local model deployed again is benchmarked from
// nothing. A clear of one model hides its calls up to the clear from the
// Overview, the Requests page, its heatmap and the ledger (the CSV), its
// alias's ids with it, and leaves other models and later calls counted; a
// clear of everything hides every call up to it. The log keeps them, so
// Visit (key spending limits, window holds) still counts them all.
func TestClearedUsageLeavesThePages(t *testing.T) {
	resetUsageTest(t)
	// the clears read last stay read for a test that builds a page without
	// reading them again (readCleared)
	t.Cleanup(func() { clearedAs.Store(nil) })
	now := Clock()
	at := now.Add(-2 * time.Hour)
	Append(Record{Time: at, Agent: "codex", Provider: "ollama", Model: "qwen3:32b", Input: 100, Output: 10, Status: 200})
	Append(Record{Time: at.Add(time.Minute), Agent: "codex", Provider: "ollama", Model: "qwen3-32b-q4", Input: 50, Output: 5, Status: 200})
	Append(Record{Time: at.Add(2 * time.Minute), Agent: "codex", Provider: "ollama", Model: "llama4", Input: 7, Output: 3, Status: 200})
	Append(Record{Time: at.Add(3 * time.Minute), Agent: "codex", Provider: "openai", Model: "qwen3:32b", Input: 9, Output: 1, Status: 200})
	if _, err := provider.SetUsageAlias("qwen3-32b-q4", "qwen3:32b"); err != nil {
		t.Fatal(err)
	}

	models := func() map[string]int {
		out := map[string]int{}
		for _, g := range Summarize(All).Models {
			out[g.Provider+"/"+g.Model] += g.Calls
		}
		return out
	}
	if got := models(); got["ollama/qwen3:32b"] != 2 || got["ollama/llama4"] != 1 || got["openai/qwen3:32b"] != 1 {
		t.Fatalf("before the clear: %v", got)
	}
	// warm every cache the pages read, so the clear has to reach past them
	_ = QueryPage(All, Filter{}, 0, 50)
	_ = HeatmapOf(Filter{})

	if err := settings.ClearUsage("ollama", "qwen3:32b", Clock().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	// a call after the clear is counted
	Append(Record{Time: Clock(), Agent: "codex", Provider: "ollama", Model: "qwen3:32b", Input: 1, Output: 1, Status: 200})

	if got := models(); got["ollama/qwen3:32b"] != 1 || got["ollama/llama4"] != 1 || got["openai/qwen3:32b"] != 1 {
		t.Fatalf("Overview after clearing ollama's qwen3:32b: %v", got)
	}
	if s := Summarize(All); s.Calls != 3 || s.Input != 17 {
		t.Fatalf("Overview totals: %d calls, %d in", s.Calls, s.Input)
	}
	page := QueryPage(All, Filter{}, 0, 50)
	if page.Total != 3 {
		t.Fatalf("Requests after the clear: %d rows", page.Total)
	}
	for _, r := range page.Rows {
		if r.Provider == "ollama" && r.Time.Before(Clock().Add(-time.Hour)) && r.Model != "llama4" {
			t.Fatalf("a cleared row is listed: %+v", r.Record)
		}
	}
	heat := 0
	for _, d := range HeatmapOf(Filter{}).Days {
		heat += d.Calls
	}
	if heat != 3 {
		t.Fatalf("heatmap after the clear: %d calls", heat)
	}
	if rows, _, _ := Ledger(All, Filter{}); len(rows) != 3 {
		t.Fatalf("ledger (CSV) after the clear: %d rows", len(rows))
	}
	// the log is whole: what key limits and window holds read
	seen := 0
	Visit(time.Time{}, func(Record) { seen++ })
	if seen != 5 {
		t.Fatalf("Visit after the clear: %d records, want all 5", seen)
	}

	// everything, now: nothing up to it is counted
	if err := settings.ClearUsage("", "", Clock()); err != nil {
		t.Fatal(err)
	}
	if s := Summarize(All); s.Calls != 0 || len(s.Models) != 0 {
		t.Fatalf("Overview after clearing everything: %d calls, %d models", s.Calls, len(s.Models))
	}
	if page := QueryPage(All, Filter{}, 0, 50); page.Total != 0 {
		t.Fatalf("Requests after clearing everything: %d", page.Total)
	}
	Append(Record{Time: Clock().Add(time.Second), Agent: "pi", Provider: "ollama", Model: "llama4", Input: 2, Output: 2, Status: 200})
	if s := Summarize(All); s.Calls != 1 || !slices.ContainsFunc(s.Agents, func(g Group) bool { return g.ID == "pi" }) {
		t.Fatalf("a call after clearing everything: %+v", s.Agents)
	}
	seen = 0
	Visit(time.Time{}, func(Record) { seen++ })
	if seen != 6 {
		t.Fatalf("Visit after clearing everything: %d", seen)
	}
}
