package edit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestYAMLNestedRejectsInvalidInput(t *testing.T) {
	for _, c := range []struct{ name, in string }{
		{"malformed first document", "model:\n  default: [\n"},
		{"multiple documents", "model:\n  default: old\n---\nimportant: keep-me\n"},
		{"malformed second document", "model:\n  default: old\n---\nimportant: [\n"},
		{"nonempty third document", "model:\n  default: old\n---\nnull\n---\nimportant: keep-me\n"},
		{"malformed third document", "model:\n  default: old\n---\nnull\n---\nimportant: [\n"},
		{"invalid null tag", "model:\n  default: old\n---\n!!null invalid\n"},
		{"duplicate root key", "model: {default: old}\nmodel: {default: other}\n"},
		{"duplicate nested key", "model:\n  default: old\n  \"default\": other\n"},
		{"undefined alias", "model:\n  default: *missing\n"},
		{"sequence root", "- model: {default: old}\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, op := range []struct {
				name string
				fn   func(string) error
			}{
				{"set", func(p string) error { return SetYAML(p, KV{"model.default", "new"}) }},
				{"delete", func(p string) error { return DelYAML(p, "model.default") }},
				{"edit strings", func(p string) error {
					return EditYAMLStrings(p, []string{"model"}, func(s string) string {
						return strings.ReplaceAll(s, "old", "new")
					})
				}},
			} {
				t.Run(op.name, func(t *testing.T) {
					p := tmpFile(t, "config.yaml", c.in)
					if err := op.fn(p); err == nil || !strings.Contains(err.Error(), p) {
						t.Fatalf("error = %v, want an error with the file path", err)
					}
					if got := read(t, p); got != c.in {
						t.Fatalf("failed edit changed file: %q", got)
					}
				})
			}
		})
	}
}

func TestYAMLNestedReadsStayLenient(t *testing.T) {
	// Parsing nodes need not expand aliases, even when decoding values would
	// exceed yaml.v3's expansion limit.
	const aliases = "a: &a [0,0,0,0,0,0,0,0,0]\n" +
		"b: &b [*a,*a,*a,*a,*a,*a,*a,*a,*a]\n" +
		"c: &c [*b,*b,*b,*b,*b,*b,*b,*b,*b]\n" +
		"d: &d [*c,*c,*c,*c,*c,*c,*c,*c,*c]\n" +
		"e: &e [*d,*d,*d,*d,*d,*d,*d,*d,*d]\n" +
		"f: &f [*e,*e,*e,*e,*e,*e,*e,*e,*e]\n" +
		"g: &g [*f,*f,*f,*f,*f,*f,*f,*f,*f]\n" +
		"h: &h [*g,*g,*g,*g,*g,*g,*g,*g,*g]\n" +
		"i: &i [*h,*h,*h,*h,*h,*h,*h,*h,*h]\n"
	for _, c := range []struct{ name, extra string }{
		{"duplicate unrelated key", "other:\n  key: first\n  key: second\n"},
		{"sequence key", "? [a, b]\n: other\n"},
		{"mapping key", "? {a: b}\n: other\n"},
		{"invalid tagged scalar", "other: !!int abc\n"},
		{"heavy aliases", aliases},
		{"multiple documents", "---\nother: keep-me\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := "model:\n  default: old\nmodels: [old, fallback]\n" + c.extra
			p := tmpFile(t, "config.yaml", in)
			if got, ok := GetYAML(p, "model.default"); !ok || got != "old" {
				t.Errorf("GetYAML = %q, %v", got, ok)
			}
			if got := GetYAMLMap(p, "model"); len(got) != 1 || got["default"] != "old" {
				t.Errorf("GetYAMLMap = %#v", got)
			}
			if got := GetYAMLList(p, "models"); len(got) != 2 || got[0] != "old" || got[1] != "fallback" {
				t.Errorf("GetYAMLList = %#v", got)
			}
			if got, ok := GetYAMLText(p, "models"); !ok || got != "[old, fallback]" {
				t.Errorf("GetYAMLText = %q, %v", got, ok)
			}
			// Read compatibility must not let an unsafe file be edited.
			for _, op := range []struct {
				name string
				fn   func(string) error
			}{
				{"set", func(p string) error { return SetYAML(p, KV{"model.default", "new"}) }},
				{"delete", func(p string) error { return DelYAML(p, "model.default") }},
				{"edit strings", func(p string) error {
					return EditYAMLStrings(p, []string{"model"}, func(s string) string {
						return strings.ReplaceAll(s, "old", "new")
					})
				}},
			} {
				if err := op.fn(p); err == nil || !strings.Contains(err.Error(), p) {
					t.Fatalf("%s: error = %v, want an error with the file path", op.name, err)
				}
				if got := read(t, p); got != in {
					t.Fatalf("%s: failed edit changed file: %q", op.name, got)
				}
			}
		})
	}
}

