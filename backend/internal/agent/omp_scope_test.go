package agent

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"gopkg.in/yaml.v3"
)

// ompScopeHome is a home with omp's agent folder and a provider of uni-switch's.
func ompScopeHome(t *testing.T) (home, configPath, modelsPath string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	for _, k := range []string{"PI_CODING_AGENT_DIR", "PI_CONFIG_DIR", "OMP_PROFILE", "PI_PROFILE"} {
		t.Setenv(k, "")
	}
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro", "flash"}}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".omp", "agent")
	os.MkdirAll(dir, 0o755)
	return home, filepath.Join(dir, "config.yml"), filepath.Join(dir, "models.yml")
}

func ompScopeRead(t *testing.T, p string) (map[string]any, string) {
	t.Helper()
	b, _ := os.ReadFile(p)
	var m map[string]any
	yaml.Unmarshal(b, &m)
	return m, string(b)
}

// The reporter's case (#1769): omp's config.yml already has a non-empty
// enabledModels, and the default role is set to one of uni-switch's models
// outside it. omp would start new sessions on a model from the list, and
// its picker and Ctrl+P show only the list's models, so uni-switch/** joins its
// end: every model uni-switch gives omp, the one picked among them. The user's
// entries, their order and comments stay. Only the default role is held to
// the list (omp resolves its other roles against every model it has).
func TestOmpDefaultJoinsEnabledModels(t *testing.T) {
	home, configPath, _ := ompScopeHome(t)
	os.WriteFile(configPath, []byte("# mine\nenabledModels:\n  - provider-a/model-a # first\n  - provider-b/model-b\nmodelRoles:\n  default: provider-a/model-a\n"), 0o644)
	a := omp(home)
	if err := a.Field("model").Set("uni-switch/deepseek/pro:high"); err != nil {
		t.Fatal(err)
	}
	want := []string{"provider-a/model-a", "provider-b/model-b", "uni-switch/**"}
	if got := edit.GetYAMLList(configPath, "enabledModels"); !slices.Equal(got, want) {
		t.Fatalf("enabledModels: %q, want %q", got, want)
	}
	if d := a.Check(); d != "" {
		t.Fatalf("check: %q", d)
	}
	if _, raw := ompScopeRead(t, configPath); !strings.Contains(raw, "# mine") || !strings.Contains(raw, "# first") {
		t.Fatalf("comments lost:\n%s", raw)
	}
	if v, _ := edit.GetYAML(configPath, "modelRoles.default"); v != "uni-switch/deepseek/pro:high" {
		t.Fatalf("default: %q", v)
	}
	// set again, or at another level: listed once
	for _, v := range []string{"uni-switch/deepseek/pro:high", "uni-switch/deepseek/pro:low"} {
		if err := a.Field("model").Set(v); err != nil {
			t.Fatal(err)
		}
	}
	if got := edit.GetYAMLList(configPath, "enabledModels"); !slices.Equal(got, want) {
		t.Fatalf("set again: %q", got)
	}
	// a role other than the default isn't held to the list
	if err := a.Field("small").Set("uni-switch/deepseek/flash"); err != nil {
		t.Fatal(err)
	}
	if got := edit.GetYAMLList(configPath, "enabledModels"); !slices.Equal(got, want) {
		t.Fatalf("smol: %q", got)
	}
	// the user's own model outside the list joins it too, as it would never
	// start either
	if err := a.Field("model").Set("provider-c/model-c"); err != nil {
		t.Fatal(err)
	}
	if got := edit.GetYAMLList(configPath, "enabledModels"); !slices.Contains(got, "provider-c/model-c") {
		t.Fatalf("own model: %q", got)
	}
}

