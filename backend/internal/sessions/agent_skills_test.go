package sessions

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The sessions under testdata/opencode-skills, cline-skills and dsh-skills
// are real ones, run in a sandbox home through uni-switch's gateway, each
// with a skill greet-user: one where the model runs a command and then
// calls the skill tool, one where the user picks or types the skill, and
// one with /nonexistentzz, which no skill has. They are byte for byte as
// each agent wrote them (OpenCode's as its database's own .dump of
// session_v2 and session_message; dsh's working-directory folder is named
// shorter). MaxMooreMy on X asked whether every agent's skills count;
// these three counted none.

// agentSessions are the Sessions statistics' summaries of an agent's
// sessions on 2026-10-30 (UTC), by id.
func agentSessions(t *testing.T, agent string) (map[string]Summary, Stats) {
	t.Helper()
	s := statsAt(0, time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC))
	by := map[string]Summary{}
	for _, x := range s.Sessions {
		if x.Agent == agent {
			by[x.ID] = x
		}
	}
	return by, s
}

// OpenCode 2.0.25: a reply's content has {"type":"tool","name":"skill",
// "state":{"input":{"id":"greet-user"}}}; a prompt with the skill picked
// (@greet-user in the TUI, sent here as the TUI sends it) lists it in
// skills; a skill row is one brought in by itself.
func TestOpenCodeToolsAndSkills(t *testing.T) {
	inZone(t, 0)
	setupAgents(t)
	if err := os.MkdirAll(OpenCodeDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	dump, err := os.ReadFile("testdata/opencode-skills/opencode.sql")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(OpenCodeDir(), "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(dump)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	Reset()

	by, s := agentSessions(t, "opencode")
	if len(by) != 6 {
		t.Fatalf("want the 6 OpenCode sessions, got %d", len(by))
	}
	called := by["ses_e753a44afffeaDlZRyBkvVR0wE"] // cat notes.txt, then the skill
	if called.ToolCalls != 2 || called.tools.of("read") != 1 || called.tools.of("skill") != 1 {
		t.Fatalf("tools %d %v", called.ToolCalls, called.tools)
	}
	if called.skills.of("greet-user") != 1 || len(called.skills) != 1 {
		t.Fatalf("the skill tool's skill: %v", called.skills)
	}
	// /greet-user typed in opencode run: sent as typed, the model calls the tool
	if x := by["ses_e7539d2a5ffePUVJFMb1gKVccL"]; x.tools.of("skill") != 1 || x.skills.of("greet-user") != 1 {
		t.Fatalf("/greet-user: %v %v", x.tools, x.skills)
	}
	// @greet-user picked: no tool call, the skill in the prompt's skills
	if x := by["ses_e75393cbaffe9qtPD0p9PI6xYK"]; x.ToolCalls != 0 || x.skills.of("greet-user") != 1 || len(x.skills) != 1 {
		t.Fatalf("@greet-user picked: %d %v", x.ToolCalls, x.skills)
	}
	// a skill row
	if x := by["ses_e7538abfbffe2oKYxDK1OHgsrO"]; x.ToolCalls != 0 || x.skills.of("greet-user") != 1 {
		t.Fatalf("a skill row: %d %v", x.ToolCalls, x.skills)
	}
	// @greet-user only as text, and /nonexistentzz: no skill
	for _, id := range []string{"ses_e753a0f28ffe4fFER8Ll41K3GY", "ses_e75386b77ffeTIwbQnkzH3CYw8"} {
		if x := by[id]; x.ToolCalls != 0 || len(x.skills) != 0 {
			t.Fatalf("%s: %d %v", id, x.ToolCalls, x.skills)
		}
	}
	o := s.Overview("", "", "", 10)
	if o.Skills.Top[0].Name != "greet-user" || o.Skills.Top[0].Agents["opencode"] != 4 {
		t.Fatalf("overview skills %+v", o.Skills)
	}
}

// Cline 3.0.70: Anthropic's content blocks, its skills tool's input
// {"skill": "greet-user"}. /greet-user with the skills tool on is sent as
// typed and the model calls the tool; with it off (1793329949703_9114b)
// Cline puts the skill's text in the prompt and nothing names it.
func TestClineToolsAndSkills(t *testing.T) {
	inZone(t, 0)
	setupAgents(t)
	copyTree(t, "testdata/cline-skills/sessions", ClineSessionDir())
	Reset()

	by, s := agentSessions(t, "cline")
	called := by["1793329741817_a7bqa"]
	if called.ToolCalls != 2 || called.tools.of("run_commands") != 1 || called.tools.of("skills") != 1 {
		t.Fatalf("tools %d %v", called.ToolCalls, called.tools)
	}
	if called.skills.of("greet-user") != 1 || len(called.skills) != 1 {
		t.Fatalf("the skills tool's skill: %v", called.skills)
	}
	if x := by["1793329916934_dkxa7"]; x.skills.of("greet-user") != 1 || len(x.skills) != 1 || x.Title != "/greet-user" {
		t.Fatalf("/greet-user: %v %q", x.skills, x.Title)
	}
	for _, id := range []string{"1793329949703_9114b", "1793329947187_qcjyw"} {
		if x, ok := by[id]; !ok || x.ToolCalls != 0 || len(x.skills) != 0 {
			t.Fatalf("%s: %v %+v", id, ok, x)
		}
	}
	o := s.Overview("", "", "", 10)
	if o.Skills.Top[0].Name != "greet-user" || o.Skills.Top[0].Agents["cline"] != 2 {
		t.Fatalf("overview skills %+v", o.Skills)
	}
}

// dsh 0.2.1-rc.2: a tool/call {name, arguments}, the skill tool's
// arguments the JSON string {"name":"greet-user"}; /greet-user typed is a
// user/message whose source is {kind: skill-invocation, name: greet-user},
// after the prompt as typed.
func TestDshToolsAndSkills(t *testing.T) {
	inZone(t, 0)
	setupAgents(t)
	copyTree(t, "testdata/dsh-skills/sessions", filepath.Join(DshDir(), "sessions"))
	Reset()

	by, s := agentSessions(t, "dsh")
	called := by["session-548398a9-6407-4b87-9579-de4558c276df"]
	if called.ToolCalls != 2 || called.tools.of("bash") != 1 || called.tools.of("skill") != 1 {
		t.Fatalf("tools %d %v", called.ToolCalls, called.tools)
	}
	if called.skills.of("greet-user") != 1 || len(called.skills) != 1 {
		t.Fatalf("the skill tool's skill: %v", called.skills)
	}
	typed := by["session-c4f0a364-ae68-4ebb-9ceb-1e921c68aa45"]
	if typed.ToolCalls != 0 || typed.skills.of("greet-user") != 1 || len(typed.skills) != 1 {
		t.Fatalf("/greet-user: %d %v", typed.ToolCalls, typed.skills)
	}
	if x, ok := by["session-89de7f4b-ca58-4020-a137-c46fd50028ad"]; !ok || x.ToolCalls != 0 || len(x.skills) != 0 {
		t.Fatalf("/nonexistentzz: %v %+v", ok, x)
	}
	o := s.Overview("", "", "", 10)
	if o.Skills.Top[0].Name != "greet-user" || o.Skills.Top[0].Agents["dsh"] != 2 {
		t.Fatalf("overview skills %+v", o.Skills)
	}
}

func TestDshSkill(t *testing.T) {
	for _, c := range []struct{ tool, args, want string }{
		{"skill", `{"name":"greet-user"}`, "greet-user"},
		{"bash", `{"name":"greet-user"}`, ""},
		{"skill", `{"name":"greet`, ""},
		{"skill", ``, ""},
	} {
		if got := dshSkill(c.tool, c.args); got != c.want {
			t.Errorf("%s %q: %q, want %q", c.tool, c.args, got, c.want)
		}
	}
}
