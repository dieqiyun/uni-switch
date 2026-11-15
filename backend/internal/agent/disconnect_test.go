package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Disconnect leaves Codex as it was before uni-switch was wired in (Fate on
// Discord asked for one action that takes out what uni-switch wrote): the
// provider, model and effort the user had come back, and uni-switch's base URL,
// catalog, subagent model and the effort it set go. Picking the agent's
// default instead leaves Codex as installed, the user's own provider gone.
func TestCodexDisconnect(t *testing.T) {
	const own = "model_provider = \"mine\"\nmodel = \"gpt-5.4\"\nmodel_reasoning_effort = \"high\"\n\n" +
		"[model_providers.mine]\nname = \"mine\"\nbase_url = \"https://mine.example/v1\"\n"
	for _, tc := range []struct{ name, config, auth string }{
		{"own provider", own, ""},
		{"own provider, signed in", own, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`},
		{"nothing set", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, read := codexHome(t, tc.auth, tc.config)
			path := filepath.Join(home, ".codex", "config.toml")
			top := func() map[string]string {
				m := map[string]string{}
				for _, k := range []string{"model", "model_provider", "model_reasoning_effort", "openai_base_url", "model_catalog_json"} {
					if v, _ := edit.GetTOMLTop(path, k); v != "" {
						m[k] = v
					}
				}
				return m
			}
			was := top()
			cx := codex(home)
			if cx.Wired() {
				t.Fatal("wired before uni-switch set anything")
			}
			if err := cx.Apply("model", "fake/m1"); err != nil {
				t.Fatal(err)
			}
			if err := cx.Apply("effort", "low"); err != nil {
				t.Fatal(err)
			}
			if err := cx.Apply("subagent", "fake/m1"); err != nil {
				t.Fatal(err)
			}
			if !cx.Wired() {
				t.Fatalf("not wired on a uni-switch model:\n%s", read())
			}
			if err := cx.Disconnect(); err != nil {
				t.Fatal(err)
			}
			if got := top(); !reflect.DeepEqual(got, was) {
				t.Fatalf("after disconnect %v, want %v:\n%s", got, was, read())
			}
			if agents, _ := edit.GetTOMLTable(path, "agents"); agents["default_subagent_model"] != "" {
				t.Fatalf("subagent model left:\n%s", read())
			}
			if mine, _ := edit.GetTOMLTable(path, "model_providers.mine"); tc.config != "" && mine["base_url"] != "https://mine.example/v1" {
				t.Fatalf("own provider table changed:\n%s", read())
			}
			if _, err := os.Stat(filepath.Join(home, ".codex", "uni-switch-models.json")); err == nil {
				t.Error("uni-switch's catalog left")
			}
			if cx.Wired() || cx.Drift() != nil {
				t.Fatalf("still wired (drift %+v):\n%s", cx.Drift(), read())
			}
			if _, ok := appliedLoad()["codex"]; ok {
				t.Error("uni-switch still remembers setting Codex")
			}
			for _, k := range []string{"codex.model", "codex.effort", "codex.provider", "codex.catalog"} {
				if v, ok := stashLoad()[here(home).key(k)]; ok {
					t.Errorf("stash keeps %s = %q", k, v)
				}
			}
		})
	}
}

// Disconnect leaves Claude Code's settings.json as it was before uni-switch
// was wired in: the relay's endpoint and token and the model the user had
// come back, and every env key, tier and capability uni-switch wrote goes.
// The tier and subagent models the user set for their relay come back with
// it (#1050), whether uni-switch steps out by Disconnect or by Claude Code's
// own model. So does the main model set for the relay in the env
// (ANTHROPIC_MODEL), which uni-switch takes out while wired in, and the auto
// mode classifier's model (CLAUDE_CODE_AUTO_MODE_MODEL).
func TestClaudeDisconnect(t *testing.T) {
	for name, before := range map[string]string{
		"own relay":   `{"theme":"dark","model":"opus","env":{"ANTHROPIC_BASE_URL":"https://relay.example","ANTHROPIC_AUTH_TOKEN":"sk-relay","DISABLE_TELEMETRY":"1"}}`,
		"own tiers":   `{"model":"sonnet","env":{"ANTHROPIC_BASE_URL":"https://relay.example","ANTHROPIC_AUTH_TOKEN":"sk-relay","ANTHROPIC_DEFAULT_OPUS_MODEL":"relay-opus","ANTHROPIC_DEFAULT_SONNET_MODEL":"relay-sonnet","ANTHROPIC_DEFAULT_HAIKU_MODEL":"relay-haiku","ANTHROPIC_SMALL_FAST_MODEL":"relay-haiku","CLAUDE_CODE_SUBAGENT_MODEL":"relay-sonnet"}}`,
		"nothing set": `{"theme":"dark"}`,
		// the auto mode classifier's model the user set themselves, which
		// uni-switch takes out while wired in and puts back
		"own auto mode": `{"env":{"ANTHROPIC_BASE_URL":"https://relay.example","ANTHROPIC_AUTH_TOKEN":"sk-relay","CLAUDE_CODE_AUTO_MODE_MODEL":"relay-haiku"}}`,
		// the main model set for the relay in the env (with no settings
		// model, and beside one), which uni-switch takes out while wired in:
		// it outranks a pick in /model
		"own env model":       `{"env":{"ANTHROPIC_BASE_URL":"https://relay.example","ANTHROPIC_AUTH_TOKEN":"sk-relay","ANTHROPIC_MODEL":"relay-main"}}`,
		"own env model, both": `{"model":"opus","env":{"ANTHROPIC_BASE_URL":"https://relay.example","ANTHROPIC_AUTH_TOKEN":"sk-relay","ANTHROPIC_MODEL":"relay-main[1m]"}}`,
	} {
		for _, out := range []string{"disconnect", "default"} {
			t.Run(name+", "+out, func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("USERPROFILE", home)
				t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
				t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
				if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro", "flash"}}); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(home, ".claude", "settings.json")
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
					t.Fatal(err)
				}
				parse := func() map[string]any {
					b, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					var m map[string]any
					if err := json.Unmarshal(b, &m); err != nil {
						t.Fatal(err)
					}
					// an object uni-switch emptied out is the same as none
					for k, v := range m {
						if o, ok := v.(map[string]any); ok && len(o) == 0 {
							delete(m, k)
						}
					}
					return m
				}
				want := parse()
				a := claude(home)
				if a.Wired() {
					t.Fatal("wired before uni-switch set anything")
				}
				if err := a.Apply("model", "deepseek/pro"); err != nil {
					t.Fatal(err)
				}
				if err := a.Apply("haiku", "deepseek/flash"); err != nil {
					t.Fatal(err)
				}
				if err := a.Apply("subagent", "deepseek/flash"); err != nil {
					t.Fatal(err)
				}
				if err := a.Apply("auto_mode", "deepseek/flash"); err != nil {
					t.Fatal(err)
				}
				if !a.Wired() {
					t.Fatal("not wired on a uni-switch model")
				}
				if out == "default" {
					if err := a.Apply("model", ""); err != nil {
						t.Fatal(err)
					}
					// Claude Code's own model, set by leaving none
					delete(want, "model")
				} else if err := a.Disconnect(); err != nil {
					t.Fatal(err)
				}
				if got := parse(); !reflect.DeepEqual(got, want) {
					b, _ := os.ReadFile(path)
					t.Fatalf("after disconnect:\n%s\nwant %v", b, want)
				}
				if a.Wired() {
					t.Fatal("still wired")
				}
				if _, ok := appliedLoad()["claude"]; ok && out == "disconnect" {
					t.Error("uni-switch still remembers setting Claude Code")
				}
				for k := range stashLoad() {
					if strings.HasPrefix(k, "claude.env.") {
						t.Errorf("stash keeps %s", k)
					}
				}
			})
		}
	}
}

// Gemini CLI's model default forgets the sign-in it had before uni-switch:
// Disconnect brings back the sign-in, the model and the user's own key.
func TestGeminiDisconnect(t *testing.T) {
	home, _ := codexHome(t, "", "")
	dir := filepath.Join(home, ".gemini")
	os.MkdirAll(dir, 0o755)
	settings := `{"security":{"auth":{"selectedType":"gemini-api-key"}},"model":{"name":"gemini-2.5-pro"}}`
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settings), 0o644)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("GEMINI_API_KEY=mine\n"), 0o600)
	g := gemini(home)
	was := g.Values()
	if err := g.Apply("model", "fake/m1"); err != nil {
		t.Fatal(err)
	}
	if !g.Wired() {
		t.Fatal("not wired on a uni-switch model")
	}
	if err := g.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if got := g.Values(); !reflect.DeepEqual(got, was) || g.Wired() {
		t.Fatalf("after disconnect %v, want %v", got, was)
	}
	if k, _ := edit.GetEnvFile(filepath.Join(dir, ".env"), "GEMINI_API_KEY"); k != "mine" {
		t.Fatalf("own key %q", k)
	}
	if u, _ := edit.GetEnvFile(filepath.Join(dir, ".env"), "GOOGLE_GEMINI_BASE_URL"); u != "" {
		t.Fatalf("base URL left: %q", u)
	}
}

// An agent uni-switch is not in has nothing to disconnect: its config stays
// byte for byte.
func TestDisconnectUnwiredLeavesConfig(t *testing.T) {
	home, read := codexHome(t, "", "model = \"gpt-5.4\"\nmodel_reasoning_effort = \"high\"\n")
	was := read()
	if err := codex(home).Disconnect(); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != was {
		t.Fatalf("changed:\n%s\nwant\n%s", got, was)
	}
}

// An auth Gemini CLI doesn't know, set on its provider field, is refused
// before uni-switch is taken out. `uni-switch gemini <a model it has no option
// for>` sets that field (it is the first), and took Gemini CLI off uni-switch,
// its sign-in and key put back, before saying the auth was unknown: the
// sibling of `uni-switch snow --help` (#1457).
func TestGeminiUnknownAuthChangesNothing(t *testing.T) {
	home, _ := codexHome(t, "", "")
	dir := filepath.Join(home, ".gemini")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"security":{"auth":{"selectedType":"gemini-api-key"}},"model":{"name":"gemini-2.5-pro"}}`), 0o644)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("GEMINI_API_KEY=mine\n"), 0o600)
	g := gemini(home)
	if err := g.Apply("model", "fake/m1"); err != nil {
		t.Fatal(err)
	}
	read := func() string {
		s, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
		e, _ := os.ReadFile(filepath.Join(dir, ".env"))
		return string(s) + "\n" + string(e)
	}
	was, vals := read(), g.Values()
	for _, v := range []string{"nonsense-model", "--help"} {
		if err := g.Field("provider").Set(v); err == nil {
			t.Errorf("%q taken", v)
		}
		if got := read(); got != was || !g.Wired() || !reflect.DeepEqual(g.Values(), vals) {
			t.Fatalf("%q changed Gemini CLI:\n%s\nwas\n%s", v, got, was)
		}
	}
}
