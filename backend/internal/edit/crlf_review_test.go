package edit

import (
	"strings"
	"testing"
)

func bareLF(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' && (i == 0 || s[i-1] != '\r') {
			return true
		}
	}
	return false
}

// LF files stay LF byte for byte through every editor.
func TestReviewLFUntouched(t *testing.T) {
	e := tmpFile(t, ".env", "A=1\nB=2\n")
	SetEnvFile(e, KV{Path: "C", Value: "3"})
	DelEnvFile(e, "A")
	if got := read(t, e); got != "B=2\nC=3\n" {
		t.Fatalf("env %q", got)
	}
	tm := tmpFile(t, "c.toml", "top = 1\n[a]\nk = \"v\"\n")
	SetTOMLTop(tm, KV{Path: "x", Value: "y"})
	SetTOMLKey(tm, "a", "n", "m")
	SetTOMLTable(tm, "b", KV{Path: "q", Value: "1"})
	if got := read(t, tm); strings.Contains(got, "\r") {
		t.Fatalf("toml %q", got)
	}
	y := tmpFile(t, "c.yaml", "A: 1\nB: 2\n")
	SetYAMLTop(y, KV{Path: "C", Value: "3"})
	DelYAMLTop(y, "A")
	if got := read(t, y); strings.Contains(got, "\r") {
		t.Fatalf("yaml_top %q", got)
	}
	j := tmpFile(t, "s.json", "{\n  \"a\": 1\n}\n")
	SetJSON(j, KV{Path: "b.c", Value: []string{"x"}})
	DelJSON(j, "a")
	if got := read(t, j); strings.Contains(got, "\r") {
		t.Fatalf("json %q", got)
	}
	y2 := tmpFile(t, "d.yaml", "a: 1\nb:\n  c: 2\n")
	SetYAML(y2, KV{Path: "b.d", Value: 3})
	EditYAMLStrings(y2, []string{"a"}, func(s string) string { return s })
	if got := read(t, y2); strings.Contains(got, "\r") {
		t.Fatalf("yaml %q", got)
	}
}

// CRLF files with no trailing line break, a multi-line TOML string and a
// YAML block scalar keep \r\n everywhere and still parse to the same value.
func TestReviewCRLFEdges(t *testing.T) {
	e := tmpFile(t, ".env", "A=1\r\nB=2")
	if err := SetEnvFile(e, KV{Path: "C", Value: "3"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, e); bareLF(got) {
		t.Fatalf("env %q", got)
	}
	tm := tmpFile(t, "c.toml", "s = \"\"\"\r\nline1\r\nline2\"\"\"\r\n[a]\r\nk = 1\r\n")
	if err := SetTOMLKey(tm, "a", "n", "m"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, tm); bareLF(got) || !strings.Contains(got, "line1\r\nline2") {
		t.Fatalf("toml %q", got)
	}
	y := tmpFile(t, "c.yaml", "a: |\r\n  one\r\n  two\r\nb: 1\r\n")
	if err := SetYAML(y, KV{Path: "c", Value: 2}); err != nil {
		t.Fatal(err)
	}
	got := read(t, y)
	if bareLF(got) {
		t.Fatalf("yaml %q", got)
	}
	if v, _ := GetYAML(y, "a"); v != "one\ntwo\n" {
		t.Fatalf("block scalar now %q (file %q)", v, got)
	}
	j := tmpFile(t, "s.json", "{\r\n  \"a\": 1,\r\n  \"b\": 2\r\n}\r\n")
	if err := DelJSON(j, "a"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, j); bareLF(got) {
		t.Fatalf("json del %q", got)
	}
}
