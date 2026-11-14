package edit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Turning the Library's MCP hub on for OpenCode and off again (chenxiang0010
// on Discord, Windows) left opencode.jsonc's "mcp" as {,}: the member taken
// out was the only one and had a trailing comma, which jsonc blanks before
// the delete looked for it, so the comma stayed with nothing before it, and
// OpenCode's jsonc-parser (allowTrailingComma) called the file invalid. Each
// order the sync can add and take out entries in, from each shape of file,
// leaves a file OpenCode reads.
func TestHubToggleLeavesValidJSONC(t *testing.T) {
	hub := map[string]any{"type": "remote", "url": "http://127.0.0.1:3425/mcp/uni-switch/hub/opencode", "enabled": true}
	srv := map[string]any{"type": "local", "command": []string{"npx", "x"}, "enabled": true}
	fixtures := map[string]string{
		"empty_mcp":         "{\n  \"mcp\": {}\n}\n",
		"no_mcp":            "{\n  \"$schema\": \"https://opencode.ai/config.json\"\n}\n",
		"one":               "{\n  \"mcp\": {\n    \"x\": {\"type\": \"local\"}\n  }\n}\n",
		"one_trailing":      "{\n  \"mcp\": {\n    \"x\": {\"type\": \"local\"},\n  },\n}\n",
		"one_trailing_crlf": "{\r\n  \"mcp\": {\r\n    \"x\": {\"type\": \"local\"},\r\n  },\r\n}\r\n",
		"one_crlf":          "{\r\n  \"mcp\": {\r\n    \"x\": {\"type\": \"local\"}\r\n  }\r\n}\r\n",
		"two_trailing":      "{\n  \"mcp\": {\n    \"x\": {\"type\": \"local\"},\n    \"y\": {\"type\": \"local\"},\n  }\n}\n",
		"comment_only":      "{\n  \"mcp\": {\n    // none yet\n  }\n}\n",
		"comment_after":     "{\n  \"mcp\": {\n    \"x\": {\"type\": \"local\"} // mine\n  }\n}\n",
		"comma_after_note":  "{\n  \"mcp\": {\n    \"x\": {\"type\": \"local\"} // mine\n    ,\n  }\n}\n",
		"tabs_trailing":     "{\n\t\"mcp\": {\n\t\t\"x\": {\"type\": \"local\"},\n\t},\n}\n",
		"compact_trailing":  `{"mcp":{"x":{"type":"local"},}}`,
	}
	seqs := map[string][]string{
		"hub_on_off":           {"+uni-switch", "-uni-switch"},
		"add_first_del_first":  {"+uni-switch", "-x", "-uni-switch", "+x"},
		"add_first_add_first":  {"+uni-switch", "-x", "+x", "-uni-switch"},
		"del_first_del_first":  {"-x", "+uni-switch", "-uni-switch", "+x"},
		"del_first_add_first":  {"-x", "+uni-switch", "+x", "-uni-switch"},
		"two_servers":          {"+uni-switch", "-x", "-y", "-uni-switch", "+x", "+y"},
		"two_servers_add_back": {"-x", "-y", "+uni-switch", "+x", "+y", "-uni-switch"},
	}
	for fn, fx := range fixtures {
		for sn, seq := range seqs {
			t.Run(fn+"/"+sn, func(t *testing.T) {
				p := filepath.Join(t.TempDir(), "opencode.jsonc")
				if err := os.WriteFile(p, []byte(fx), 0o644); err != nil {
					t.Fatal(err)
				}
				for i, st := range seq {
					name := st[1:]
					var err error
					if st[0] == '+' {
						v := srv
						if name == "uni-switch" {
							v = hub
						}
						err = SetJSON(p, KV{Path: "mcp." + name, Value: v})
					} else {
						err = DelJSON(p, "mcp."+name)
					}
					b, _ := os.ReadFile(p)
					if err != nil || !ValidJSONC(b) {
						t.Fatalf("step %d %s: err=%v, file OpenCode can't read:\n%s", i, st, err, strings.ReplaceAll(string(b), "\r", `\r`))
					}
					if _, ok := GetJSON(p, "mcp."+name); ok != (st[0] == '+') {
						t.Fatalf("step %d %s: mcp.%s there=%v", i, st, name, ok)
					}
					if strings.Contains(fx, "\r\n") && strings.Count(string(b), "\n") != strings.Count(string(b), "\r\n") {
						t.Fatalf("step %d %s: CRLF file got a bare \\n:\n%q", i, st, b)
					}
					if strings.Contains(fx, "// ") && !strings.Contains(string(b), "// ") {
						t.Fatalf("step %d %s: the comment went:\n%s", i, st, b)
					}
				}
			})
		}
	}
}

