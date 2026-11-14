package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// claudeOpusThenSonnet is #1050's Claude Code before the switch: on
// uni-switch's Opus 5.5, its sonnet tier and its subagents given Sonnet 5.5,
// both 1M models, as uni-switch's record (applied.json) had them. It answers
// the settings.json.
func claudeOpusThenSonnet(t *testing.T, a *Agent, home string) string {
	t.Helper()
	if err := provider.Save(provider.Provider{ID: "anth", Name: "Anthropic", Chat: "https://api.anthropic.com/v1", Key: "k",
		Models: []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-4-5"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("anth", "https://api.anthropic.com/v1", []catalog.Model{
		{ID: "claude-opus-5-5", Context: 1000000}, {ID: "claude-sonnet-5-5", Context: 1000000}, {ID: "claude-haiku-4-5", Context: 200000},
	}); err != nil {
		t.Fatal(err)
	}
	for _, kv := range [][2]string{{"model", "anth/claude-opus-5-5"}, {"sonnet", "anth/claude-sonnet-5-5"}, {"subagent", "anth/claude-sonnet-5-5"}} {
		if err := a.Apply(kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	if rec := appliedOf(a.ID).Fields; rec["model"] != "anth/claude-opus-5-5[1m]" || rec["sonnet"] != "anth/claude-sonnet-5-5[1m]" || rec["subagent"] != "anth/claude-sonnet-5-5[1m]" {
		t.Fatalf("not #1050's record: %v", rec)
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// sonnetFollowsAsBefore moves the main model onto Sonnet 5.5 in Claude
// Code's /model, and leaves what uni-switch's Follow writes as a uni-switch before
// #1050's fix left it: the tier and the subagents on the new main model
// following it, not marked as the user's own.
func sonnetFollowsAsBefore(t *testing.T, a *Agent, path string) {
	t.Helper()
	if err := edit.SetJSON(path, edit.KV{Path: "model", Value: "anth/claude-sonnet-5-5[1m]"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Follow(); err != nil {
		t.Fatal(err)
	}
	at := place{}
	if _, distro, ok := strings.Cut(a.ID, "@wsl:"); ok {
		at.id = "claude@wsl:" + distro
	}
	forget(at.key("claude.tier_own.sonnet"), at.key("claude.tier_own.subagent"))
}

// The main model moved onto the Sonnet 5.5 uni-switch had given the sonnet tier
// and the subagents, in Claude Code's own /model (whose list uni-switch writes,
// each "· via uni-switch"): the two now follow the main model, read as empty,
// and run on the model uni-switch set them to. That isn't "changed outside
// uni-switch" (#1050, cjyrainbow: Claude Code · WSL Ubuntu 已在 uni-switch 之外换掉
// claude/claude-sonnet-5-5[1m] while everything still went through uni-switch).
// The main model taken off uni-switch still is.
func TestClaudeFollowerOnTheMainModelIsNoDrift(t *testing.T) {
	home, _ := codexHome(t, "", "")
	a := claude(home)
	path := claudeOpusThenSonnet(t, a, home)
	sonnetFollowsAsBefore(t, a, path)
	env := func(k string) string { v, _ := edit.GetJSON(path, "env."+k); return v }
	if v := a.Values(); v["model"] != "anth/claude-sonnet-5-5[1m]" || v["sonnet"] != "" || v["subagent"] != "" ||
		env("ANTHROPIC_DEFAULT_SONNET_MODEL") != "anth/claude-sonnet-5-5[1m]" || env("CLAUDE_CODE_SUBAGENT_MODEL") != "anth/claude-sonnet-5-5[1m]" {
		t.Fatalf("not #1050's state: %v\n%s", v, readFile(path))
	}
	if d := a.Drift(); d != nil {
		t.Fatalf("drift on a tier following uni-switch's model: %+v", *d)
	}

	// the main model on Anthropic's own: changed outside uni-switch
	if err := edit.SetJSON(path, edit.KV{Path: "env.ANTHROPIC_BASE_URL", Value: "https://api.anthropic.com"}, edit.KV{Path: "model", Value: "claude-sonnet-5-5"}); err != nil {
		t.Fatal(err)
	}
	if d := a.Drift(); d == nil || d.Kind != "replaced" && d.Kind != "unwired" {
		t.Fatalf("the main model off uni-switch isn't said: %+v", d)
	}
}

// The same in a WSL distro's Claude Code, the reporter's, running and
// stopped: a stopped one reads what was last seen there.
func TestWSLClaudeFollowerOnTheMainModelIsNoDrift(t *testing.T) {
	root, home := claudeDistroHome(t, `{"model": "opus"}`)
	d := distro{Name: "Ubuntu", Root: root, Home: "/home/me", Has: map[string]bool{"dir:.claude": true},
		Gateway: "172.20.0.1", Mirrored: true, Running: true}
	// what uni-switch sees there is kept for when the distro stops
	seen := d
	wsl.Lock()
	if wsl.seen == nil {
		wsl.seen = map[string]*distro{}
	}
	wsl.seen["Ubuntu"] = &seen
	wsl.Unlock()
	t.Cleanup(func() { wsl.Lock(); delete(wsl.seen, "Ubuntu"); wsl.Unlock() })
	a := wslAgent(wslKindOf("claude"), d)
	path := claudeOpusThenSonnet(t, a, home)
	sonnetFollowsAsBefore(t, a, path)
	if v := a.Values(); v["model"] != "anth/claude-sonnet-5-5[1m]" || v["sonnet"] != "" || v["subagent"] != "" {
		t.Fatalf("not #1050's state: %v\n%s", v, readFile(path))
	}
	if dr := a.Drift(); dr != nil {
		t.Fatalf("drift on a tier following uni-switch's model: %+v", *dr)
	}
	d.Running = false
	stopped := wslAgent(wslKindOf("claude"), d)
	if v := stopped.Values(); v["model"] != "anth/claude-sonnet-5-5[1m]" || v["sonnet"] != "" {
		t.Fatalf("stopped: not what was last seen: %v", v)
	}
	if dr := stopped.Drift(); dr != nil {
		t.Fatalf("stopped: drift on a tier following uni-switch's model: %+v", *dr)
	}
	noOwnClaude(t)
}

// A model given to a tier or the subagents stays theirs when the main model
// moves onto it and on again, picked in uni-switch or in Claude Code's /model:
// #1050's sonnet tier and subagents, given Sonnet 5.5, went to Opus 5.5
// with the main model's way back from Sonnet 5.5, the pick lost without a
// word. "Same as model" picked for them afterwards has them follow again.
func TestClaudeOwnTierStaysThroughTheMainModel(t *testing.T) {
	for _, via := range []string{"uni-switch", "/model"} {
		t.Run(via, func(t *testing.T) {
			home, _ := codexHome(t, "", "")
			a := claude(home)
			path := claudeOpusThenSonnet(t, a, home)
			env := func(k string) string { v, _ := edit.GetJSON(path, "env."+k); return v }
			main := func(m string) {
				t.Helper()
				if via == "uni-switch" {
					if err := a.Apply("model", m); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := edit.SetJSON(path, edit.KV{Path: "model", Value: m + "[1m]"}); err != nil {
						t.Fatal(err)
					}
					if err := a.Follow(); err != nil {
						t.Fatal(err)
					}
				}
				if d := a.Drift(); d != nil {
					t.Fatalf("main %s: drift %+v", m, *d)
				}
			}
			main("anth/claude-sonnet-5-5")
			if v := a.Values(); v["sonnet"] != "anth/claude-sonnet-5-5[1m]" || v["subagent"] != "anth/claude-sonnet-5-5[1m]" || v["opus"] != "" {
				t.Fatalf("on the main model, the tier and subagents read as their own: %v", v)
			}
			main("anth/claude-opus-5-5")
			if env("ANTHROPIC_DEFAULT_SONNET_MODEL") != "anth/claude-sonnet-5-5[1m]" || env("CLAUDE_CODE_SUBAGENT_MODEL") != "anth/claude-sonnet-5-5[1m]" ||
				env("ANTHROPIC_DEFAULT_OPUS_MODEL") != "anth/claude-opus-5-5[1m]" {
				t.Fatalf("the sonnet tier and subagents lost their Sonnet:\n%s", readFile(path))
			}
			if v := a.Values(); v["sonnet"] != "anth/claude-sonnet-5-5[1m]" || v["subagent"] != "anth/claude-sonnet-5-5[1m]" {
				t.Fatalf("values %v", v)
			}

			// back on Sonnet, "same as model" for both: they follow again
			main("anth/claude-sonnet-5-5")
			for _, k := range []string{"sonnet", "subagent"} {
				if err := a.Apply(k, ""); err != nil {
					t.Fatal(err)
				}
			}
			main("anth/claude-opus-5-5")
			if env("ANTHROPIC_DEFAULT_SONNET_MODEL") != "anth/claude-opus-5-5[1m]" || env("CLAUDE_CODE_SUBAGENT_MODEL") != "" {
				t.Fatalf("same as model doesn't follow:\n%s", readFile(path))
			}
		})
	}
}

// Every tier given a model of its own keeps it when the main model moves
// onto that model and on again, picked in uni-switch or in Claude Code's
// /model, whatever the tier would follow there (#2257, luokinging): a
// fable tier given GPT-6.1 Sol, with the main model moved from K2.8
// Preview onto GPT-6.1 Sol, read as "Same as main model", since fable
// follows no model that is no Fable, and the main model's next move
// took it off. A haiku tier given Opus, the main model moved onto Opus,
// was lost the same way (haiku follows a Claude model on its Haiku).
func TestClaudeOwnTierOnTheNewMainModelStays(t *testing.T) {
	for _, own := range []string{"oa/gpt-6.1-sol", "anth/claude-opus-5-5"} {
		for _, tier := range claudeTiers {
			for _, via := range []string{"uni-switch", "/model"} {
				t.Run(own+"/"+tier+"/"+via, func(t *testing.T) {
					home, _ := codexHome(t, "", "")
					a := claude(home)
					if err := provider.Save(provider.Provider{ID: "anth", Name: "Anthropic", Chat: "https://api.anthropic.com/v1", Key: "k",
						Models: []string{"claude-opus-5-5", "claude-haiku-4-5"}}); err != nil {
						t.Fatal(err)
					}
					if err := provider.Save(provider.Provider{ID: "oa", Name: "OA", Chat: "https://oa.example/v1", Key: "k",
						Models: []string{"gpt-6.1-sol", "k2.8-preview", "glm-6"}}); err != nil {
						t.Fatal(err)
					}
					path := filepath.Join(home, ".claude", "settings.json")
					bare := func(s string) string { return strings.TrimSuffix(s, "[1m]") }
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
					if err := a.Apply("model", "oa/k2.8-preview"); err != nil {
						t.Fatal(err)
					}
					if err := a.Apply(tier, own); err != nil {
						t.Fatal(err)
					}
					if v := a.Values(); bare(v[tier]) != own {
						t.Fatalf("the tier given %s reads %q", own, v[tier])
					}
					main(own)
					if v := a.Values(); bare(v[tier]) != own {
						t.Fatalf("main model moved onto the tier's model: %s reads %q, not its own %s\n%s", tier, v[tier], own, readFile(path))
					}
					main("oa/glm-6")
					if m, _ := edit.GetJSON(path, "env."+tierEnv(tier)); bare(m) != own {
						t.Fatalf("main model moved on again: %s is %q, not its own %s\n%s", tier, m, own, readFile(path))
					}
					if v := a.Values(); bare(v[tier]) != own {
						t.Fatalf("main model moved on again: %s reads %q", tier, v[tier])
					}
				})
			}
		}
	}
}
