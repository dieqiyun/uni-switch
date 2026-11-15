package library

import (
	"slices"
	"testing"
)

// An entry of Applied with nothing in it is no record of what uni-switch wrote
// into an agent, and the syncs that only looked at an agent — its hooks,
// its subagents, its MCP file, with nothing of the library's to give it —
// must not leave one behind in library.json: entries grew there per agent
// at every start ({"claude":{}}, then {"claude":{},"codex":{}}), and the
// write they brought into a file otherwise unchanged is what a test of the
// CLI's caught, red once on Linux (#4247). A save drops them, and keeps
// every entry that holds something.
func TestSaveDropsEmptyApplied(t *testing.T) {
	sandbox(t)
	l := &Library{Applied: map[string]*Applied{
		"claude": {},
		"codex":  {MCP: []string{"fs"}},
		"dsh":    {Skills: []string{"pdf"}, Instructions: true, InstrHash: "h"},
		"goose":  {Hooks: []HookEntry{{Name: "check"}}},
		"pi":     {Subagents: []SubagentFile{{Name: "scout"}}},
		"crush":  {ToolRules: []string{"mcp__fs__write"}, Startup: map[string]float64{"fs": 5}},
	}}
	if err := l.save(); err != nil {
		t.Fatal(err)
	}
	l, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := l.Applied["claude"]; ok {
		t.Errorf("claude's empty entry was saved:\n%s", mustJSON(t, l))
	}
	for id, want := range map[string]Applied{
		"codex": {MCP: []string{"fs"}},
		"dsh":   {Skills: []string{"pdf"}, Instructions: true, InstrHash: "h"},
		"goose": {Hooks: []HookEntry{{Name: "check"}}},
		"pi":    {Subagents: []SubagentFile{{Name: "scout"}}},
		"crush": {ToolRules: []string{"mcp__fs__write"}, Startup: map[string]float64{"fs": 5}},
	} {
		a := l.Applied[id]
		if a == nil {
			t.Errorf("%s's entry is gone", id)
			continue
		}
		if a.Instructions != want.Instructions || a.InstrHash != want.InstrHash ||
			!slices.Equal(a.MCP, want.MCP) || !slices.Equal(a.Skills, want.Skills) ||
			!slices.Equal(a.ToolRules, want.ToolRules) || len(a.Startup) != len(want.Startup) ||
			len(a.Hooks) != len(want.Hooks) || len(a.Subagents) != len(want.Subagents) {
			t.Errorf("%s's entry came back %+v, want %+v", id, *a, want)
		}
	}
}

// A sync alone, with nothing in the library to give any agent, is writes
// nothing: every agent it looked at used to take an empty applied entry
// into library.json (#4247).
func TestSyncAloneLeavesNoEmptyApplied(t *testing.T) {
	sandbox(t)
	if _, err := Sync(); err != nil {
		t.Fatal(err)
	}
	l, err := load()
	if err != nil {
		t.Fatal(err)
	}
	for id, a := range l.Applied {
		if a.empty() {
			t.Errorf("the sync left an empty applied entry for %s", id)
		}
	}
}
