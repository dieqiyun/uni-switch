package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #804 (liuweifeng): dsh applies its home patch layer, ~/.dsh/cordis.patch.yml,
// over every profile's, so a llm-pi-ai or agent-default-model entry there
// replaced what uni-switch wrote in the profiles, and dsh ran the home layer's
// old snapshot while uni-switch read as connected. uni-switch now writes those two
// entries there too when the home layer has them, the user's routes beside
// uni-switch's kept, and adds nothing to a home layer without them.
func TestDshHomePatchLayer(t *testing.T) {
	home, dir, web := dshRouteHome(t)
	desktop := filepath.Join(dir, "profiles", "desktop", "cordis.patch.yml")
	os.MkdirAll(filepath.Dir(desktop), 0o755)
	os.WriteFile(web, []byte("[]\n"), 0o644)
	os.WriteFile(desktop, []byte("[]\n"), 0o644)
	hp := filepath.Join(dir, "cordis.patch.yml")
	userStart := "- id: agent-default-model\n  config:\n    provider: deepseek-official\n    model: deepseek-v4-pro\n"
	os.WriteFile(hp, []byte("# shared by web and desktop\n"+
		"- id: llm-pi-ai\n  name: \"@deepseek-ai/dsh-llm-pi-ai\"\n  config:\n    providers:\n      mine:\n        displayName: Mine\n        apiKeyEnv: MINE_API_KEY\n        api: openai-completions\n        baseURL: https://mine.example/v1\n        models:\n          - id: m1\n"+
		"      uni-switch:\n        displayName: uni-switch\n        apiKeyEnv: "+dshKeyRef+"\n        api: openai-completions\n        baseURL: "+gatewayV1()+"\n        models:\n          - id: old/gone\n"+
		userStart), 0o644)
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }
	a := dsh(home)
	f := a.Field("model")

	// the home layer's start goes over the profiles'
	if f.Get() != "deepseek-v4-pro" {
		t.Fatalf("get %q", f.Get())
	}
	if err := f.Set("uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	s := read(hp)
	for _, want := range []string{"# shared by web and desktop\n", "      mine:\n", "- id: deepseek/pro", "- id: deepseek/flash", "provider: uni-switch", `model: "deepseek/pro"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in the home layer:\n%s", want, s)
		}
	}
	if strings.Contains(s, "old/gone") || strings.Contains(s, "deepseek-official") {
		t.Fatalf("the home layer kept its snapshot:\n%s", s)
	}
	if f.Get() != "uni-switch/deepseek/pro" || a.Check() != "" || !a.Joined() {
		t.Fatalf("get %q, check %q, joined %v", f.Get(), a.Check(), a.Joined())
	}

	// a home llm-pi-ai written without uni-switch's route (dsh's Models page) is
	// said, and a sync puts the route back beside the user's
	os.WriteFile(hp, []byte("- id: llm-pi-ai\n  name: \"@deepseek-ai/dsh-llm-pi-ai\"\n  config:\n    providers:\n      mine:\n        displayName: Mine\n        apiKeyEnv: MINE_API_KEY\n        api: openai-completions\n        baseURL: https://mine.example/v1\n        models:\n          - id: m1\n"), 0o644)
	if c := a.Check(); !strings.Contains(c, "home patch layer") {
		t.Fatalf("check %q", c)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if s := read(hp); !strings.Contains(s, "      uni-switch:\n") || !strings.Contains(s, "      mine:\n") || a.Check() != "" {
		t.Fatalf("check %q, home layer:\n%s", a.Check(), s)
	}

	// back to dsh's own model: uni-switch's route leaves the home layer, the
	// user's stays
	if err := f.Set("deepseek-flash"); err != nil {
		t.Fatal(err)
	}
	if s := read(hp); strings.Contains(s, "      uni-switch:\n") || !strings.Contains(s, "      mine:\n") {
		t.Fatalf("home layer:\n%s", s)
	}

	// a home layer without those entries is left as it is
	other := "- id: tool-shell\n  config:\n    timeout: 30\n"
	os.WriteFile(hp, []byte(other), 0o644)
	if err := f.Set("uni-switch/deepseek/flash"); err != nil {
		t.Fatal(err)
	}
	if read(hp) != other || f.Get() != "uni-switch/deepseek/flash" {
		t.Fatalf("get %q, home layer:\n%s", f.Get(), read(hp))
	}
}

// A model and effort picked in uni-switch reach the home layer, and setting none
// gives it its own start back.
func TestDshHomePatchLayerUnwire(t *testing.T) {
	home, dir, web := dshRouteHome(t)
	os.WriteFile(web, []byte("[]\n"), 0o644)
	hp := filepath.Join(dir, "cordis.patch.yml")
	userStart := "- id: agent-default-model\n  config:\n    provider: deepseek-official\n    model: deepseek-v4-pro\n"
	os.WriteFile(hp, []byte(userStart), 0o644)
	a := dsh(home)
	if err := a.Field("model").Set("uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if a.Field("model").Get() != "uni-switch/deepseek/pro" {
		t.Fatalf("get %q", a.Field("model").Get())
	}
	// one of dsh's own models, and its effort, go there too
	if err := a.Field("model").Set("deepseek-flash"); err != nil {
		t.Fatal(err)
	}
	if err := a.Field("effort").Set("max"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(hp); !strings.Contains(string(b), `model: "deepseek-flash"`) || !strings.Contains(string(b), `reasoningEffort: "max"`) || a.Field("effort").Get() != "max" {
		t.Fatalf("effort %q, home layer:\n%s", a.Field("effort").Get(), b)
	}
	if err := a.Field("model").Set(""); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(hp); string(b) != userStart {
		t.Fatalf("home layer:\n%s", b)
	}
}
