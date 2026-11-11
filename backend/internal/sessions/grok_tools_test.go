package sessions

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Grok Build's tool calls and skills (MaxMooreMy on X: "grok build 的工具
// 调用啥的也没采集到"). The four sessions in testdata/grok-tools are real
// grok 1.0.67 sessions, run headless (grok -p) in a sandbox home, byte for
// byte; only the folder of their working directory is named shorter. In
// one the model runs `cat notes.txt` and reads the greet-user skill's
// SKILL.md with read_file; in one the user types /greet-user; one types
// /nonexistentzz, a skill no folder has; one types /compact, Grok Build's
// own command, marked hostTurn, which calls up no skill even with one of
// that name about.
func TestGrokToolsAndSkills(t *testing.T) {
	inZone(t, 0)
	claude, _ := setup(t)
	home := filepath.Join(filepath.Dir(claude), "home")
	copyTree(t, "testdata/grok-tools", filepath.Join(home, ".grok"))
	compact := filepath.Join(home, ".grok", "skills", "compact")
	os.MkdirAll(compact, 0o755)
	os.WriteFile(filepath.Join(compact, "SKILL.md"), []byte("---\nname: compact\n---\n"), 0o644)

	s := statsAt(0, time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC))
	by := map[string]Summary{}
	for _, x := range s.Sessions {
		if x.Agent == "grok" {
			by[x.ID] = x
		}
	}
	if len(by) != 4 {
		t.Fatalf("want the 4 Grok Build sessions, got %d", len(by))
	}
	called := by["01a1826b-5457-7d52-9706-c7e26b3fe171"]
	if called.ToolCalls != 2 || called.tools.of("run_terminal_command") != 1 || called.tools.of("read_file") != 1 {
		t.Fatalf("tools %d %v", called.ToolCalls, called.tools)
	}
	if called.skills.of("greet-user") != 1 || len(called.skills) != 1 {
		t.Fatalf("the skill read: %v", called.skills)
	}
	typed := by["01a1826b-d49a-76b0-acf7-0a7a5a0a4dca"]
	if typed.ToolCalls != 0 || typed.skills.of("greet-user") != 1 || len(typed.skills) != 1 {
		t.Fatalf("/greet-user: %d %v", typed.ToolCalls, typed.skills)
	}
	for _, id := range []string{"01a1826c-64a3-7871-a4c1-5cfb6a077576", "01a1826c-7ff2-7f92-8ba4-feb3b83bad3a"} {
		if x := by[id]; x.ToolCalls != 0 || len(x.skills) != 0 {
			t.Fatalf("%s: %d %v", id, x.ToolCalls, x.skills)
		}
	}
	o := s.Overview("", "", "", 10)
	if o.Skills.Calls != 2 || o.Skills.Top[0].Name != "greet-user" || o.Skills.Top[0].Agents["grok"] != 2 {
		t.Fatalf("overview skills %+v", o.Skills)
	}
}

func TestGrokSlashSkill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GROK_HOME", "")
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "app")
	for _, p := range []string{
		filepath.Join(repo, ".claude", "skills", "frontend", "SKILL.md"),
		filepath.Join(cwd, ".grok", "commands", "ship.md"),
		filepath.Join(home, ".grok", "bundled", "skills", "review", "SKILL.md"),
		filepath.Join(home, ".agents", "skills", "pdf", "SKILL.md"),
	} {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	for text, want := range map[string]string{
		"/frontend":             "frontend",
		"/ship now please":      "ship",
		"/review\nthe diff":     "review",
		"/pdf":                  "pdf",
		"/tmp is full":          "",
		"/../repo":              "",
		"please /review":        "",
		"/frontend/x":           "",
		"/nonexistentzz say ok": "",
	} {
		if got := grokSlashSkill(cwd, text); got != want {
			t.Errorf("%q: %q, want %q", text, got, want)
		}
	}
}