func TestYAMLNestedAcceptsNullTrailingDocuments(t *testing.T) {
	for _, c := range []struct{ name, tail string }{
		{"empty", "---\n"},
		{"comment only", "---\n# empty\n...\n"},
		{"null", "---\nnull\n"},
		{"tilde", "---\n~\n"},
		{"uppercase null", "---\nNULL\n"},
		{"several", "---\n---\nnull\n---\n~\n"},
		{"explicit end", "...\n---\nnull\n...\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, op := range []struct {
				name, want string
				fn         func(string) error
			}{
				{"set", "model:\n  default: new\nother: keep\n", func(p string) error {
					return SetYAML(p, KV{"model.default", "new"})
				}},
				{"delete", "model: {}\nother: keep\n", func(p string) error {
					return DelYAML(p, "model.default")
				}},
				{"edit strings", "model:\n  default: new\nother: keep\n", func(p string) error {
					return EditYAMLStrings(p, []string{"model"}, func(s string) string {
						return strings.ReplaceAll(s, "old", "new")
					})
				}},
			} {
				t.Run(op.name, func(t *testing.T) {
					p := tmpFile(t, "config.yaml", "model:\n  default: old\nother: keep\n"+c.tail)
					if err := op.fn(p); err != nil {
						t.Fatal(err)
					}
					if got := read(t, p); got != op.want {
						t.Fatalf("got %q, want %q", got, op.want)
					}
				})
			}
		})
	}
}

func TestYAMLNestedValidatesBeforeWriting(t *testing.T) {
	for _, c := range []struct {
		name, in string
		fn       func(string) error
	}{
		{"replace anchor", "model:\n  default: &chosen old\nother: *chosen\n", func(p string) error {
			return SetYAML(p, KV{"model.default", "new"})
		}},
		{"delete anchor", "model:\n  default: &chosen old\nother: *chosen\n", func(p string) error {
			return DelYAML(p, "model.default")
		}},
		{"replace anchor parent", "model:\n  default: &chosen old\nother: *chosen\n", func(p string) error {
			return SetYAML(p, KV{"model", "new"})
		}},
		{"duplicate edited key", "model:\n  default: old\n  other: keep\n", func(p string) error {
			return EditYAMLStrings(p, []string{"model"}, func(s string) string {
				if s == "other" {
					return "default"
				}
				return s
			})
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := tmpFile(t, "config.yaml", c.in)
			if err := c.fn(p); err == nil || !strings.Contains(err.Error(), p) {
				t.Fatalf("error = %v, want an error with the file path", err)
			}
			if got := read(t, p); got != c.in {
				t.Fatalf("failed edit changed file: %q", got)
			}
		})
	}
}

func TestYAMLNestedValidEditsWithAliases(t *testing.T) {
	p := tmpFile(t, "config.yaml", "# models\nmodel:\n  default: &chosen old # pinned\nother: *chosen\n")
	if err := SetYAML(p, KV{"model.effort", "high"}); err != nil {
		t.Fatal(err)
	}
	if err := EditYAMLStrings(p, []string{"model.default"}, func(string) string { return "new" }); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := yaml.Unmarshal([]byte(read(t, p)), &got); err != nil {
		t.Fatal(err)
	}
	model := got["model"].(map[string]any)
	if model["default"] != "new" || model["effort"] != "high" || got["other"] != "new" {
		t.Fatalf("values or aliases changed: %#v", got)
	}
	for _, comment := range []string{"# models", "# pinned"} {
		if !strings.Contains(read(t, p), comment) {
			t.Fatalf("lost comment %q", comment)
		}
	}
	// Removing an anchor and all its uses in one edit leaves valid YAML.
	if err := DelYAML(p, "model.default", "other"); err != nil {
		t.Fatal(err)
	}
	if content := read(t, p); content != "# models\nmodel:\n  effort: high\n" {
		t.Fatalf("after deleting anchor and alias: %q", content)
	}
}

