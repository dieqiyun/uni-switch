package edit

import (
	"os"
	"strings"
	"testing"
)

// marking says "N" for any value on port 3425, as agent.lineNote does for
// the gateway's
func marking(t *testing.T) {
	t.Helper()
	Mark = func(table, key, value string) string {
		if strings.Contains(value, ":3425") {
			if table != "" {
				return "T " + table
			}
			return "N"
		}
		return ""
	}
	t.Cleanup(func() { Mark = nil })
}

func TestMarkTOML(t *testing.T) {
	marking(t)
	p := tmpFile(t, "config.toml", "model = \"gpt-5\"\nmine = \"http://127.0.0.1:3425/x\" # my own\n")
	if err := SetTOMLTop(p, KV{Path: "openai_base_url", Value: "http://127.0.0.1:3425/backend-api/codex"}); err != nil {
		t.Fatal(err)
	}
	if err := SetTOMLTable(p, "model_providers.uni-switch", KV{Path: "base_url", Value: "http://127.0.0.1:3425/v1"}, KV{Path: "name", Value: "uni-switch"}); err != nil {
		t.Fatal(err)
	}
	got := read(t, p)
	for _, want := range []string{
		"# uni-switch: N\nopenai_base_url = \"http://127.0.0.1:3425/backend-api/codex\"\n",
		"# uni-switch: T model_providers.uni-switch\nbase_url = \"http://127.0.0.1:3425/v1\"\n",
		"\nname = \"uni-switch\"\n",
		"model = \"gpt-5\"\n",
		// a line of the user's own, which uni-switch didn't write, gets none,
		// and their comment stays theirs
		"\"gpt-5\"\nmine = \"http://127.0.0.1:3425/x\" # my own\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	// written again: one note, not two
	if err := SetTOMLTopPreserving(p, KV{Path: "openai_base_url", Value: "http://127.0.0.1:3425/backend-api/codex"}); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(read(t, p), "# uni-switch: N"); n != 1 {
		t.Fatalf("%d notes on openai_base_url:\n%s", n, read(t, p))
	}
	// a value of the user's put in its place, its spacing kept: the note goes
	if err := SetTOMLTopPreserving(p, KV{Path: "openai_base_url", Value: "https://proxy.example/v1"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); !strings.Contains(got, "openai_base_url = \"https://proxy.example/v1\"\n") {
		t.Fatalf("note kept on the user's value:\n%s", got)
	}
	// taken out, with its note
	if err := DelTOMLKey(p, "model_providers.uni-switch", "base_url"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); strings.Contains(got, "T model_providers") {
		t.Fatalf("note outlived its key:\n%s", got)
	}
	if err := validateTOML([]byte(read(t, p))); err != nil {
		t.Fatal(err)
	}
}

func TestMarkTOMLKeepsCRLF(t *testing.T) {
	marking(t)
	p := tmpFile(t, "config.toml", "model = \"gpt-5\"\r\n")
	if err := SetTOMLTop(p, KV{Path: "openai_base_url", Value: "http://127.0.0.1:3425/x"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); got != "model = \"gpt-5\"\r\n# uni-switch: N\r\nopenai_base_url = \"http://127.0.0.1:3425/x\"\r\n" {
		t.Fatalf("got %q", got)
	}
}

func TestMarkTOMLOff(t *testing.T) {
	p := tmpFile(t, "config.toml", "")
	if err := SetTOMLTop(p, KV{Path: "openai_base_url", Value: "http://127.0.0.1:3425/x"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); strings.Contains(got, "#") {
		t.Fatalf("a note with Mark unset: %q", got)
	}
}

func TestMarkYAML(t *testing.T) {
	marking(t)
	p := tmpFile(t, "config.yaml", "model:\n  default: gpt-5\n")
	if err := SetYAML(p, KV{Path: "model.base_url", Value: "http://127.0.0.1:3425/v1"}); err != nil {
		t.Fatal(err)
	}
	got := read(t, p)
	if !strings.Contains(got, "  # uni-switch: T model\n  base_url: http://127.0.0.1:3425/v1\n") {
		t.Fatalf("got:\n%s", got)
	}
	if err := SetYAML(p, KV{Path: "model.base_url", Value: "https://api.example/v1"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); strings.Contains(got, "uni-switch:") {
		t.Fatalf("note kept on the user's value:\n%s", got)
	}
}

func TestMarkYAMLTop(t *testing.T) {
	marking(t)
	p := tmpFile(t, "config.yaml", "GOOSE_MODEL: gpt-5 # mine\n")
	if err := SetYAMLTop(p, KV{Path: "OPENAI_HOST", Value: "http://127.0.0.1:3425"}); err != nil {
		t.Fatal(err)
	}
	got := read(t, p)
	if got != "GOOSE_MODEL: gpt-5 # mine\n# uni-switch: N\nOPENAI_HOST: \"http://127.0.0.1:3425\"\n" {
		t.Fatalf("got %q", got)
	}
	if v, _ := GetYAMLTop(p, "OPENAI_HOST"); v != "http://127.0.0.1:3425" {
		t.Fatalf("read back %q", v)
	}
	if err := SetYAMLTop(p, KV{Path: "OPENAI_HOST", Value: "http://127.0.0.1:3425"}); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(read(t, p), "uni-switch: N"); n != 1 {
		t.Fatalf("%d notes:\n%s", n, read(t, p))
	}
	if err := DelYAMLTop(p, "OPENAI_HOST"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); got != "GOOSE_MODEL: gpt-5 # mine\n" {
		t.Fatalf("got %q", got)
	}
}

func TestMarkEnv(t *testing.T) {
	marking(t)
	p := tmpFile(t, ".env", "# my keys\nGEMINI_API_KEY=abc\n")
	if err := SetEnvFile(p, KV{Path: "GOOGLE_GEMINI_BASE_URL", Value: "http://127.0.0.1:3425"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); got != "# my keys\nGEMINI_API_KEY=abc\n# uni-switch: N\nGOOGLE_GEMINI_BASE_URL=http://127.0.0.1:3425\n" {
		t.Fatalf("got %q", got)
	}
	if v, _ := GetEnvFile(p, "GOOGLE_GEMINI_BASE_URL"); v != "http://127.0.0.1:3425" {
		t.Fatalf("read back %q", v)
	}
	if err := SetEnvFile(p, KV{Path: "GOOGLE_GEMINI_BASE_URL", Value: "http://127.0.0.1:3425"}); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(read(t, p), "uni-switch: N"); n != 1 {
		t.Fatalf("%d notes:\n%s", n, read(t, p))
	}
	// the user's comment above their own key is never taken
	if err := SetEnvFile(p, KV{Path: "GEMINI_API_KEY", Value: "def"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(read(t, p), "# my keys\nGEMINI_API_KEY=def\n") {
		t.Fatalf("got %q", read(t, p))
	}
	if err := DelEnvFile(p, "GOOGLE_GEMINI_BASE_URL"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); got != "# my keys\nGEMINI_API_KEY=def\n" {
		t.Fatalf("got %q", got)
	}
}

// a .env uni-switch made for its key alone still goes when the key does,
// its note with it
func TestMarkEnvFileGoesWhole(t *testing.T) {
	marking(t)
	p := tmpFile(t, ".env", "")
	if err := SetEnvFile(p, KV{Path: "GOOGLE_GEMINI_BASE_URL", Value: "http://127.0.0.1:3425"}); err != nil {
		t.Fatal(err)
	}
	if err := DelEnvFile(p, "GOOGLE_GEMINI_BASE_URL"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("file left: %q", read(t, p))
	}
}

// A line uni-switch leaves as the user had it gets no note, even on the
// gateway, while one uni-switch writes again keeps its own.
func TestMarkOnlyWhatUniSwitchWrites(t *testing.T) {
	marking(t)
	p := tmpFile(t, "config.toml", "model_provider = \"x\"\n[model_providers.x]\nbase_url = \"http://127.0.0.1:3425/v1\"\n")
	if err := SetTOMLTop(p, KV{Path: "model", Value: "gpt-5"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); strings.Contains(got, "uni-switch:") {
		t.Fatalf("a note on the user's own line:\n%s", got)
	}
	if err := SetTOMLTop(p, KV{Path: "openai_base_url", Value: "http://127.0.0.1:3425/x"}); err != nil {
		t.Fatal(err)
	}
	if err := SetTOMLTop(p, KV{Path: "model", Value: "gpt-5.5"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); strings.Count(got, "# uni-switch: ") != 1 || !strings.Contains(got, "# uni-switch: N\nopenai_base_url") {
		t.Fatalf("got:\n%s", got)
	}
}

// A note is a line of its own: a reader that takes the rest of a line for
// its value, as Grok App's does, reads the value as written.
func TestMarkNeverSharesTheValuesLine(t *testing.T) {
	marking(t)
	p := tmpFile(t, "config.toml", "")
	if err := SetTOMLTable(p, "providers.uni-switch", KV{Path: "base_url", Value: "http://127.0.0.1:3425/v1"}); err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(read(t, p), "\n") {
		if strings.Contains(l, "3425") && strings.Contains(l, "#") {
			t.Fatalf("a note beside the value: %q", l)
		}
	}
	if !strings.Contains(read(t, p), "# uni-switch: T providers.uni-switch\nbase_url") {
		t.Fatalf("got:\n%s", read(t, p))
	}
}
