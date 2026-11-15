package sessions

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// almaSchema is Alma's tables as far as uni-switch reads them, with the
// columns of chat_threads.db that matter here.
const almaSchema = `
CREATE TABLE workspaces (id TEXT PRIMARY KEY, path TEXT NOT NULL, name TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE chat_threads (id TEXT PRIMARY KEY, title TEXT NOT NULL, model TEXT, metadata TEXT DEFAULT '{}',
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL, is_incognito INTEGER DEFAULT 0, workspace_id TEXT, parent_thread_id TEXT);
CREATE TABLE chat_messages (id TEXT, thread_id TEXT NOT NULL, parent_id TEXT, slot_id TEXT, depth TEXT NOT NULL, message TEXT NOT NULL,
 timestamp TEXT NOT NULL, metadata TEXT DEFAULT '{}', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, parent_tool_call_id TEXT);
CREATE INDEX idx_messages_version_info ON chat_messages(thread_id, timestamp, id, slot_id, created_at);
CREATE TABLE usage_records (id TEXT PRIMARY KEY, message_id TEXT NOT NULL, thread_id TEXT NOT NULL, model TEXT, provider_id TEXT,
 date TEXT NOT NULL, input_tokens INTEGER DEFAULT 0, output_tokens INTEGER DEFAULT 0, cached_input_tokens INTEGER DEFAULT 0,
 cache_write_input_tokens INTEGER DEFAULT 0, reasoning_tokens INTEGER DEFAULT 0, total_tokens INTEGER DEFAULT 0,
 timestamp TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE aux_usage_records (id TEXT PRIMARY KEY, purpose TEXT NOT NULL, model TEXT, provider_id TEXT, thread_id TEXT,
 date TEXT NOT NULL, input_tokens INTEGER DEFAULT 0, output_tokens INTEGER DEFAULT 0, cached_input_tokens INTEGER DEFAULT 0,
 cache_write_input_tokens INTEGER DEFAULT 0, reasoning_tokens INTEGER DEFAULT 0, total_tokens INTEGER DEFAULT 0,
 timestamp TEXT NOT NULL, created_at TEXT NOT NULL);`

