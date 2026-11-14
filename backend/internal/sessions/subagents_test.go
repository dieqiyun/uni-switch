package sessions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func transcriptText(t *testing.T, tr Transcript) string {
	t.Helper()
	var b strings.Builder
	for _, p := range tr.Parts {
		b.WriteString(p.Text + "\n")
	}
	return b.String()
}

// A Claude Code session lists the subagents beside its file (#2261): each
// one's meta file names its type and the task it was given, and, for one a
// subagent started, that subagent. A compaction's own run is no subagent.
// The meta and line shapes are Claude Code's under ~/.claude/projects,
// their words and ids replaced.
func TestClaudeSubagents(t *testing.T) {
	inZone(t, 0)
	claude, _ := setup(t)
	subs := filepath.Join(claude, "projects", "-work-app", "11111111-2222-3333-4444-555555555555", "subagents")
	writeFile(t, filepath.Join(subs, "agent-a1.meta.json"), `{"agentType":"Explore","description":"Inventory the parser's callers","toolUseId":"toolu_01BZaaaaaaaaaaaaaaaaaaaa","spawnDepth":1,"requestShape":"background","requestNonInteractive":true}`)
	writeFile(t, filepath.Join(subs, "agent-ac1d8e7a80d42178d.meta.json"), `{"agentType":"general-purpose","inheritedWorktreePath":"/work/app/.claude/worktrees/agent-a1","spawnedWithWorktree":true,"description":"Research MCP config","toolUseId":"toolu_01Njaaaaaaaaaaaaaaaaaaaa","parentAgentId":"a1","spawnDepth":2,"requestShape":"background","requestNonInteractive":true}`)
	writeFile(t, filepath.Join(subs, "agent-ac1d8e7a80d42178d.jsonl"),
		`{"parentUuid":null,"isSidechain":true,"promptId":"78ea7f8b-1b22-4b3c-a3b0-24b6fb216210","agentId":"ac1d8e7a80d42178d","type":"user","message":{"role":"user","content":"Research task only (no code changes, no commits)."},"uuid":"n1","timestamp":"2026-09-20T10:03:00.000Z","cwd":"/work/app","sessionId":"11111111-2222-3333-4444-555555555555"}`+"\n"+
			`{"parentUuid":"n1","isSidechain":true,"agentId":"ac1d8e7a80d42178d","message":{"model":"claude-haiku-4-5-20251001","id":"msg_n1","type":"message","role":"assistant","content":[{"type":"text","text":"Kimi reads ~/.kimi/mcp.json"}],"usage":{"input_tokens":10,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":5}},"type":"assistant","uuid":"n2","timestamp":"2026-09-20T10:04:00.000Z","cwd":"/work/app","sessionId":"11111111-2222-3333-4444-555555555555"}`+"\n")
	writeFile(t, filepath.Join(subs, "agent-acompact-baed5063bd614f3d.jsonl"),
		`{"parentUuid":null,"isSidechain":true,"agentId":"acompact-baed5063bd614f3d","type":"progress","data":{"type":"hook_progress","hookEvent":"SessionStart","hookName":"SessionStart:startup","command":"hook"},"timestamp":"2026-09-20T10:05:00.000Z","uuid":"c1","userType":"external"}`+"\n")

	s := find(t, List(0), "claude", "11111111-2222-3333-4444-555555555555")
	if len(s.Subagents) != 2 {
		t.Fatalf("subagents %+v", s.Subagents)
	}
	a, n := s.Subagents[0], s.Subagents[1]
	if a.ID != "a1" || a.Kind != "Explore" || a.Title != "Inventory the parser's callers" || a.Parent != "" || !a.Start.Equal(at("2026-09-20T10:01:00Z")) {
		t.Fatalf("the session's subagent %+v", a)
	}
	if n.ID != "ac1d8e7a80d42178d" || n.Kind != "general-purpose" || n.Title != "Research MCP config" || n.Parent != "a1" {
		t.Fatalf("the subagent's subagent %+v", n)
	}

	tr, err := SubagentTranscript(s, "ac1d8e7a80d42178d")
	if err != nil {
		t.Fatal(err)
	}
	if got := transcriptText(t, tr); !strings.Contains(got, "Research task only") || !strings.Contains(got, "Kimi reads ~/.kimi/mcp.json") {
		t.Fatalf("subagent transcript %q", got)
	}
	// the session's own conversation is not the subagent's
	if got := transcriptText(t, tr); strings.Contains(got, "Search the repo") {
		t.Fatalf("another subagent's words: %q", got)
	}
	if _, err := SubagentTranscript(s, "acompact-baed5063bd614f3d"); !errors.Is(err, ErrNoSubagent) {
		t.Fatalf("a compaction's run: %v", err)
	}
	if _, err := SubagentTranscript(s, "../11111111-2222-3333-4444-555555555555"); !errors.Is(err, ErrNoSubagent) {
		t.Fatalf("an id it doesn't list: %v", err)
	}
}

