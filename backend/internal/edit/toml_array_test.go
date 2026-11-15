package edit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTOMLArrayTableEditsOnlyNamedEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	before := `# global
default_model = "native/a"
[[providers]]
name = "native"
models = [
  "a", # keep
  "b",
]
[providers.headers]
x = "keep"
[[ 'providers' ]]
name = 'uni-switch'
models = ["old"]
[providers.model_overrides."old"]
context_window = 10
note = """
[[providers]]
name = "fake"
"""
# keep the next section's comment
[[providers]]
name = "last"
models = ["z"]
[ui]
theme = "dark"
`
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	block := "[[providers]]\nname = \"uni-switch\"\nmodels = [\"new\"]\n[providers.model_overrides.\"new\"]\ncontext_window = 100\n"
	if err := SetTOMLArrayTable(path, "providers", "name", "uni-switch", block); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	keep := before[:strings.Index(before, "[[ 'providers' ]]")]
	tail := before[strings.Index(before, "# keep the next"):]
	if !strings.HasPrefix(string(got), keep) || !strings.HasSuffix(string(got), tail) || strings.Contains(string(got), "context_window = 10\n") {
		t.Fatalf("neighboring data or the owned subtree was edited incorrectly:\n%s", got)
	}
	if err := DelTOMLArrayTable(path, "providers", "name", "uni-switch"); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if !strings.HasPrefix(string(got), keep) || !strings.HasSuffix(string(got), tail) || strings.Contains(string(got), `name = "uni-switch"`) {
		t.Fatalf("delete changed another entry:\n%s", got)
	}
}

func TestTOMLArrayTableRejectsAmbiguityAndInvalidInput(t *testing.T) {
	for _, input := range []string{
		"[[providers]]\nname = \"uni-switch\"\n[[providers]]\nname = \"uni-switch\"\n",
		"[[providers]]\nname = \"other\"\nname = \"duplicate\"\n",
		"[[providers]]\nname = [\n",
		"providers = [{ name = \"native\" }]\n",
	} {
		t.Run(input, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			os.WriteFile(path, []byte(input), 0o600)
			if err := SetTOMLArrayTable(path, "providers", "name", "uni-switch", "[[providers]]\nname = \"uni-switch\"\n"); err == nil {
				t.Fatal("accepted unsafe input")
			}
			got, _ := os.ReadFile(path)
			if string(got) != input {
				t.Fatalf("failed edit changed the file: %s", got)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := SetTOMLArrayTable(path, "providers", "name", "uni-switch", "[[providers]]\nname = \"foreign\"\n"); err == nil {
		t.Fatal("accepted a mismatching replacement")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid replacement created a file")
	}
}

func TestTOMLTopKeepsCommentAndSpacing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("default_model  =  'native/a'  # chosen by me\n[ui]\ntheme = 'dark'\n"), 0o600)
	if err := SetTOMLTopPreserving(path, KV{Path: "default_model", Value: "uni-switch/group/code"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "default_model  =  \"uni-switch/group/code\"  # chosen by me\n[ui]\ntheme = 'dark'\n" {
		t.Fatalf("lost formatting: %s", got)
	}
}

func TestTOMLArrayTableInterleavedChildren(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	input := "[[providers]]\nname = \"uni-switch\"\n[ui]\ntheme = \"dark\"\n[providers.model_overrides.\"old\"]\ncontext_window = 10\n[[providers]]\nname = \"native\"\n"
	os.WriteFile(path, []byte(input), 0o600)
	if err := SetTOMLArrayTable(path, "providers", "name", "uni-switch", "[[providers]]\nname = \"uni-switch\"\nmodels = [\"new\"]\n"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), "context_window = 10") || !strings.Contains(string(got), "[ui]\ntheme = \"dark\"") || !strings.Contains(string(got), "name = \"native\"") {
		t.Fatalf("interleaved subtree: %s", got)
	}
}

// SetTOMLArrayTables replaces every matching entry in one place, keeps the
// others and what lies between them, and a second run with the same blocks
// leaves the file's bytes and time alone.
func TestTOMLArrayTablesReplaceEveryMatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	before := `default_model = { provider = "custom", model_id = "own" }

[[models]]
provider = "uni-switch"
model_id = "old-1"

[models.pricing]
output = 1.0

[[models]]
provider = "uni-switch"
model_id = "old-2"

[[models]]
provider = "custom"
model_id = "own"
[models.pricing]
output = 2.0

[[models]]
provider = "uni-switch"
model_id = "old-3"
[[command_policy.rules]]
pattern = "rm"
`
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	blocks := []string{
		"[[models]]\nprovider = \"uni-switch\"\nmodel_id = \"new-1\"\n\n[models.pricing]\noutput = 3.0\n",
		"[[models]]\nprovider = \"uni-switch\"\nmodel_id = \"new-2\"\n",
	}
	if err := SetTOMLArrayTables(path, "models", "provider", "uni-switch", blocks); err != nil {
		t.Fatal(err)
	}
	want := `default_model = { provider = "custom", model_id = "own" }

[[models]]
provider = "uni-switch"
model_id = "new-1"

[models.pricing]
output = 3.0

[[models]]
provider = "uni-switch"
model_id = "new-2"

[[models]]
provider = "custom"
model_id = "own"
[models.pricing]
output = 2.0

[[command_policy.rules]]
pattern = "rm"
`
	got, _ := os.ReadFile(path)
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := SetTOMLArrayTables(path, "models", "provider", "uni-switch", blocks); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != want {
		t.Fatalf("a second run changed the file:\n%s", got)
	}
	if st, err := os.Stat(path); err != nil || !st.ModTime().Equal(old) {
		t.Fatalf("a second run wrote the file again: %v %v", st.ModTime(), err)
	}

	if err := SetTOMLArrayTables(path, "models", "provider", "uni-switch", nil); err != nil {
		t.Fatal(err)
	}
	want = `default_model = { provider = "custom", model_id = "own" }

[[models]]
provider = "custom"
model_id = "own"
[models.pricing]
output = 2.0

[[command_policy.rules]]
pattern = "rm"
`
	got, _ = os.ReadFile(path)
	if string(got) != want {
		t.Fatalf("after removal got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTOMLArrayTablesAppendAndRefuse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("x = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetTOMLArrayTables(path, "models", "provider", "uni-switch", []string{"[[models]]\nprovider = \"uni-switch\"\nmodel_id = \"a\"\n"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "x = 1\n\n[[models]]\nprovider = \"uni-switch\"\nmodel_id = \"a\"\n" {
		t.Fatalf("append got %q", got)
	}
	for _, bad := range []string{
		"[[models]]\nprovider = \"other\"\n",
		"[[models]]\nprovider = \"uni-switch\"\n[[models]]\nprovider = \"uni-switch\"\n",
		"[[models]]\nprovider = \"uni-switch\"\n[ui]\nx = 1\n",
		"y = 2\n[[models]]\nprovider = \"uni-switch\"\n",
		"[models]\nprovider = \"uni-switch\"\n",
	} {
		if err := SetTOMLArrayTables(path, "models", "provider", "uni-switch", []string{bad}); err == nil {
			t.Errorf("block %q was taken", bad)
		}
	}
}
