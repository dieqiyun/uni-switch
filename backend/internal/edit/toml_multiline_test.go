package edit

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// The config.toml of #253: a top-level array over several lines. A new
// top-level key went in right after the array's first line, inside it
// ("edited TOML is invalid: line 2, column 1: incomplete number").
const issue253 = `notify = [
    "/usr/local/bin/notifier",
    "turn-ended",
]

[model_providers.example]
base_url = "http://127.0.0.1:1/v1"
`

func TestTOMLTopMultilineArray(t *testing.T) {
	p := tmpFile(t, "config.toml", issue253)
	if v, ok := GetTOMLTop(p, "notify"); !ok || v != "[\n    \"/usr/local/bin/notifier\",\n    \"turn-ended\",\n]" {
		t.Fatalf("notify = %q %v", v, ok)
	}
	if err := SetTOMLTop(p, KV{Path: "model", Value: "gpt-6-luna"}, KV{Path: "model_provider", Value: "uni-switch"}); err != nil {
		t.Fatal(err)
	}
	assertTOMLContent(t, p, `notify = [
    "/usr/local/bin/notifier",
    "turn-ended",
]
model = "gpt-6-luna"
model_provider = "uni-switch"

[model_providers.example]
base_url = "http://127.0.0.1:1/v1"
`)
	assertNotify(t, p)
	// replacing a key before the array, and the array itself
	if err := SetTOMLTop(p, KV{Path: "model", Value: "gpt-5.5"}); err != nil {
		t.Fatal(err)
	}
	assertNotify(t, p)
	if err := DelTOMLTop(p, "model_provider", "notify"); err != nil {
		t.Fatal(err)
	}
	assertTOMLContent(t, p, "model = \"gpt-5.5\"\n\n[model_providers.example]\nbase_url = \"http://127.0.0.1:1/v1\"\n")
}

func assertNotify(t *testing.T, p string) {
	t.Helper()
	var doc struct{ Notify []string }
	if err := toml.Unmarshal([]byte(read(t, p)), &doc); err != nil {
		t.Fatal(err)
	}
	if want := []string{"/usr/local/bin/notifier", "turn-ended"}; !reflect.DeepEqual(doc.Notify, want) {
		t.Fatalf("notify = %q", doc.Notify)
	}
}

// Nested arrays, trailing commas, comments inside an array, and brackets
// and `key = value` inside its strings: none of it is a key or a header.
func TestTOMLTopNestedArrays(t *testing.T) {
	const input = `model = "old" # the model
matrix = [
  [1, 2,],
  ["[x]", "a = b"], # a comment with [brackets]
  # model = "not a key"
  [
    3,
  ],
  { name = "[t]" },
]
[mcp]
x = 1
`
	p := tmpFile(t, "config.toml", input)
	if v, ok := GetTOMLTop(p, "model"); !ok || v != "old" {
		t.Fatalf("model = %q %v", v, ok)
	}
	if err := SetTOMLTop(p, KV{Path: "model", Value: "new"}, KV{Path: "openai_base_url", Value: "http://127.0.0.1:1"}); err != nil {
		t.Fatal(err)
	}
	assertTOMLContent(t, p, `model = "new"
matrix = [
  [1, 2,],
  ["[x]", "a = b"], # a comment with [brackets]
  # model = "not a key"
  [
    3,
  ],
  { name = "[t]" },
]
openai_base_url = "http://127.0.0.1:1"
[mcp]
x = 1
`)
	if err := DelTOMLTop(p, "matrix"); err != nil {
		t.Fatal(err)
	}
	assertTOMLContent(t, p, "model = \"new\"\nopenai_base_url = \"http://127.0.0.1:1\"\n[mcp]\nx = 1\n")
}

// Multi-line strings, basic and literal, whose lines look like keys and
// tables: they are the string's text, not the file's.
func TestTOMLTopMultilineStrings(t *testing.T) {
	const input = `instructions = """
model = "inner"
[not.a.table]
"""
raw = '''
[another]
model_provider = x
'''

[t]
x = 1
`
	p := tmpFile(t, "config.toml", input)
	if v, ok := GetTOMLTop(p, "model"); ok {
		t.Fatalf("model read from inside a string: %q", v)
	}
	if v, _ := GetTOMLTop(p, "raw"); v != "[another]\nmodel_provider = x\n" {
		t.Fatalf("raw = %q", v)
	}
	if got, err := TOMLTables(p); err != nil || !reflect.DeepEqual(got, []string{"t"}) {
		t.Fatalf("tables = %v, %v", got, err)
	}
	if err := SetTOMLTop(p, KV{Path: "model", Value: "m"}, KV{Path: "model_provider", Value: "uni-switch"}); err != nil {
		t.Fatal(err)
	}
	assertTOMLContent(t, p, `instructions = """
model = "inner"
[not.a.table]
"""
raw = '''
[another]
model_provider = x
'''
model = "m"
model_provider = "uni-switch"

[t]
x = 1
`)
	if err := DelTOMLTop(p, "instructions", "model_provider"); err != nil {
		t.Fatal(err)
	}
	assertTOMLContent(t, p, "raw = '''\n[another]\nmodel_provider = x\n'''\nmodel = \"m\"\n\n[t]\nx = 1\n")
	if err := SetTOMLKey(p, "t", "y", "z"); err != nil {
		t.Fatal(err)
	}
	if err := SetTOMLTable(p, "u", KV{Path: "k", Value: "v"}); err != nil {
		t.Fatal(err)
	}
	assertTOMLContent(t, p, "raw = '''\n[another]\nmodel_provider = x\n'''\nmodel = \"m\"\n\n[t]\nx = 1\ny = \"z\"\n\n[u]\nk = \"v\"\n")
}

// Atomically puts back every file an edit in several steps touched once a
// step fails: changed bytes, mode, and a file that wasn't there.
func TestAtomically(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	extra := filepath.Join(dir, "models.json")
	os.WriteFile(cfg, []byte(issue253), 0o600)
	fail := errors.New("step two")
	err := Atomically(func() error {
		if err := SetTOMLTable(cfg, "model_providers.uni-switch", KV{Path: "name", Value: "uni-switch"}); err != nil {
			return err
		}
		if err := WriteAtomic(extra, []byte("{}")); err != nil {
			return err
		}
		os.Chmod(cfg, 0o644)
		return fail
	}, cfg, extra)
	if !errors.Is(err, fail) {
		t.Fatalf("err = %v", err)
	}
	if got := read(t, cfg); got != issue253 {
		t.Fatalf("config not put back:\n%s", got)
	}
	if st, _ := os.Stat(cfg); runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
	if _, err := os.Stat(extra); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a file made by the failed edit stays: %v", err)
	}
	// a successful edit is kept
	if err := Atomically(func() error { return SetTOMLTop(cfg, KV{Path: "model", Value: "m"}) }, cfg); err != nil {
		t.Fatal(err)
	}
	if v, _ := GetTOMLTop(cfg, "model"); v != "m" {
		t.Fatalf("model = %q", v)
	}
}
