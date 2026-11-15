package edit

import (
	"os"
	"path/filepath"
	"testing"
)

func tmpFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if content != "" || name == "empty.json" {
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestJSONReplaceKeepsEverything(t *testing.T) {
	in := "{\n  \"env\": {\n    \"X\": \"1\"\n  },\n  \"model\": \"old\",\n  \"hooks\": []\n}\n"
	p := tmpFile(t, "settings.json", in)
	if err := SetJSON(p, KV{Path: "model", Value: "new"}); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"env\": {\n    \"X\": \"1\"\n  },\n  \"model\": \"new\",\n  \"hooks\": []\n}\n"
	if got := read(t, p); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if v, ok := GetJSON(p, "model"); !ok || v != "new" {
		t.Fatalf("GetJSON = %q, %v", v, ok)
	}
}

func TestJSONInsertTopLevel(t *testing.T) {
	in := "{\n  \"a\": 1\n}\n"
	p := tmpFile(t, "settings.json", in)
	if err := SetJSON(p, KV{Path: "model", Value: "m"}); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"model\": \"m\",\n  \"a\": 1\n}\n"
	if got := read(t, p); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestJSONInsertNestedIntoExistingObject(t *testing.T) {
	in := "{\n  \"model\": {\n    \"other\": true\n  }\n}\n"
	p := tmpFile(t, "settings.json", in)
	if err := SetJSON(p, KV{Path: "model.name", Value: "gemini-2.5-pro"}); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"model\": {\n    \"name\": \"gemini-2.5-pro\",\n    \"other\": true\n  }\n}\n"
	if got := read(t, p); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestJSONInsertNestedCreatesParents(t *testing.T) {
	in := "{\n  \"ui\": {}\n}\n"
	p := tmpFile(t, "settings.json", in)
	if err := SetJSON(p, KV{Path: "models.large.model", Value: "x"}); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"models\": {\n    \"large\": {\n      \"model\": \"x\"\n    }\n  },\n  \"ui\": {}\n}\n"
	if got := read(t, p); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestJSONScalarParentBecomesObject(t *testing.T) {
	in := "{\n  \"model\": \"gemini-2.5-pro\"\n}\n"
	p := tmpFile(t, "settings.json", in)
	if err := SetJSON(p, KV{Path: "model.name", Value: "gemini-3-pro"}); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"model\": {\n    \"name\": \"gemini-3-pro\"\n  }\n}\n"
	if got := read(t, p); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestJSONCKeepsComments(t *testing.T) {
	in := "{\n  // schema\n  \"$schema\": \"https://opencode.ai/config.json\", /* x */\n  \"model\": \"a/b\"\n}\n"
	p := tmpFile(t, "opencode.jsonc", in)
	if err := SetJSON(p, KV{Path: "model", Value: "anthropic/claude-sonnet-5"}, KV{Path: "small_model", Value: "anthropic/claude-haiku-4-5"}); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"small_model\": \"anthropic/claude-haiku-4-5\",\n  // schema\n  \"$schema\": \"https://opencode.ai/config.json\", /* x */\n  \"model\": \"anthropic/claude-sonnet-5\"\n}\n"
	if got := read(t, p); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if v, _ := GetJSON(p, "small_model"); v != "anthropic/claude-haiku-4-5" {
		t.Fatalf("GetJSON = %q", v)
	}
}

func TestJSONEmptyAndMissingFile(t *testing.T) {
	p := tmpFile(t, "empty.json", "")
	if err := SetJSON(p, KV{Path: "model", Value: "m"}, KV{Path: "flag", Value: true}); err != nil {
		t.Fatal(err)
	}
	if got, want := read(t, p), "{\n  \"flag\": true,\n  \"model\": \"m\"\n}\n"; got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	p2 := filepath.Join(t.TempDir(), "sub", "new.json")
	if err := SetJSON(p2, KV{Path: "model", Value: "m"}); err != nil {
		t.Fatal(err)
	}
	if v, _ := GetJSON(p2, "model"); v != "m" {
		t.Fatalf("missing file: got %q", v)
	}
}

func TestJSONCompact(t *testing.T) {
	p := tmpFile(t, "c.json", `{"a":1}`)
	if err := SetJSON(p, KV{Path: "model", Value: "m"}); err != nil {
		t.Fatal(err)
	}
	if got, want := read(t, p), `{"model":"m","a":1}`; got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestJSONBracesInStrings(t *testing.T) {
	in := "{\n  \"cmd\": \"echo {\\\"x\\\": 1}\"\n}\n"
	p := tmpFile(t, "s.json", in)
	if err := SetJSON(p, KV{Path: "model", Value: "m"}); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"model\": \"m\",\n  \"cmd\": \"echo {\\\"x\\\": 1}\"\n}\n"
	if got := read(t, p); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTOMLTop(t *testing.T) {
	in := "model_reasoning_effort = \"medium\"\nmodel = \"old\" # comment\n\n[projects.\"/x\"]\nmodel = \"not-top\"\n"
	p := tmpFile(t, "config.toml", in)
	if v, ok := GetTOMLTop(p, "model"); !ok || v != "old" {
		t.Fatalf("get = %q %v", v, ok)
	}
	if err := SetTOMLTop(p, KV{Path: "model", Value: "gpt-5.5"}, KV{Path: "model_provider", Value: "openai"}); err != nil {
		t.Fatal(err)
	}
	want := "model_reasoning_effort = \"medium\"\nmodel = \"gpt-5.5\"\nmodel_provider = \"openai\"\n\n[projects.\"/x\"]\nmodel = \"not-top\"\n"
	if got := read(t, p); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if v, _ := GetTOMLTop(p, "model"); v != "gpt-5.5" {
		t.Fatalf("get after set = %q", v)
	}
}

func TestTOMLMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := SetTOMLTop(p, KV{Path: "model", Value: "m"}); err != nil {
		t.Fatal(err)
	}
	if got, want := read(t, p), "model = \"m\"\n"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestYAMLTop(t *testing.T) {
	in := "GOOSE_PROVIDER: anthropic\nGOOSE_MODEL: claude-3-7 # old\nextensions:\n  developer:\n    enabled: true\n"
	p := tmpFile(t, "config.yaml", in)
	if v, _ := GetYAMLTop(p, "GOOSE_MODEL"); v != "claude-3-7" {
		t.Fatalf("get = %q", v)
	}
	if err := SetYAMLTop(p, KV{Path: "GOOSE_PROVIDER", Value: "openrouter"}, KV{Path: "GOOSE_MODEL", Value: "z-ai/glm-5.2:batch"}); err != nil {
		t.Fatal(err)
	}
	want := "GOOSE_PROVIDER: openrouter\nGOOSE_MODEL: \"z-ai/glm-5.2:batch\"\nextensions:\n  developer:\n    enabled: true\n"
	if got := read(t, p); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if v, _ := GetYAMLTop(p, "GOOSE_MODEL"); v != "z-ai/glm-5.2:batch" {
		t.Fatalf("get after set = %q", v)
	}
}