// Qoder keeps Claude Code's layout, and its subagents show the same way;
// with no meta file, a subagent is titled by what it was first asked.
func TestQoderSubagents(t *testing.T) {
	inZone(t, 0)
	home := setupAgents(t)
	subs := filepath.Join(home, ".qoder", "projects", "-work-q", qoderID, "subagents")
	writeFile(t, filepath.Join(subs, "agent-b2.jsonl"),
		`{"parentUuid":null,"isSidechain":true,"agentId":"b2","type":"user","message":{"role":"user","content":"List the tests"},"uuid":"q1","timestamp":"2026-09-27T09:00:30.000Z","cwd":"/work/q","sessionId":"`+qoderID+`"}`+"\n")
	s := find(t, List(0), "qoder", qoderID)
	var got []string
	for _, sa := range s.Subagents {
		got = append(got, sa.ID+"|"+sa.Title)
	}
	if strings.Join(got, ",") != "a1|,b2|List the tests" {
		t.Fatalf("qoder subagents %q", got)
	}
	tr, err := SubagentTranscript(s, "b2")
	if err != nil || !strings.Contains(transcriptText(t, tr), "List the tests") {
		t.Fatalf("qoder subagent transcript %+v %v", tr, err)
	}
}

// An OpenCode session its task tool started is a subagent of the one that
// started it, not a session of its own; OpenCode 1.x titled it
// "<description> (@<agent> subagent)", 2.x the description alone (task
// tool, opencode 2.0.25), and "Child session - <time>" before either is
// known.
func TestOpenCodeSubagents(t *testing.T) {
	for _, c := range []struct{ named, title, kind string }{
		{"Find the flaky test (@general subagent)", "Find the flaky test", "general"},
		{"Find the flaky test", "Find the flaky test", ""},
		{"Child session - 2026-09-26T10:01:00.000Z", "", ""},
		{"", "", ""},
	} {
		if title, kind := ocSubagentTitle(c.named); title != c.title || kind != c.kind {
			t.Errorf("%q: %q %q", c.named, title, kind)
		}
	}
	for _, db := range []bool{false, true} {
		inZone(t, 0)
		data, _ := setupMore(t)
		if db {
			ocMakeDB(t, filepath.Join(data, "opencode", "opencode.db"))
		}
		s := find(t, List(0), "opencode", ocMain)
		if len(s.Subagents) != 1 || s.Subagents[0].ID != ocChild || s.Subagents[0].Parent != "" || !s.Subagents[0].Start.Equal(at("2026-09-26T10:01:00Z")) {
			t.Fatalf("db %v: %+v", db, s.Subagents)
		}
		if _, err := SubagentTranscript(s, ocChild); err != nil {
			t.Fatalf("db %v: %v", db, err)
		}
	}
}

// A Kimi Code session's agents/agent-<n> are its subagents. Kimi Code 1.4
// (the reporter's files) says nothing more of them than the task each was
// given; 1.5's subagent.spawned record, which the agent that started it
// writes, names its description, type and starter. The 1.5 record is made
// from Kimi Code's schema (agent-core-v2 SubagentSpawned), not a file.
func TestKimiSubagents(t *testing.T) {
	inZone(t, 0)
	claude, _ := setup(t)
	dir := filepath.Join(filepath.Dir(claude), "home", ".kimi-code")
	kimiSessionsIn(t, dir)
	s := find(t, List(0), "kimi", kimiJune)
	if len(s.Subagents) != 1 || s.Subagents[0].ID != "agent-0" || s.Subagents[0].Title != "[redacted]" || s.Subagents[0].Kind != "" {
		t.Fatalf("1.4 subagents %+v", s.Subagents)
	}
	tr, err := SubagentTranscript(s, "agent-0")
	if err != nil || len(tr.Parts) == 0 {
		t.Fatalf("1.4 subagent transcript %+v %v", tr, err)
	}

	main := filepath.Join(filepath.Dir(filepath.Dir(s.Subagents[0].Path)), "main", "wire.jsonl")
	f, err := os.OpenFile(main, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"type":"subagent.spawned","subagentId":"agent-0","subagentName":"coder","parentToolCallId":"call_1","parentAgentId":"main","description":"Port the lexer","runInBackground":false,"time":1781503056900}` + "\n")
	f.Close()
	Reset()
	s = find(t, List(0), "kimi", kimiJune)
	if len(s.Subagents) != 1 || s.Subagents[0].Title != "Port the lexer" || s.Subagents[0].Kind != "coder" || s.Subagents[0].Parent != "" {
		t.Fatalf("1.5 subagents %+v", s.Subagents)
	}
}

// #2290 (infinitr0us): OpenCode 2.0.29 names a child session's agent in
// session_v2.agent and titles it with the task alone ("Port Phase2
// physics", no "(@general subagent)"), so the type comes from the column.
// A child whose row names no agent still takes it from an older title.
func TestOpenCodeV2SubagentKind(t *testing.T) {
	inZone(t, 0)
	data, _ := setupMore(t)
	db := ocMakeV2(t, filepath.Join(data, "opencode", "opencode.db"))
	const older = "ses_1a2b3c4d5e6fEEEEEEEEEEEEEE"
	for _, q := range []string{
		`UPDATE session_v2 SET agent = 'build' WHERE id = '` + ocMain + `'`,
		`UPDATE session_v2 SET agent = 'explore', title = 'Port Phase2 physics', version = '2.0.29' WHERE id = '` + ocChild + `'`,
		`INSERT INTO session_v2 (id, project_id, parent_id, slug, directory, title, version, time_created, time_updated) VALUES
			('` + older + `', 'global', '` + ocMain + `', 'old-heron', '/work/oc', 'Find the flaky test (@general subagent)', '2.0.18', 1790416870000, 1790416870000)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s := find(t, List(0), "opencode", ocMain)
	var got []string
	for _, sa := range s.Subagents {
		got = append(got, sa.ID+"|"+sa.Title+"|"+sa.Kind)
	}
	if want := ocChild + "|Port Phase2 physics|explore," + older + "|Find the flaky test|general"; strings.Join(got, ",") != want {
		t.Fatalf("subagents %q, want %q", got, want)
	}
}