func TestYAMLNestedAcceptsFlowRoot(t *testing.T) {
	p := tmpFile(t, "config.yaml", "{model: {default: old}, other: keep}\n")
	if err := SetYAML(p, KV{"model.default", "new"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); got != "{model: {default: new}, other: keep}\n" {
		t.Fatalf("flow mapping changed: %q", got)
	}
}

func TestYAMLTextRejectsInvalidInput(t *testing.T) {
	for _, c := range []struct{ name, text string }{
		{"multiple documents", "old\n---\nkeep-me\n"},
		{"duplicate key", "default: old\n\"default\": other\n"},
		{"undefined alias", "*missing\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			const in = "model:\n  default: old\n"
			p := tmpFile(t, "config.yaml", in)
			if err := SetYAML(p, KV{"model.default", YAMLText(c.text)}); err == nil ||
				!strings.Contains(err.Error(), p) || !strings.Contains(err.Error(), "model.default") {
				t.Fatalf("error = %v, want an error with the file and key paths", err)
			}
			if got := read(t, p); got != in {
				t.Fatalf("failed edit changed file: %q", got)
			}
			text, err := EditYAMLTextStrings(YAMLText(c.text), func(s string) string {
				return strings.ReplaceAll(s, "old", "new")
			})
			if err == nil || text != YAMLText(c.text) {
				t.Fatalf("edited invalid YAML: %q, error = %v", text, err)
			}
		})
	}
}

func TestYAMLTextValidatesEditedOutput(t *testing.T) {
	const in YAMLText = "default: old\nother: keep\n"
	got, err := EditYAMLTextStrings(in, func(s string) string {
		if s == "other" {
			return "default"
		}
		return s
	})
	if err == nil || got != in {
		t.Fatalf("edited YAML has duplicate keys: %q, error = %v", got, err)
	}
}

func TestYAMLTextAcceptsStructuredValues(t *testing.T) {
	p := tmpFile(t, "config.yaml", "model:\n  existing: keep\n")
	text, err := EditYAMLTextStrings("[old, fallback]", func(s string) string {
		return strings.ReplaceAll(s, "old", "new")
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := SetYAML(p, KV{"model.default", text}, KV{"model.effort", YAMLText("high")}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); got != "model:\n  existing: keep\n  default: [new, fallback]\n  effort: high\n" {
		t.Fatalf("structured values changed: %q", got)
	}
}

func TestJSONToYAMLRejectsDuplicateKeys(t *testing.T) {
	for _, c := range []struct{ name, in string }{
		{"root", `{"model": "old", "model": "other"}`},
		{"nested", `{"providers": {"mine": {"baseUrl": "old", "baseUrl": "other"}}}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, target := range []struct {
				name   string
				exists bool
			}{
				{"missing target", false},
				{"existing target", true},
			} {
				t.Run(target.name, func(t *testing.T) {
					src := tmpFile(t, "models.json", c.in)
					dst := filepath.Join(filepath.Dir(src), "models.yml")
					const before = "model: keep\n"
					if target.exists {
						if err := os.WriteFile(dst, []byte(before), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					if err := JSONToYAML(src, dst); err == nil || !strings.Contains(err.Error(), dst) {
						t.Fatalf("error = %v, want an error with the target path", err)
					}
					if target.exists {
						if got := read(t, dst); got != before {
							t.Fatalf("failed conversion changed target: %q", got)
						}
					} else if _, err := os.Stat(dst); !os.IsNotExist(err) {
						t.Fatalf("failed conversion created target: %v", err)
					}
				})
			}
		})
	}
}