// No list is made where there is none, an empty one stays as it is, and so
// does one with only entries scoped to folders (a plain entry would hold
// every other folder to it). An entry that already takes the model in —
// the same model at a level, a glob — is enough.
func TestOmpEnabledModelsLeftAlone(t *testing.T) {
	cases := []struct{ name, config string }{
		{"none", "modelRoles:\n  default: a/b\n"},
		{"empty", "enabledModels: []\nmodelRoles:\n  default: a/b\n"},
		{"folders only", "enabledModels:\n  - path: /work\n    models:\n      - a/b\n"},
		{"same at a level", "enabledModels:\n  - a/b\n  - uni-switch/deepseek/pro:max\n"},
		{"glob", "enabledModels:\n  - a/b\n  - uni-switch/deepseek/*\n"},
		// omp matches a glob against provider/id and the id alone (Bun.Glob,
		// whose * stops at a slash), and a model of uni-switch's id is
		// provider/model
		{"glob on the id", "enabledModels:\n  - a/b\n  - deepseek/*\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home, configPath, _ := ompScopeHome(t)
			os.WriteFile(configPath, []byte(c.config), 0o644)
			before, _ := ompScopeRead(t, configPath)
			if err := omp(home).Field("model").Set("uni-switch/deepseek/pro"); err != nil {
				t.Fatal(err)
			}
			after, raw := ompScopeRead(t, configPath)
			bs, _ := yaml.Marshal(before["enabledModels"])
			as, _ := yaml.Marshal(after["enabledModels"])
			if string(bs) != string(as) {
				t.Fatalf("enabledModels changed:\n%s", raw)
			}
		})
	}
}

// Once nothing else is on uni-switch, its models leave the list with its
// provider; a list left with nothing goes, never written as []. One scoped
// to folders is the user's and keeps the provider.
func TestOmpEnabledModelsCleared(t *testing.T) {
	hasUniSwitch := func(modelsPath string) bool {
		m, _ := ompScopeRead(t, modelsPath)
		p, _ := m["providers"].(map[string]any)
		return p["uni-switch"] != nil
	}

	home, configPath, modelsPath := ompScopeHome(t)
	os.WriteFile(configPath, []byte("enabledModels:\n  - provider-a/model-a # mine\nmodelRoles:\n  default: provider-a/model-a\n"), 0o644)
	f := omp(home).Field("model")
	if err := f.Set("uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if v, _ := edit.GetYAML(configPath, "modelRoles.default"); v != "provider-a/model-a" {
		t.Fatalf("reset: %q", v)
	}
	if got := edit.GetYAMLList(configPath, "enabledModels"); !slices.Equal(got, []string{"provider-a/model-a"}) {
		t.Fatalf("enabledModels: %q", got)
	}
	if _, raw := ompScopeRead(t, configPath); !strings.Contains(raw, "# mine") {
		t.Fatalf("comment lost:\n%s", raw)
	}
	if hasUniSwitch(modelsPath) {
		t.Fatal("uni-switch's provider stayed")
	}

	// the list had only uni-switch's model: the key goes
	os.WriteFile(configPath, []byte("enabledModels:\n  - uni-switch/deepseek/flash\nmodelRoles:\n  default: uni-switch/deepseek/flash\n"), 0o644)
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if c, raw := ompScopeRead(t, configPath); c["enabledModels"] != nil || strings.Contains(raw, "enabledModels") {
		t.Fatalf("emptied list kept:\n%s", raw)
	}

	// a folder's own list on uni-switch keeps it, and its entries
	os.WriteFile(configPath, []byte("enabledModels:\n  - uni-switch/deepseek/flash\n  - path: /work\n    models:\n      - uni-switch/deepseek/pro\nmodelRoles:\n  default: uni-switch/deepseek/flash\n"), 0o644)
	if err := f.Set("uni-switch/deepseek/flash"); err != nil {
		t.Fatal(err)
	}
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if !hasUniSwitch(modelsPath) {
		t.Fatal("a folder's list on uni-switch lost uni-switch's provider")
	}
	if _, raw := ompScopeRead(t, configPath); !strings.Contains(raw, "uni-switch/deepseek/pro") {
		t.Fatalf("folder's entry dropped:\n%s", raw)
	}
}

