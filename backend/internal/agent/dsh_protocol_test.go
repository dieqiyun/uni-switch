package agent

import (
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"gopkg.in/yaml.v3"
)

// dshProtocolHome is dshRouteHome with a provider serving its models on
// OpenAI's Responses API alone and one on Anthropic's Messages API alone,
// beside its DeepSeek on Chat.
func dshProtocolHome(t *testing.T) (home, dir, web string) {
	t.Helper()
	home, dir, web = dshRouteHome(t)
	for _, p := range []provider.Provider{
		{ID: "resp", Name: "Resp", Key: "k", Responses: "http://127.0.0.1:1/v1", Models: []string{"gpt-5.5"}},
		{ID: "anth", Name: "Anth", Key: "k", Anthropic: "http://127.0.0.1:1", Models: []string{"claude-sonnet-5"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := catalog.SaveLive("resp", "http://127.0.0.1:1/v1", []catalog.Model{{ID: "gpt-5.5", Efforts: []string{"low", "medium", "high"}}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("anth", "http://127.0.0.1:1", []catalog.Model{{ID: "claude-sonnet-5", Efforts: []string{"low", "high", "max"}}}); err != nil {
		t.Fatal(err)
	}
	return home, dir, web
}

// dshPiProviders are the routes of a patch list's llm-pi-ai entry.
func dshPiProviders(t *testing.T, path string) map[string]dshPiRoute {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var list []struct {
		ID     string `yaml:"id"`
		Config struct {
			Providers map[string]dshPiRoute `yaml:"providers"`
		} `yaml:"config"`
	}
	if err := yaml.Unmarshal(b, &list); err != nil {
		t.Fatalf("%v:\n%s", err, b)
	}
	for _, it := range list {
		if it.ID == dshPiRow {
			return it.Config.Providers
		}
	}
	return nil
}

func dshRouteModelIDs(r dshPiRoute) []string {
	var out []string
	for _, m := range r.Models {
		out = append(out, m.ID)
	}
	return out
}

// Discord (01huadalang): 可以按不同协议分成组来注入，例如 chat 一个组，response
// 一个组. llm-pi-ai sets the api for a route as a whole, so uni-switch's one
// route asked every model on Chat Completions, and a GPT served on
// Responses or a Claude served on Messages was translated from Chat, its
// sealed reasoning lost between tool calls. Each model now goes on a route
// of uni-switch's for the API it is served on, and the start names that route.
// The user's own route stays.
func TestDshRoutesAModelOnItsNativeAPI(t *testing.T) {
	home, dir, web := dshProtocolHome(t)
	dshKeyFixture(dir)
	os.WriteFile(web, []byte("# Your patch layer for this dsh profile.\n"+dshMineRow), 0o644)
	a := dsh(home)
	f := a.Field("model")
	if err := f.Set("uni-switch/resp/gpt-5.5"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(web)
	s := string(b)
	ps := dshPiProviders(t, web)
	for key, want := range map[string]struct {
		api, base, name string
		models          []string
	}{
		"uni-switch":           {"openai-completions", gatewayV1(), "uni-switch", []string{"deepseek/pro", "deepseek/flash"}},
		"uni-switch-responses": {"openai-responses", gatewayV1(), "uni-switch · Responses", []string{"resp/gpt-5.5"}},
		"uni-switch-anthropic": {"anthropic-messages", gateway.URL(), "uni-switch · Anthropic", []string{"anth/claude-sonnet-5"}},
	} {
		r, ok := ps[key]
		if !ok {
			t.Fatalf("no route %s:\n%s", key, s)
		}
		if r.API != want.api || r.BaseURL != want.base || r.DisplayName != want.name || r.APIKeyEnv != dshKeyRef {
			t.Errorf("%s: %+v", key, r)
		}
		got := dshRouteModelIDs(r)
		for _, id := range want.models {
			if !contains(got, id) {
				t.Errorf("%s lacks %s: %v", key, id, got)
			}
		}
		// a model is on one route alone: dsh's list would show it twice
		for other, o := range ps {
			if other == key {
				continue
			}
			for _, id := range want.models {
				if contains(dshRouteModelIDs(o), id) {
					t.Errorf("%s is on %s too", id, other)
				}
			}
		}
	}
	if m := ps["uni-switch-anthropic"].Models[0]; m.Compat == nil || !m.Compat.ForceAdaptiveThinking {
		t.Errorf("a Claude thinking adaptively on Messages isn't told so: %+v", m)
	}
	if _, ok := ps["mine"]; !ok {
		t.Fatalf("the user's own route went:\n%s", s)
	}
	if !strings.Contains(s, "    provider: uni-switch-responses\n") || f.Get() != "uni-switch/resp/gpt-5.5" || a.Check() != "" {
		t.Fatalf("start: get %q, check %q\n%s", f.Get(), a.Check(), s)
	}
	// the effort is written beside the model on its route
	if err := a.Field("effort").Set("high"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(web)
	if !strings.Contains(string(b), "    provider: uni-switch-responses\n") || a.Field("effort").Get() != "high" || a.Check() != "" {
		t.Fatalf("effort moved the start off its route:\n%s", b)
	}
	if err := f.Set("uni-switch/anth/claude-sonnet-5"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(web)
	if !strings.Contains(string(b), "    provider: uni-switch-anthropic\n") || a.Check() != "" {
		t.Fatalf("Claude's start: check %q\n%s", a.Check(), b)
	}
	// Disconnect takes every route of uni-switch's out, the user's stays
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(web)
	if s := string(b); strings.Contains(s, "uni-switch") || !strings.Contains(s, "      mine:\n") {
		t.Fatalf("disconnect:\n%s", s)
	}
}

// A dsh wired before the routes were split has every model on uni-switch's
// one route and its start on it. A sync moves each model to its route and
// the start with its model, its effort and what dsh's Models page added to
// uni-switch's route kept, so the session started next still finds its model.
// A start the user picked on a route of their own stays where it is.
func TestDshSyncMovesAStartToItsModelsRoute(t *testing.T) {
	home, dir, web := dshProtocolHome(t)
	dshKeyFixture(dir)
	old := "# Your patch layer for this dsh profile.\n" +
		"- id: llm-pi-ai # uni-switch\n  name: \"@deepseek-ai/dsh-llm-pi-ai\"\n  config:\n    providers:\n" +
		"      mine:\n        displayName: Mine\n        apiKeyEnv: MINE_API_KEY\n        api: openai-completions\n        baseURL: https://mine.example/v1\n        models:\n          - id: m1\n" +
		"      uni-switch:\n        displayName: uni-switch\n        apiKeyEnv: " + dshKeyRef + "\n        api: openai-completions\n        baseURL: " + gatewayV1() + "\n        reasoning: high\n        models:\n" +
		"          - id: deepseek/pro\n          - id: deepseek/flash\n          - id: resp/gpt-5.5\n          - id: anth/claude-sonnet-5\n" +
		"- id: agent-default-model # uni-switch\n  config:\n    provider: uni-switch\n    model: \"resp/gpt-5.5\"\n    reasoningEffort: \"medium\"\n"
	os.WriteFile(web, []byte(old), 0o644)
	a := dsh(home)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(web)
	s := string(b)
	if !strings.Contains(s, "    provider: uni-switch-responses\n") || !strings.Contains(s, "resp/gpt-5.5") || !strings.Contains(s, "reasoningEffort: \"medium\"") && !strings.Contains(s, "reasoningEffort: medium") {
		t.Fatalf("start not moved with its model:\n%s", s)
	}
	if !strings.Contains(s, "- id: agent-default-model # uni-switch\n") {
		t.Fatalf("the start lost uni-switch's mark:\n%s", s)
	}
	ps := dshPiProviders(t, web)
	if contains(dshRouteModelIDs(ps["uni-switch"]), "resp/gpt-5.5") || !contains(dshRouteModelIDs(ps["uni-switch-responses"]), "resp/gpt-5.5") {
		t.Fatalf("routes:\n%s", s)
	}
	if !strings.Contains(s, "reasoning: high") || a.Field("model").Get() != "uni-switch/resp/gpt-5.5" || a.Check() != "" {
		t.Fatalf("get %q, check %q:\n%s", a.Field("model").Get(), a.Check(), s)
	}
	// the round after leaves it as it is
	dshWiredOnce()
	if again, _ := os.ReadFile(web); string(again) != s {
		t.Fatalf("written again:\n%s", again)
	}

	// a start on the user's own route is theirs
	mine := strings.Replace(s, "    provider: uni-switch-responses\n    model: resp/gpt-5.5\n", "    provider: mine\n    model: m1\n", 1)
	if mine == s {
		mine = strings.Replace(s, "    provider: uni-switch-responses\n    model: \"resp/gpt-5.5\"\n", "    provider: mine\n    model: m1\n", 1)
	}
	if mine == s {
		t.Fatalf("fixture:\n%s", s)
	}
	os.WriteFile(web, []byte(mine), 0o644)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(web); !strings.Contains(string(b), "    provider: mine\n    model: m1\n") {
		t.Fatalf("the user's own start was moved:\n%s", b)
	}
}

// The check speaks for the route a start names: a start on uni-switch's first
// route whose model is now on another is said (a session there fails with
// no such configured model) until a sync moves it.
func TestDshCheckSaysAStartOnAnotherRoutesModel(t *testing.T) {
	home, dir, web := dshProtocolHome(t)
	dshKeyFixture(dir)
	os.WriteFile(web, []byte("# Your patch layer for this dsh profile.\n"), 0o644)
	a := dsh(home)
	if err := a.Field("model").Set("uni-switch/resp/gpt-5.5"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(web)
	wrong := strings.Replace(string(b), "    provider: uni-switch-responses\n", "    provider: uni-switch\n", 1)
	os.WriteFile(web, []byte(wrong), 0o644)
	if d := a.Check(); !strings.Contains(d, "doesn't list") {
		t.Fatalf("check: %q\n%s", d, wrong)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if d := a.Check(); d != "" {
		t.Fatalf("after sync: %q", d)
	}
}