// almaFixture makes Alma's database in its data folder, in WAL mode as
// Alma keeps it: a chat in a workspace with a tool loop, a subagent's
// reply, a compaction's note and two models' usage; a chat Alma hasn't
// named yet; one opened and left empty; an incognito one; and two that
// Alma's scheduled tasks ran in.
func almaFixture(t *testing.T) string {
	t.Helper()
	dir := AlmaDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "chat_threads.db")
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(almaSchema); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO workspaces VALUES ('ws1', '/work/alma', 'alma', '2026-10-01T09:00:00.000Z', '2026-10-01T09:00:00.000Z')`)
	exec(`INSERT INTO chat_threads (id, title, model, created_at, updated_at, workspace_id) VALUES
	 ('thA', 'Fix the build', 'claude-subscription:claude-opus-5-5', '2026-10-01T10:00:00.000Z', '2026-10-01T10:00:30.000Z', 'ws1'),
	 ('thB', 'New Chat', 'gemini:gemini-3.1-pro-preview', '2026-10-01T11:00:00.000Z', '2026-10-01T11:00:00.000Z', NULL),
	 ('thC', 'New Chat', NULL, '2026-10-01T12:00:00.000Z', '2026-10-01T12:00:00.000Z', NULL)`)
	exec(`INSERT INTO chat_threads (id, title, created_at, updated_at, is_incognito) VALUES
	 ('thD', 'Secret', '2026-10-01T13:00:00.000Z', '2026-10-01T13:00:00.000Z', 1)`)
	// chats Alma's scheduled tasks ran in: one flagged in its metadata, one
	// (from before the flag was set) known only by its title
	exec(`INSERT INTO chat_threads (id, title, created_at, updated_at, metadata) VALUES
	 ('thE', 'Daily digest', '2026-10-01T14:00:00.000Z', '2026-10-01T14:00:00.000Z', '{"isCron":true}'),
	 ('thF', '⏰ Cron: daily digest (Retry 1)', '2026-10-01T15:00:00.000Z', '2026-10-01T15:00:00.000Z', '{}')`)
	msg := func(id, thread, at, message, meta, parent string) {
		t.Helper()
		var p any
		if parent != "" {
			p = parent
		}
		exec(`INSERT INTO chat_messages (id, thread_id, slot_id, depth, message, timestamp, metadata, created_at, updated_at, parent_tool_call_id)
		 VALUES (?, ?, ?, '0.0', ?, ?, ?, ?, ?, ?)`, id, thread, id, message, at, meta, at, at, p)
	}
	msg("thA--user-1", "thA", "2026-10-01T10:00:00.000Z",
		`{"id":"u1","role":"user","parts":[{"type":"text","text":"Please fix the build"},{"type":"file","url":"x","mediaType":"image/png"}]}`, `{"source":"app"}`, "")
	msg("thA--a1", "thA", "2026-10-01T10:00:05.000Z",
		`{"id":"a1","role":"assistant","parts":[{"type":"step-start"},{"type":"reasoning","text":"hm"},
		 {"type":"tool-Bash","toolCallId":"c1","state":"output-available","input":{"command":"make"},"output":"ok"},
		 {"type":"tool-Skill","toolCallId":"c2","state":"output-available","input":{"skill":"image-gen"},"output":"loaded"},
		 {"type":"tool-Task","toolCallId":"c3","state":"output-available","input":"not an object","output":"done"},
		 {"type":"dynamic-tool","toolName":"mcp_search","toolCallId":"c4","state":"output-available","input":{}},
		 {"type":"text","text":"Fixed."}]}`, `{"usage":{"inputTokens":1000}}`, "")
	msg("thA--sub1", "thA", "2026-10-01T10:00:03.000Z",
		`{"id":"s1","role":"assistant","parts":[{"type":"tool-Read","toolCallId":"r1","state":"output-available","input":{"path":"Makefile"}}]}`,
		`{"subagentTaskId":"t1"}`, "c3")
	msg("thA--compact", "thA", "2026-10-01T10:00:20.000Z",
		`{"id":"k1","role":"assistant","parts":[{"type":"text","text":"Summary of the chat so far"}]}`,
		`{"isCompactionIndicator":true,"compactionSummary":"…"}`, "")
	msg("thB--user-1", "thB", "2026-10-01T11:00:01.000Z",
		`{"id":"u2","role":"user","parts":[{"type":"text","text":"What   is\nWAL?"}]}`, `{}`, "")
	msg("thD--user-1", "thD", "2026-10-01T13:00:01.000Z",
		`{"id":"u3","role":"user","parts":[{"type":"text","text":"hidden"}]}`, `{}`, "")
	for _, th := range []string{"thE", "thF"} {
		msg(th+"--user-1", th, "2026-10-01T16:00:01.000Z",
			`{"id":"`+th+`u","role":"user","parts":[{"type":"text","text":"Summarise today's mail"}]}`, `{}`, "")
		msg(th+"--a1", th, "2026-10-01T16:00:02.000Z",
			`{"id":"`+th+`a","role":"assistant","parts":[{"type":"text","text":"Done."}]}`, `{}`, "")
	}
	exec(`INSERT INTO usage_records (id, message_id, thread_id, model, provider_id, date, input_tokens, output_tokens, cached_input_tokens, cache_write_input_tokens, reasoning_tokens, total_tokens, timestamp, created_at) VALUES
	 ('u1', 'thA--a1', 'thA', 'plugin:openai-codex-auth:openai-codex:gpt-6-astra', 'plugin', '2026-10-01', 1000, 50, 600, 0, 10, 1050, '2026-10-01T10:00:05.000Z', '2026-10-01T10:00:05.000Z'),
	 ('u2', 'thA--a1', 'thA', 'claude-subscription:claude-opus-5-5', 'claude-subscription', '2026-10-01', 500, 20, 100, 300, 0, 520, '2026-10-01T10:00:06.000Z', '2026-10-01T10:00:06.000Z')`)
	exec(`INSERT INTO aux_usage_records (id, purpose, model, provider_id, thread_id, date, input_tokens, output_tokens, timestamp, created_at) VALUES
	 ('x1', 'title', 'gemini:gemini-3.1-flash-lite-preview', 'gemini', 'thB', '2026-10-01', 30, 5, '2026-10-01T11:00:02.000Z', '2026-10-01T11:00:02.000Z')`)
	return path
}

func almaSession(t *testing.T, list []Session, id string) Session {
	t.Helper()
	for _, s := range list {
		if s.Agent == "alma" && s.ID == id {
			return s
		}
	}
	t.Fatalf("Alma chat %q not listed: %+v", id, list)
	return Session{}
}

func TestAlmaSessions(t *testing.T) {
	setup(t)
	path := almaFixture(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	list := List(0)
	a := almaSession(t, list, "thA")
	if !a.ReadOnly || a.Resume != "" || a.Title != "Fix the build" || a.Cwd != "/work/alma" {
		t.Errorf("chat A: %+v", a)
	}
	if got := model(a, "gpt-6-astra").Tokens; got != (Tokens{Input: 400, Output: 50, CacheRead: 600}) {
		t.Errorf("gpt-6-astra = %+v, want Alma's input less its cache reads", got)
	}
	if got := model(a, "claude-opus-5-5").Tokens; got != (Tokens{Input: 100, Output: 20, CacheRead: 100, CacheWrite: 300}) {
		t.Errorf("claude-opus-5-5 = %+v, want Alma's input less its cache reads and writes", got)
	}
	if a.Cost == 0 || a.Unpriced != 0 {
		t.Errorf("chat A priced: cost %v, unpriced %d", a.Cost, a.Unpriced)
	}
	if want := time.Date(2026, 10, 1, 10, 0, 20, 0, time.UTC); !a.Last.Equal(want) || !a.Start.Equal(time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("chat A times %v – %v", a.Start, a.Last)
	}
	b := almaSession(t, list, "thB")
	if b.Title != "What is WAL?" || b.Cwd != "" {
		t.Errorf("an unnamed chat is titled by its first prompt: %+v", b)
	}
	if got := model(b, "gemini-3.1-flash-lite-preview").Tokens; got != (Tokens{Input: 30, Output: 5}) {
		t.Errorf("side work's usage = %+v", got)
	}
	for _, s := range list {
		if s.Agent == "alma" && (s.ID == "thC" || s.ID == "thD" || s.ID == "thE" || s.ID == "thF") {
			t.Errorf("an empty, incognito or scheduled chat was listed: %+v", s)
		}
	}

	// the Sessions page: listed, never deletable; prompts and replies of the
	// chat itself, not its subagent's or a compaction's note
	managed := ListAgent("alma")
	if len(managed) != 2 {
		t.Fatalf("ListAgent(alma) = %+v", managed)
	}
	for _, m := range managed {
		if m.Deletable {
			t.Errorf("an Alma chat is deletable: %+v", m)
		}
		if m.ID == "thA" && (m.Messages != 2 || m.Size == 0) {
			t.Errorf("chat A on the page: %d messages, %d bytes", m.Messages, m.Size)
		}
	}
	found := false
	for _, ac := range Agents() {
		if ac.Agent == "alma" {
			found = true
			if ac.Count != 2 || ac.Deletable {
				t.Errorf("Agents: %+v", ac)
			}
		}
	}
	if !found {
		t.Error("Alma missing from Agents()")
	}
	if _, err := Delete("alma", "thA"); err == nil {
		t.Error("Delete took an Alma chat out of Alma's database")
	}

	// the tools and skills it called, its subagent's too
	st := StatsAt(0, time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC))
	var sum Summary
	for _, s := range st.Sessions {
		if s.Key == "alma:thA" {
			sum = s
		}
	}
	if sum.Prompts != 1 || sum.Replies != 1 || sum.ToolCalls != 5 {
		t.Errorf("chat A stats: prompts %d replies %d tools %d", sum.Prompts, sum.Replies, sum.ToolCalls)
	}
	for _, tool := range []string{"Bash", "Skill", "Task", "mcp_search", "Read"} {
		if sum.tools.of(tool) != 1 {
			t.Errorf("tool %s called %d times, want 1 (%v)", tool, sum.tools.of(tool), sum.tools)
		}
	}
	if sum.skills.of("image-gen") != 1 {
		t.Errorf("skills %v", sum.skills)
	}

	found = false
	for _, d := range Dirs() {
		if d == AlmaDir() {
			found = true
		}
	}
	if !found {
		t.Errorf("Dirs() %v leaves out Alma's", Dirs())
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("reading Alma's chats changed its database")
	}
}

// Alma writes to its WAL all the time; a chat is read again when its own
// rows change, though the database file's size and time don't, nor the
// chat's latest time (a reply's usage written after a later message).
func TestAlmaChatReadAgainWhenItChanges(t *testing.T) {
	setup(t)
	path := almaFixture(t)
	if b := almaSession(t, List(0), "thB"); b.Title != "What is WAL?" {
		t.Fatalf("chat B: %+v", b)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE chat_threads SET title = 'Explaining WAL' WHERE id = 'thB'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO usage_records (id, message_id, thread_id, model, provider_id, date, input_tokens, output_tokens, timestamp, created_at)
	 VALUES ('u3', 'thB--a', 'thB', 'gemini:gemini-3.1-pro-preview', 'gemini', '2026-10-01', 70, 9, '2026-10-01T11:00:00.500Z', '2026-10-01T11:00:00.500Z')`); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	b := almaSession(t, List(0), "thB")
	if b.Title != "Explaining WAL" || model(b, "gemini-3.1-pro-preview").Tokens != (Tokens{Input: 70, Output: 9}) {
		t.Errorf("chat B not read again: %+v", b)
	}
}

func TestAlmaModel(t *testing.T) {
	for _, c := range []struct{ model, provider, want string }{
		{"plugin:openai-codex-auth:openai-codex:gpt-5.4", "plugin", "gpt-5.4"},
		{"claude-subscription:claude-opus-4-6", "claude-subscription", "claude-opus-4-6"},
		{"muqbtywmwhw85ee22l:anthropic/claude-opus-5-5", "muqbtywmwhw85ee22l", "anthropic/claude-opus-5-5"},
		{"gemini:gemini-3.1-pro-preview", "", "gemini-3.1-pro-preview"},
		{"ollama:qwen3:8b", "ollama", "qwen3:8b"},
		{"gemini-2.5-pro", "", "gemini-2.5-pro"},
	} {
		if got := almaModel(c.model, c.provider); got != c.want {
			t.Errorf("almaModel(%q, %q) = %q, want %q", c.model, c.provider, got, c.want)
		}
	}
}