// The reporter's file, as the delete leaves it.
func TestDelOnlyMemberTakesItsTrailingComma(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"{\n  \"mcp\": {\n    \"uni-switch\": {\n      \"type\": \"remote\"\n    },\n  },\n}\n", "{\n  \"mcp\": {},\n}\n"},
		{"{\r\n  \"mcp\": {\r\n    \"uni-switch\": {\"type\": \"remote\"},\r\n  }\r\n}\r\n", "{\r\n  \"mcp\": {}\r\n}\r\n"},
		{"{\n  \"mcp\": {\n    \"a\": 1,\n    \"uni-switch\": 2,\n  }\n}\n", "{\n  \"mcp\": {\n    \"a\": 1\n  }\n}\n"},
		{`{"mcp":{"uni-switch":2,}}`, `{"mcp":{}}`},
	} {
		p := filepath.Join(t.TempDir(), "opencode.jsonc")
		os.WriteFile(p, []byte(c.in), 0o644)
		if err := DelJSON(p, "mcp.uni-switch"); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(p); string(b) != c.want {
			t.Errorf("DelJSON(%q):\n got %q\nwant %q", c.in, b, c.want)
		}
	}
}

// An object of comments only gets its first member with no comma after it.
func TestInsertIntoCommentOnlyObjectNoTrailingComma(t *testing.T) {
	p := filepath.Join(t.TempDir(), "opencode.jsonc")
	os.WriteFile(p, []byte("{\n  \"mcp\": {\n    // none yet\n  }\n}\n"), 0o644)
	if err := SetJSON(p, KV{Path: "mcp.uni-switch", Value: 1}); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"mcp\": {\n    \"uni-switch\": 1\n    // none yet\n  }\n}\n"
	if b, _ := os.ReadFile(p); string(b) != want {
		t.Errorf("got %q\nwant %q", b, want)
	}
}

// A file an older uni-switch left with {,} is mended by the next edit of it.
func TestLoneCommaMendedOnNextEdit(t *testing.T) {
	p := filepath.Join(t.TempDir(), "opencode.jsonc")
	os.WriteFile(p, []byte("{\n  \"mcp\": {\n,\n  },\n  \"x\": [\n  ,]\n}\n"), 0o644)
	if err := SetJSON(p, KV{Path: "mcp.uni-switch", Value: 1}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); !ValidJSONC(b) {
		t.Errorf("still invalid:\n%s", b)
	}
	os.WriteFile(p, []byte("{\n  \"mcp\": {\n,\n  },\n  \"uni-switch\": 1\n}\n"), 0o644)
	if err := DelJSON(p, "uni-switch"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); !ValidJSONC(b) {
		t.Errorf("still invalid:\n%s", b)
	}
	out, err := PatchJSON([]byte("{\"a\": {,}}"), []KV{{Path: "b", Value: 1}}, nil)
	if err != nil || !ValidJSONC(out) {
		t.Errorf("PatchJSON: %v %s", err, out)
	}
}

// What jsonc-parser with allowTrailingComma reads, and what it calls invalid.
func TestValidJSONC(t *testing.T) {
	for in, want := range map[string]bool{
		`{}`:                      true,
		`{"a":1,}`:                true,
		"{\"a\":1 // c\n,}":       true,
		`[1,2,]`:                  true,
		`{"a":"x,}","b":[",]"]}`:  true,
		"// ,}\n{\"a\":1}":        true,
		"/* {,} */{\"a\":[1,]}":   true,
		"\xef\xbb\xbf{\"a\":1,}":  true,
		`{,}`:                     false,
		"{\n,\n}":                 false,
		`[,]`:                     false,
		`{"a":1,,}`:               false,
		`{"a":,}`:                 false,
		"{\"a\":1 /* never ends ": false,
		`{"a":1`:                  false,
	} {
		if got := ValidJSONC([]byte(in)); got != want {
			t.Errorf("ValidJSONC(%q) = %v, want %v", in, got, want)
		}
	}
}

// An edit that would turn a file the app reads into one it can't is
// refused, and nothing is written.
func TestInvalidEditNotWritten(t *testing.T) {
	if _, err := checkJSONC([]byte("{,}"), []byte("{}")); !errors.Is(err, errEditInvalid) {
		t.Errorf("checkJSONC on {,} from {}: %v", err)
	}
	if out, err := checkJSONC([]byte(`{"a":1,}`), []byte("{}")); err != nil || string(out) != `{"a":1,}` {
		t.Errorf("a trailing comma after a value is not refused: %v", err)
	}
}
