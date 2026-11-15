package agent

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// cjyrainbow (#2046): a WSL Claude Code's settings.json held a modelPicker
// uni-switch had written, but stash.json had no claude@wsl:Ubuntu.model_picker
// mark for it, so writePicker took it for the user's and never touched it
// again: a model added in uni-switch since was in the gateway's /v1/models and
// not in /model. A picker whose every row is uni-switch's ("… · via uni-switch") is
// kept up to date though its mark is gone; one the user wrote stays as it is.
func TestClaudePickerUniSwitchWroteIsKeptUpToDateWithoutItsMark(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	root := t.TempDir()
	d := distro{Name: "Ubuntu", Home: "/home/me", Root: root, Running: true, Mirrored: true}

	for _, c := range []struct {
		name string
		at   place
		mark string
	}{
		{"native", here(home), "claude.model_picker"},
		{"wsl", d.place("claude@wsl:Ubuntu"), "claude@wsl:Ubuntu.model_picker"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := provider.Save(provider.Provider{ID: "cpa", Name: "CPA", Chat: "http://127.0.0.1:1/v1", Key: "k", Models: []string{"gpt-6-luna", "claude-sonnet-4-6"}}); err != nil {
				t.Fatal(err)
			}
			a := claudeIn(c.at)
			path := filepath.Join(c.at.claudeDir(), "settings.json")
			writeFile(t, path, `{"theme":"dark"}`)
			if err := a.Apply("model", "cpa/gpt-6-luna"); err != nil {
				t.Fatal(err)
			}
			listed := func() []string {
				t.Helper()
				var p struct{ Options []map[string]string }
				raw, _ := edit.GetJSON(path, "modelPicker")
				if err := json.Unmarshal([]byte(raw), &p); err != nil {
					t.Fatalf("modelPicker: %v\n%s", err, readFile(path))
				}
				var out []string
				for _, o := range p.Options {
					out = append(out, strings.TrimSuffix(o["model"], "[1m]"))
				}
				return out
			}
			if !slices.Contains(listed(), "cpa/gpt-6-luna") {
				t.Fatalf("picker after connecting: %v", listed())
			}

			// the reporter's state: the picker uni-switch wrote, its mark gone
			forget(c.mark)
			if err := provider.Save(provider.Provider{ID: "cpa", Name: "CPA", Chat: "http://127.0.0.1:1/v1", Key: "k", Models: []string{"gpt-6-luna", "claude-sonnet-4-6", "gpt-6.1-sol"}}); err != nil {
				t.Fatal(err)
			}
			if err := a.Sync(); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(listed(), "cpa/gpt-6.1-sol") {
				t.Fatalf("a model added in uni-switch isn't in /model: %v\n%s", listed(), readFile(path))
			}
			if stashLoad()[c.mark] == "" {
				t.Fatalf("the picker uni-switch keeps up to date has no %s mark", c.mark)
			}

			// a picker the user wrote stays theirs
			forget(c.mark)
			mine := map[string]any{"options": []any{map[string]any{"model": "cpa/gpt-6-luna", "label": "Luna", "description": "mine"}}}
			if err := edit.SetJSON(path, edit.KV{Path: "modelPicker", Value: mine}); err != nil {
				t.Fatal(err)
			}
			if err := a.Sync(); err != nil {
				t.Fatal(err)
			}
			if got := listed(); !slices.Equal(got, []string{"cpa/gpt-6-luna"}) {
				t.Fatalf("the user's own picker was written over: %v", got)
			}
		})
	}
}
