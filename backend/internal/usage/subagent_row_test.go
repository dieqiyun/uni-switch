package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// A call read from a Claude Code subagent's own file names that subagent
// and, from its meta file, the subagent that started it,
// and the subagent and the one that started it are kept when the rows are
// packed away and read back.
func TestRowKeepsSubagent(t *testing.T) {
	f := filepath.FromSlash("/h/.claude/projects/-p/2bd9379e-9a4a-4cb6-ba42-e6b2fb6dc5a5/subagents/agent-a76fc11ece25eafc9.jsonl")
	if r := logRecord(sessions.Call{File: f, Agent: "claude", Model: "m"}); r.Subagent != "a76fc11ece25eafc9" || r.ParentAgent != "" {
		t.Fatalf("from the subagent's file: %+v", r)
	}
	if r := logRecord(sessions.Call{File: filepath.FromSlash("/h/.claude/projects/-p/2bd9379e.jsonl"), Agent: "claude", Model: "m"}); r.Subagent != "" {
		t.Fatalf("from the session's own file: %q", r.Subagent)
	}
	sub := filepath.Join(t.TempDir(), "3ac5fc87", "subagents")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "agent-a143287ac46b82a96.meta.json"), []byte(`{"agentType":"general-purpose","parentAgentId":"a99ca20c03d1813d8","spawnDepth":2}`), 0o644)
	if r := logRecord(sessions.Call{File: filepath.Join(sub, "agent-a143287ac46b82a96.jsonl"), Agent: "claude", Model: "m"}); r.Subagent != "a143287ac46b82a96" || r.ParentAgent != "a99ca20c03d1813d8" {
		t.Fatalf("a subagent a subagent started: %+v", r)
	}
	c := &rowChunk{}
	c.add(Row{Record: Record{Time: time.Now(), Agent: "claude", Session: "conv-1", Subagent: "a1", ParentAgent: "a0"}}, "msg", 1, false)
	for _, chunk := range []*rowChunk{c, c.pack().unpack()} {
		if r := chunk.row(0); r.Subagent != "a1" || r.ParentAgent != "a0" || r.Session != "conv-1" || chunk.Strings[chunk.text(&chunk.Rows[0], rowMsg)] != "msg" {
			t.Fatalf("read back: %+v", r)
		}
	}
}
