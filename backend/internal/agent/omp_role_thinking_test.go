package agent

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #1397 (KKKKKKKEM): omp's plan, vision and advisor roles can be put on a
// model as its other roles can, and every role but the default one has a
// thinking level of its own, written as omp 18.8.6 reads it: the role's
// model with ":<level>" after it (splitThinkingSuffix). The level stays
// when the role's model changes, goes with the role when it is reset, and
// is none to set while the role follows another or is a list of models.
func TestOmpRoleThinking(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("PATH", t.TempDir())
	for _, k := range []string{"PI_CODING_AGENT_DIR", "PI_CONFIG_DIR", "OMP_PROFILE", "PI_PROFILE"} {
		t.Setenv(k, "")
	}
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro", "flash"}}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".omp", "agent")
	configPath := filepath.Join(dir, "config.yml")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(configPath, []byte("defaultThinkingLevel: medium\nmodelRoles:\n  default: anthropic/claude-opus-5\n  task: openai/gpt-6,anthropic/claude-opus-5:high\n"), 0o644)
	a := omp(home)
	role := func(name string) string { v, _ := edit.GetYAML(configPath, "modelRoles."+name); return v }
	apply := func(k, v string) {
		t.Helper()
		if err := a.Apply(k, v); err != nil {
			t.Fatalf("%s = %q: %v", k, v, err)
		}
	}

	for _, k := range []string{"plan", "vision", "advisor"} {
		f := a.Field(k)
		if f == nil || !f.Quiet || f.Label != k {
			t.Fatalf("role %s: %+v", k, f)
		}
		apply(k, "uni-switch/deepseek/pro")
		if got := role(k); got != "uni-switch/deepseek/pro" {
			t.Fatalf("modelRoles.%s: %q", k, got)
		}
	}
	if a.Field("model_thinking") != nil {
		t.Fatal("the default role's level is the session's, not a field of its own")
	}

	// unset, a role has no level to pick; on one model it has omp's
	slow := a.Field("slow_thinking")
	if slow == nil || slow.Label != "slow thinking" || !slow.Quiet {
		t.Fatalf("slow thinking: %+v", slow)
	}
	if o := slow.Options(a.Values()); len(o) != 0 {
		t.Fatalf("slow unset offers levels: %+v", o)
	}
	if err := slow.Set("high"); err == nil {
		t.Fatal("a level set on a role with no model")
	}
	apply("slow", "uni-switch/deepseek/pro")
	var levels []string
	for _, o := range slow.Options(a.Values()) {
		levels = append(levels, o.Value)
	}
	if want := []string{"auto", "off", "minimal", "low", "medium", "high", "xhigh", "max"}; !slices.Equal(levels, want) {
		t.Fatalf("levels %v, want %v", levels, want)
	}

	apply("slow_thinking", "high")
	if got := role("slow"); got != "uni-switch/deepseek/pro:high" {
		t.Fatalf("modelRoles.slow: %q", got)
	}
	if got := slow.Get(); got != "high" {
		t.Fatalf("slow thinking reads %q", got)
	}
	if v, _ := edit.GetYAML(configPath, "defaultThinkingLevel"); v != "medium" {
		t.Fatalf("the session's level moved: %q", v)
	}
	// the level stays with the role on another model
	apply("slow", "uni-switch/deepseek/flash")
	if got := role("slow"); got != "uni-switch/deepseek/flash:high" {
		t.Fatalf("slow on another model: %q", got)
	}
	apply("advisor_thinking", "max")
	if got := role("advisor"); got != "uni-switch/deepseek/pro:max" {
		t.Fatalf("modelRoles.advisor: %q", got)
	}
	// none: the role at the session's level again
	apply("slow_thinking", "")
	if got := role("slow"); got != "uni-switch/deepseek/flash" {
		t.Fatalf("slow's level cleared: %q", got)
	}
	if err := slow.Set("loud"); err == nil {
		t.Fatal("a level omp doesn't read was written")
	}

	// a list of models omp falls back through is the user's own: no level
	sub := a.Field("subagent_thinking")
	if o := sub.Options(a.Values()); len(o) != 0 || sub.Get() != "" {
		t.Fatalf("a list offers levels: %+v, reads %q", o, sub.Get())
	}
	if err := sub.Set("low"); err == nil || role("task") != "openai/gpt-6,anthropic/claude-opus-5:high" {
		t.Fatalf("a level written onto a list: %v, %q", err, role("task"))
	}

	// reset, the role and its level go, and uni-switch's provider with the
	// last role on it
	for _, k := range []string{"slow", "plan", "vision", "advisor"} {
		apply(k, "")
	}
	if got := role("advisor"); got != "" {
		t.Fatalf("advisor reset: %q", got)
	}
	if a.Field("advisor_thinking").Get() != "" {
		t.Fatal("advisor's level outlived it")
	}
	if v, ok := edit.GetYAML(filepath.Join(dir, "models.yml"), "providers.uni-switch.baseUrl"); ok && v != "" {
		t.Fatal("uni-switch's provider stayed with no role on it")
	}
}

// #1397 (KKKKKKKEM, after v0.1.1164): commit and tiny were still missing.
// Every one of omp's chat roles, as CHAT_MODEL_ROLE_IDS in its pi-tui
// model-browser.ts lists them (18.6.1 to 18.8.8), is a field of the row
// with a thinking level of its own but the default one; a role the user had
// set off uni-switch comes back when uni-switch's is reset.
func TestOmpEveryChatRole(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("PATH", t.TempDir())
	for _, k := range []string{"PI_CODING_AGENT_DIR", "PI_CONFIG_DIR", "OMP_PROFILE", "PI_PROFILE"} {
		t.Setenv(k, "")
	}
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro", "flash"}}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".omp", "agent")
	configPath := filepath.Join(dir, "config.yml")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(configPath, []byte("modelRoles:\n  tiny: openai/gpt-6-mini\n"), 0o644)
	a := omp(home)
	role := func(name string) string { v, _ := edit.GetYAML(configPath, "modelRoles."+name); return v }

	// omp's name for the role → uni-switch's field key
	chat := map[string]string{
		"default": "model", "smol": "small", "slow": "slow", "vision": "vision", "plan": "plan",
		"commit": "commit", "tiny": "tiny", "memory": "memory", "task": "subagent", "advisor": "advisor",
	}
	for name, key := range chat {
		f := a.Field(key)
		if f == nil || f.Quiet == (name == "default") {
			t.Fatalf("omp's %s role: %+v", name, f)
		}
		if err := a.Apply(key, "uni-switch/deepseek/pro"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := role(name); got != "uni-switch/deepseek/pro" {
			t.Fatalf("modelRoles.%s: %q", name, got)
		}
		if name == "default" {
			continue
		}
		if err := a.Apply(key+"_thinking", "high"); err != nil {
			t.Fatalf("%s thinking: %v", name, err)
		}
		if got := role(name); got != "uni-switch/deepseek/pro:high" {
			t.Fatalf("modelRoles.%s at high: %q", name, got)
		}
	}
	for _, k := range []string{"commit", "tiny", "memory"} {
		if f := a.Field(k); f.Label != k {
			t.Fatalf("%s is labelled %q", k, f.Label)
		}
		if err := a.Apply(k, ""); err != nil {
			t.Fatal(err)
		}
	}
	if got := role("tiny"); got != "openai/gpt-6-mini" {
		t.Fatalf("the user's own tiny role not put back: %q", got)
	}
	if got := role("commit") + role("memory"); got != "" {
		t.Fatalf("commit and memory reset: %q", got)
	}
}
