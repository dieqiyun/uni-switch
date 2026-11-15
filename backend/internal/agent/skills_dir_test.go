package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// Skill installers (npx skills and the like) make <folder>/skills for
// every agent they know. A folder holding only that is no sign the agent is
// here; anything the agent writes beside it is (qtwaiter on X: Goose, Crush,
// Command Code, fx, Devin, Hermes and Droid listed, none installed).
func TestSkillsOnlyFolderIsNotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, mk := range []func(home, cfg string) *Agent{
		goose, crush, devin,
		func(h, _ string) *Agent { return commandCode(h) },
		func(h, _ string) *Agent { return fx(h) },
		func(h, _ string) *Agent { return hermes(h) },
		func(h, _ string) *Agent { return droid(h) },
	} {
		home := t.TempDir()
		ownAppData(t, home)
		cfg := filepath.Join(home, ".config")
		a := mk(home, cfg)
		t.Run(a.ID, func(t *testing.T) {
			skill := filepath.Join(a.Dir, "skills", "grill", "SKILL.md")
			if err := os.MkdirAll(filepath.Dir(skill), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(skill, []byte("---\nname: grill\n---\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			os.WriteFile(filepath.Join(a.Dir, ".DS_Store"), nil, 0o644)
			if mk(home, cfg).Detected() {
				t.Errorf("a %s holding only skills is taken for %s", a.Dir, a.Name)
			}
			if err := os.WriteFile(filepath.Join(a.Dir, "history.jsonl"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			if !mk(home, cfg).Detected() {
				t.Errorf("%s with its own files beside skills isn't found", a.Name)
			}
		})
	}
}

// An uninstalled agent leaves the folders it writes as it runs: v5tech's
// ~/.codebuddy held only logs/ and diagnostics/, ~/.qwen a debug/ (#1495).
// A folder holding only those is no sign the agent is here; its own
// settings beside them are, and so is a file that only shares the name.
func TestLeftoverFoldersAreNotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, c := range []struct {
		mk   func(home string) *Agent
		left []string
	}{
		{func(h string) *Agent { return codebuddy(h) }, []string{"logs", "diagnostics"}},
		{func(h string) *Agent { return qwen(h) }, []string{"debug"}},
		{func(h string) *Agent { return hermes(h) }, []string{"cache", "log", "skills"}},
	} {
		home := t.TempDir()
		ownAppData(t, home)
		a := c.mk(home)
		t.Run(a.ID, func(t *testing.T) {
			for _, d := range c.left {
				if err := os.MkdirAll(filepath.Join(a.Dir, d), 0o755); err != nil {
					t.Fatal(err)
				}
				os.WriteFile(filepath.Join(a.Dir, d, "x.log"), []byte("x"), 0o644)
			}
			if c.mk(home).Detected() {
				t.Errorf("%s holding only %v is taken for %s", a.Dir, c.left, a.Name)
			}
			if err := os.WriteFile(filepath.Join(a.Dir, "settings.json"), []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}
			if !c.mk(home).Detected() {
				t.Errorf("%s with its own settings beside %v isn't found", a.Name, c.left)
			}
		})
	}
	home := t.TempDir()
	a := codebuddy(home)
	os.MkdirAll(a.Dir, 0o755)
	if err := os.WriteFile(filepath.Join(a.Dir, "logs"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !codebuddy(home).Detected() {
		t.Error("a file named logs, not a folder, is no longer taken for the agent's own")
	}
}

// ownAppData puts APPDATA and LOCALAPPDATA in home, where agents keep
// their folders on Windows (Hermes in %LOCALAPPDATA%\hermes): left at the
// package's home, another test's files there are taken for the agent.
func ownAppData(t *testing.T, home string) {
	t.Helper()
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
}
