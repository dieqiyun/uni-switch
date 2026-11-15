package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A tier, or the subagents, given a model picked from the list keeps it
// when the main model moves, though the pick is the main model itself or
// the model the tier would follow on it, picked in uni-switch with the main
// model moved in uni-switch or in Claude Code's /model (#2654, ypli0629: a
// sonnet tier given K2.8 Preview, the main model, read as "Same as main
// model", and the move to GLM-6 took it along). "Same as main model"
// picked afterwards has it follow again.
func TestClaudeTierPickedAsTheMainModelStays(t *testing.T) {
	type pick struct{ main, key, model string }
	var picks []pick
	for _, main := range []string{"oa/k2.8-preview", "anth/claude-opus-5-5"} {
		for _, k := range append(append([]string{}, claudeTiers...), "subagent") {
			picks = append(picks, pick{main, k, main})
		}
	}
	// haiku on a Claude main model follows its Haiku: that Haiku picked
	picks = append(picks, pick{"anth/claude-opus-5-5", "haiku", "anth/claude-haiku-4-5"})
	for _, p := range picks {
		for _, via := range []string{"uni-switch", "/model"} {
			t.Run(p.main+"/"+p.key+"="+p.model+"/"+via, func(t *testing.T) {
				home, _ := codexHome(t, "", "")
				a := claude(home)
				if err := provider.Save(provider.Provider{ID: "anth", Name: "Anthropic", Chat: "https://api.anthropic.com/v1", Key: "k",
					Models: []string{"claude-opus-5-5", "claude-haiku-4-5"}}); err != nil {
					t.Fatal(err)
				}
				if err := provider.Save(provider.Provider{ID: "oa", Name: "OA", Chat: "https://oa.example/v1", Key: "k",
					Models: []string{"k2.8-preview", "glm-6"}}); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(home, ".claude", "settings.json")
				bare := func(s string) string { return strings.TrimSuffix(s, "[1m]") }
				envKey := "CLAUDE_CODE_SUBAGENT_MODEL"
				if p.key != "subagent" {
					envKey = tierEnv(p.key)
				}
				written := func() string { m, _ := edit.GetJSON(path, "env."+envKey); return bare(m) }
				main := func(m string) {
					t.Helper()
					if via == "uni-switch" {
						if err := a.Apply("model", m); err != nil {
							t.Fatal(err)
						}
					} else {
						if err := edit.SetJSON(path, edit.KV{Path: "model", Value: m}); err != nil {
							t.Fatal(err)
						}
						if err := a.Follow(); err != nil {
							t.Fatal(err)
						}
					}
				}
				if err := a.Apply("model", p.main); err != nil {
					t.Fatal(err)
				}
				if err := a.Apply(p.key, p.model); err != nil {
					t.Fatal(err)
				}
				if v := a.Values(); bare(v[p.key]) != p.model {
					t.Fatalf("%s picked as %s reads %q, as if it followed the main model\n%s", p.key, p.model, v[p.key], readFile(path))
				}
				if written() != p.model {
					t.Fatalf("%s picked as %s is written %q\n%s", p.key, p.model, written(), readFile(path))
				}
				main("oa/glm-6")
				if written() != p.model {
					t.Fatalf("main model moved: %s is %q, not its own %s\n%s", p.key, written(), p.model, readFile(path))
				}
				if v := a.Values(); bare(v[p.key]) != p.model {
					t.Fatalf("main model moved: %s reads %q, not its own %s", p.key, v[p.key], p.model)
				}

				// "Same as main model": it follows again
				if err := a.Apply(p.key, ""); err != nil {
					t.Fatal(err)
				}
				main("oa/k2.8-preview")
				if v := a.Values(); v[p.key] != "" {
					t.Fatalf("same as main model: %s reads %q\n%s", p.key, v[p.key], readFile(path))
				}
				if w := written(); w == p.model && p.model != "oa/k2.8-preview" || w == "oa/glm-6" {
					t.Fatalf("same as main model: %s is still %q\n%s", p.key, w, readFile(path))
				}
			})
		}
	}
}

// Claude Code's subagents left unset are Claude Code's pick (KorenKrita on
// Discord): uni-switch writes no CLAUDE_CODE_SUBAGENT_MODEL, through a pick
// taken back and a main model moved, so Claude Code takes the Agent call's
// model, then the subagent's own, then the session's (2.1.306).
func TestClaudeSubagentsUnsetWriteNothing(t *testing.T) {
	home, _ := codexHome(t, "", "")
	a := claude(home)
	if err := provider.Save(provider.Provider{ID: "oa", Name: "OA", Chat: "https://oa.example/v1", Key: "k",
		Models: []string{"k2.8-preview", "glm-6"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".claude", "settings.json")
	none := func(when string) {
		t.Helper()
		if v, has := edit.GetJSON(path, "env.CLAUDE_CODE_SUBAGENT_MODEL"); has {
			t.Fatalf("%s: CLAUDE_CODE_SUBAGENT_MODEL is %q, not left to Claude Code\n%s", when, v, readFile(path))
		}
		if v := a.Values()["subagent"]; v != "" {
			t.Fatalf("%s: subagents read %q", when, v)
		}
	}
	if err := a.Apply("model", "oa/k2.8-preview"); err != nil {
		t.Fatal(err)
	}
	none("routed")
	if err := a.Apply("subagent", "oa/glm-6"); err != nil {
		t.Fatal(err)
	}
	if v, _ := edit.GetJSON(path, "env.CLAUDE_CODE_SUBAGENT_MODEL"); v == "" {
		t.Fatalf("a model picked isn't written\n%s", readFile(path))
	}
	if err := a.Apply("subagent", ""); err != nil {
		t.Fatal(err)
	}
	none("taken back")
	if err := a.Apply("model", "oa/glm-6"); err != nil {
		t.Fatal(err)
	}
	none("main model moved")
}