// #2290 (infinitr0us): a Kimi Code 2.1.1 subagent's own wire file starts
// with the task its parent gave it, a turn.prompt from a system_trigger
// named subagent. It opens as the subagent's first message, as a Claude
// Code or OpenCode subagent's task does, not as folded context; another
// system_trigger in that file, and one in the session's own file, stay
// folded. The records are in the shape of Kimi Code's 1.5 wire files.
func TestKimiSubagentTask(t *testing.T) {
	inZone(t, 0)
	claude, _ := setup(t)
	dir := filepath.Join(filepath.Dir(claude), "home", ".kimi-code")
	kimiSessionsIn(t, dir)
	s := find(t, List(0), "kimi", kimiJune)
	agents := filepath.Dir(filepath.Dir(s.Subagents[0].Path))
	writeFile(t, filepath.Join(agents, "agent-1", "wire.jsonl"), strings.Join([]string{
		`{"type":"metadata","protocol_version":"1.5","created_at":1781503100000}`,
		`{"type":"turn.prompt","agentId":"agent-1","input":[{"type":"text","text":"Map every caller of the physics step"}],"origin":{"kind":"system_trigger","name":"subagent"},"promptId":"p1","turnId":0,"time":1781503100010}`,
		`{"type":"context.append_message","agentId":"agent-1","message":{"role":"user","content":[{"type":"text","text":"Map every caller of the physics step"}],"toolCalls":[],"origin":{"kind":"system_trigger","name":"subagent"}},"time":1781503100010}`,
		`{"type":"context.append_loop_event","agentId":"agent-1","event":{"type":"content.part","uuid":"u1","turnId":"0","step":1,"stepUuid":"s1","part":{"type":"text","text":"Three callers."}},"time":1781503100500}`,
		`{"type":"turn.prompt","agentId":"agent-1","input":[{"type":"text","text":"a background task finished"}],"origin":{"kind":"system_trigger","name":"background_task"},"promptId":"p2","turnId":1,"time":1781503100600}`,
	}, "\n")+"\n")
	f, err := os.OpenFile(filepath.Join(agents, "main", "wire.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"type":"subagent.spawned","subagentId":"agent-1","subagentName":"explore","parentToolCallId":"call_2","parentAgentId":"main","description":"Map the callers","runInBackground":true,"time":1781503100000}` + "\n")
	f.WriteString(`{"type":"turn.prompt","agentId":"main","input":[{"type":"text","text":"subagent agent-1 finished"}],"origin":{"kind":"system_trigger","name":"subagent"},"promptId":"p3","turnId":9,"time":1781503200000}` + "\n")
	f.Close()
	Reset()
	s = find(t, List(0), "kimi", kimiJune)
	tr, err := SubagentTranscript(s, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range tr.Parts {
		got = append(got, p.Role+"/"+p.Kind+"|"+p.Text)
	}
	want := "user/text|Map every caller of the physics step,assistant/text|Three callers.,user/context|a background task finished"
	if strings.Join(got, ",") != want {
		t.Fatalf("subagent transcript %q, want %q", got, want)
	}
	main, err := TranscriptOf(s)
	if err != nil {
		t.Fatal(err)
	}
	last := main.Parts[len(main.Parts)-1]
	if last.Kind != "context" || last.Text != "subagent agent-1 finished" {
		t.Fatalf("the session's own system_trigger: %+v", last)
	}
}
