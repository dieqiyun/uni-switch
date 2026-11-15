package sessions

import (
	"os"
	"path/filepath"
	"testing"
)

// A Claude Code subagent's conversation is a file of its own beside the
// session's, named by the agentId its lines carry; a workflow's agents
// sit a folder deeper. The shapes are those under ~/.claude/projects.
func TestSubagentOf(t *testing.T) {
	proj := filepath.FromSlash("/h/.claude/projects/-Users-me-app")
	for p, want := range map[string]string{
		"2bd9379e-9a4a-4cb6-ba42-e6b2fb6dc5a5/subagents/agent-a76fc11ece25eafc9.jsonl":                           "a76fc11ece25eafc9",
		"4c056ab3-f2e6-4918-b774-da5c713ce1e3/subagents/workflows/wf_e241ae3e-9ce/agent-aa06d649dca14e7f9.jsonl": "aa06d649dca14e7f9",
		"2bd9379e-9a4a-4cb6-ba42-e6b2fb6dc5a5.jsonl":                                                             "",
		"2bd9379e-9a4a-4cb6-ba42-e6b2fb6dc5a5/subagents/agent-a76fc11ece25eafc9.meta.json":                       "",
		"2bd9379e-9a4a-4cb6-ba42-e6b2fb6dc5a5/subagents/notes.jsonl":                                             "",
		"2bd9379e-9a4a-4cb6-ba42-e6b2fb6dc5a5/workflows/wf_1/agent-a1.jsonl":                                     "",
		"agent-a1.jsonl": "",
	} {
		if got := SubagentOf(filepath.Join(proj, filepath.FromSlash(p))); got != want {
			t.Errorf("%s: %q, want %q", p, got, want)
		}
	}
}

// A subagent another started says which one in its meta file; the bytes
// are Claude Code 2.1.296's for a subagent spawned by a subagent, and for
// the one the conversation spawned.
func TestSubagentParent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "3ac5fc87-6335-4971-9609-0fbdc17742ce", "subagents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, s string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	nested := write("agent-a143287ac46b82a96.jsonl", "{}\n")
	write("agent-a143287ac46b82a96.meta.json", `{"agentType":"general-purpose","description":"check","toolUseId":"toolu_01","parentAgentId":"a99ca20c03d1813d8","spawnDepth":2,"requestShape":"background","requestNonInteractive":true}`)
	first := write("agent-a99ca20c03d1813d8.jsonl", "{}\n")
	write("agent-a99ca20c03d1813d8.meta.json", `{"agentType":"general-purpose","description":"check","toolUseId":"toolu_01","spawnDepth":1,"requestShape":"background","requestNonInteractive":true}`)
	if got := SubagentParent(nested); got != "a99ca20c03d1813d8" {
		t.Fatalf("nested: %q", got)
	}
	if got := SubagentParent(first); got != "" {
		t.Fatalf("started by the conversation: %q", got)
	}
	// no meta file yet is not known, so it is read again once there is one
	late := write("agent-a5.jsonl", "{}\n")
	if got := SubagentParent(late); got != "" {
		t.Fatalf("no meta: %q", got)
	}
	write("agent-a5.meta.json", `{"parentAgentId":"a99ca20c03d1813d8","spawnDepth":2}`)
	if got := SubagentParent(late); got != "a99ca20c03d1813d8" {
		t.Fatalf("meta written later: %q", got)
	}
	if got := SubagentParent(filepath.Join(filepath.Dir(dir), "x.jsonl")); got != "" {
		t.Fatalf("a session's own file: %q", got)
	}
}
