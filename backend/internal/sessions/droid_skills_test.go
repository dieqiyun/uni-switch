package sessions

import (
	"path/filepath"
	"testing"
	"time"
)

// Factory Droid's skills (MaxMooreMy on X: "droid 也能帮忙修复下吗",
// Sessions' Skills card said no skill was called). The four sessions in
// testdata/droid-skills are real droid 0.231.0 sessions, run headless
// (droid exec, on a custom model through uni-switch's gateway) in a sandbox
// home, byte for byte; only the folder of their working directory is
// named shorter. In one the model reads notes.txt and calls the Skill
// tool for greet-user; in one the user types /greet-user, and in one
// /greet-user /wave-hand, which droid sends with a <system-notification>
// per skill; one types /nonexistentzz, which no skill has and droid sends
// as typed.
func TestDroidToolsAndSkills(t *testing.T) {
	inZone(t, 0)
	home := setupAgents(t)
	t.Setenv("FACTORY_HOME_OVERRIDE", "")
	copyTree(t, "testdata/droid-skills", filepath.Join(home, ".factory"))
	Reset()

	s := statsAt(0, time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC))
	by := map[string]Summary{}
	for _, x := range s.Sessions {
		if x.Agent == "droid" {
			by[x.ID] = x
		}
	}
	if len(by) != 4 {
		t.Fatalf("want the 4 Droid sessions, got %d", len(by))
	}
	called := by["86f11060-c188-4609-942b-8c3fc53638dc"]
	if called.ToolCalls != 2 || called.tools.of("Read") != 1 || called.tools.of("Skill") != 1 {
		t.Fatalf("tools %d %v", called.ToolCalls, called.tools)
	}
	if called.skills.of("greet-user") != 1 || len(called.skills) != 1 {
		t.Fatalf("the Skill tool's skill: %v", called.skills)
	}
	typed := by["cb1d8bed-aa2c-4ba2-a23f-681dc5da403f"]
	if typed.ToolCalls != 0 || typed.skills.of("greet-user") != 1 || len(typed.skills) != 1 {
		t.Fatalf("/greet-user: %d %v", typed.ToolCalls, typed.skills)
	}
	if typed.Title != "/greet-user" {
		t.Fatalf("/greet-user's title: %q", typed.Title)
	}
	two := by["aea4c8cc-92da-4695-889d-5ad6c5b6cec3"]
	if two.skills.of("greet-user") != 1 || two.skills.of("wave-hand") != 1 || len(two.skills) != 2 {
		t.Fatalf("/greet-user /wave-hand: %v", two.skills)
	}
	if x := by["99bfae77-1ca3-4cc9-9c35-445f1fe4dfcd"]; x.ToolCalls != 0 || len(x.skills) != 0 {
		t.Fatalf("/nonexistentzz: %d %v", x.ToolCalls, x.skills)
	}
	o := s.Overview("", "", "", 10)
	if o.Skills.Calls != 4 || o.Skills.Top[0].Name != "greet-user" || o.Skills.Top[0].Agents["droid"] != 3 {
		t.Fatalf("overview skills %+v", o.Skills)
	}
}

func TestDroidSlashSkills(t *testing.T) {
	for _, c := range []struct {
		text string
		want []string
	}{
		{"/greet-user", nil},
		{"selected the following skill for immediate execution", nil},
		{"/a&amp;b\n\n<system-notification>\nThe user has selected the following skill for immediate execution by typing /a&amp;b in their message.\n<skill filePath=\"/s/a&amp;b/SKILL.md\">\n<name>a&amp;b</name>\n</skill>\n</system-notification>", []string{"a&b"}},
		// the builtin's wording, with no "by typing"
		{"<system-notification>\nThe user has selected the following skill for immediate execution. Begin.\n<skill filePath=\"/b/SKILL.md\">\n<name>review</name>\n<description>x (builtin)</description>\n</skill>", []string{"review"}},
		// a name cut short
		{"selected the following skill for immediate execution\n<skill filePath=\"/x\">\n<name>half", nil},
	} {
		got := droidSlashSkills(c.text)
		if len(got) != len(c.want) || len(got) > 0 && got[0] != c.want[0] {
			t.Errorf("%q: %v, want %v", c.text, got, c.want)
		}
	}
}
