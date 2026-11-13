package sessions

import (
	"path/filepath"
	"testing"
)

// The stats, which the Sessions page and the overview poll, read no
// subagent's file to tell which sessions are listed: only the files that
// changed are read. They built every session's subagents, and each
// subagent's task was read again from its file at every start, and on
// every poll for one whose first lines name none: 3.5 GB of a live
// uni-switch's 6.3 GB of session scans in 48 minutes.
func TestStatsReadNoSubagentFile(t *testing.T) {
	inZone(t, 0)
	claude, _ := setup(t)
	sub := filepath.Join(claude, "projects", "-work-app", "11111111-2222-3333-4444-555555555555", "subagents", "agent-b2.jsonl")
	writeFile(t, sub, ccHookLine(0)+ccHookLine(1))
	StatsAndNative(0) // every file read once
	Saved()
	// a restart: the parses come back from the disk, nothing else kept
	Reset()
	ccTasks.Clear()
	ccMetas.Clear()
	scans := scansOf(t, sub)
	for range 3 {
		StatsAndNative(0)
		StatsAndNative(30)
		StatsFor(7)
	}
	if n := scans(); n != 0 {
		t.Fatalf("the stats read an unchanged subagent's file %d times", n)
	}
	// the Sessions list still names the subagent and reads its task
	s := find(t, List(0), "claude", "11111111-2222-3333-4444-555555555555")
	found := false
	for _, sa := range s.Subagents {
		found = found || sa.ID == "b2"
	}
	if !found {
		t.Fatalf("subagents %+v", s.Subagents)
	}
}