// A default role on uni-switch that a list written by hand leaves out shows as
// a model that won't start, and setting it again (Reapply) mends it.
func TestOmpEnabledModelsLeavingOutIsDrift(t *testing.T) {
	home, configPath, _ := ompScopeHome(t)
	os.WriteFile(configPath, []byte("modelRoles:\n  default: provider-a/model-a\n"), 0o644)
	a := omp(home)
	if err := a.Field("model").Set("uni-switch/deepseek/pro:high"); err != nil {
		t.Fatal(err)
	}
	if d := a.Check(); d != "" {
		t.Fatalf("no list: %q", d)
	}
	if err := edit.SetYAML(configPath, edit.KV{Path: "enabledModels", Value: []string{"provider-a/model-a"}}); err != nil {
		t.Fatal(err)
	}
	if d := a.Check(); !strings.Contains(d, "enabledModels") || !strings.Contains(d, "uni-switch/deepseek/pro") {
		t.Fatalf("check: %q", d)
	}
	if err := a.Field("model").Set(a.Field("model").Get()); err != nil {
		t.Fatal(err)
	}
	if d := a.Check(); d != "" {
		t.Fatalf("set again: %q", d)
	}
}

// real-jacket on #1769, after v0.1.1214: the default role started, but
// omp's picker and Ctrl+P listed only the old list and the one model uni-switch
// had added, while uni-switch's Agents page offers its whole catalog. Their
// config as v0.1.1214 left it: the model picked again puts uni-switch/** in
// place of the one model, so omp's picker has all of uni-switch's; the other
// roles' models with it, without a list entry each.
func TestOmpEnabledModelsTakeEveryUniSwitchModel(t *testing.T) {
	home, configPath, modelsPath := ompScopeHome(t)
	os.WriteFile(configPath, []byte("enabledModels:\n  - provider-a/model-a\n  - provider-b/model-b\n  - uni-switch/deepseek/pro\nmodelRoles:\n  default: uni-switch/deepseek/pro:high\n"), 0o644)
	a := omp(home)
	if err := a.Field("model").Set("uni-switch/deepseek/flash:high"); err != nil {
		t.Fatal(err)
	}
	want := []string{"provider-a/model-a", "provider-b/model-b", "uni-switch/**"}
	if got := edit.GetYAMLList(configPath, "enabledModels"); !slices.Equal(got, want) {
		t.Fatalf("enabledModels: %q, want %q", got, want)
	}
	// every model uni-switch wrote for omp is one the list takes in
	m, _ := ompScopeRead(t, modelsPath)
	ms, _ := m["providers"].(map[string]any)["uni-switch"].(map[string]any)["models"].([]any)
	if len(ms) < 2 {
		t.Fatalf("models.yml: %v", m)
	}
	for _, x := range ms {
		id := "uni-switch/" + x.(map[string]any)["id"].(string)
		if !ompScopeTakes(want, id) {
			t.Errorf("%s isn't in omp's list", id)
		}
	}
	if !ompScopeTakes(want, "provider-a/model-a") || ompScopeTakes(want, "provider-c/model-c") {
		t.Error("the user's own entries changed meaning")
	}
	if d := a.Check(); d != "" {
		t.Fatalf("check: %q", d)
	}
	// picked again, it is there once
	if err := a.Field("model").Set("uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if got := edit.GetYAMLList(configPath, "enabledModels"); !slices.Equal(got, want) {
		t.Fatalf("set again: %q", got)
	}
	// disconnected, it goes with uni-switch's provider
	if err := a.Field("model").Set(""); err != nil {
		t.Fatal(err)
	}
	if got := edit.GetYAMLList(configPath, "enabledModels"); !slices.Equal(got, want[:2]) {
		t.Fatalf("after reset: %q", got)
	}
}
