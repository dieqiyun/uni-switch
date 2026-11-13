package library

import (
	"path/filepath"
	"strings"
	"testing"
)

// A change to the instructions writes the instructions and nothing else:
// it gave every agent its MCP servers and skills again too, looking through
// every skill folder, which took up to a minute a click on Windows (#1624,
// Hipple). A server taken out of an agent's file by hand stays out of it
// through an instruction made, saved, switched or removed, and comes back
// with the next change to the servers.
func TestInstructionChangeWritesOnlyInstructions(t *testing.T) {
	h := sandbox(t)
	ok(t)(SaveServer("", Server{Name: "fs", Transport: "stdio", Command: "npx", Agents: []string{"codex"}}))
	cfg := filepath.Join(h, ".codex/config.toml")
	if !strings.Contains(read(t, cfg), "fs") {
		t.Fatalf("codex wasn't given the server:\n%s", read(t, cfg))
	}
	write(t, cfg, "")

	shared := "Be brief."
	ok(t)(SaveInstructions(InstructionsChange{Shared: &shared, Agents: []string{"codex"}}))
	work := "Work rules."
	ok(t)(SaveInstructions(InstructionsChange{Create: &InstrSet{ID: "work", Name: "Work"}, Texts: map[string]*string{"work": &work}, Activate: "work"}))
	ok(t)(SaveInstructions(InstructionsChange{Activate: "default"}))
	ok(t)(SaveInstructions(InstructionsChange{Remove: "work"}))
	if s := read(t, filepath.Join(h, ".codex/AGENTS.md")); !strings.Contains(s, "Be brief.") {
		t.Errorf("codex's instructions:\n%s", s)
	}
	if s := read(t, cfg); strings.Contains(s, "fs") {
		t.Errorf("an instruction change wrote the servers again:\n%s", s)
	}

	ok(t)(ServerAgents("fs", []string{"codex"}))
	if s := read(t, cfg); !strings.Contains(s, "fs") {
		t.Errorf("a server change didn't write the servers:\n%s", s)
	}
}
