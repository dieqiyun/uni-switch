package edit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const yamlDoc = `# omp settings

theme: dark # mine
modelRoles:
  # the main one
  default: anthropic/claude-opus-5 # keep
  smol: openai/gpt-6-mini
providers:
  mine:
    baseUrl: https://x
    models:
      - id: a
`

func TestYAMLNested(t *testing.T) {
	p := tmpFile(t, "config.yml", yamlDoc)
	if v, ok := GetYAML(p, "modelRoles.default"); !ok || v != "anthropic/claude-opus-5" {
		t.Fatalf("get: %q %v", v, ok)
	}
	if m := GetYAMLMap(p, "modelRoles"); m["smol"] != "openai/gpt-6-mini" || len(m) != 2 {
		t.Fatalf("map: %v", m)
	}
	type model struct {
		ID        string `yaml:"id"`
		Reasoning bool   `yaml:"reasoning,omitempty"`
	}
	err := SetYAML(p,
		KV{Path: "modelRoles.default", Value: "uni-switch/deepseek/pro"},
		KV{Path: "providers.uni-switch", Value: map[string]any{"baseUrl": "http://127.0.0.1/v1", "models": []model{{ID: "deepseek/pro", Reasoning: true}}}},
		KV{Path: "a.b.c", Value: 3},
	)
	if err != nil {
		t.Fatal(err)
	}
	got := read(t, p)
	for _, want := range []string{"# omp settings", "theme: dark # mine", "# the main one", "default: uni-switch/deepseek/pro # keep",
		"smol: openai/gpt-6-mini", "  mine:\n    baseUrl: https://x", "  uni-switch:\n    baseUrl: http://127.0.0.1/v1\n    models:\n      - id: deepseek/pro\n        reasoning: true",
		"a:\n  b:\n    c: 3"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Index(got, "theme") > strings.Index(got, "modelRoles") || strings.Index(got, "mine:") > strings.Index(got, "uni-switch:") {
		t.Errorf("order changed:\n%s", got)
	}

	if err := DelYAML(p, "modelRoles.default", "providers.uni-switch", "no.such"); err != nil {
		t.Fatal(err)
	}
	got = read(t, p)
	if strings.Contains(got, "default:") || strings.Contains(got, "uni-switch") || !strings.Contains(got, "smol:") || !strings.Contains(got, "mine:") {
		t.Fatalf("del:\n%s", got)
	}
}

func TestYAMLMissingAndScalarParent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.yml")
	if err := DelYAML(p, "a.b"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("DelYAML created a file")
	}
	if err := SetYAML(p, KV{Path: "a", Value: "x"}, KV{Path: "a.b", Value: "y"}); err != nil {
		t.Fatal(err)
	}
	if v, _ := GetYAML(p, "a.b"); v != "y" {
		t.Fatalf("got %q:\n%s", v, read(t, p))
	}
	os.WriteFile(p, []byte("- a\n"), 0o644)
	if err := SetYAML(p, KV{Path: "a", Value: "x"}); err == nil {
		t.Fatal("a sequence at the top should be refused")
	}
}

func TestJSONToYAML(t *testing.T) {
	src := tmpFile(t, "models.json", `{
  // mine
  "providers": {"z": {"baseUrl": "https://x", "apiKey": "123", "models": [{"id": "b"}, {"id": "a"}]}, "y": {}}
}`)
	dst := filepath.Join(filepath.Dir(src), "models.yml")
	if err := JSONToYAML(src, dst); err != nil {
		t.Fatal(err)
	}
	got := read(t, dst)
	want := "providers:\n  z:\n    baseUrl: https://x\n    apiKey: \"123\"\n    models:\n      - id: b\n      - id: a\n  y: {}\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}
