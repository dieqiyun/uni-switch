package library

import (
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

// The hub turned on for OpenCode and off again (chenxiang0010 on Discord,
// Windows) left opencode.jsonc's "mcp" ending in a comma with nothing
// before it, which OpenCode calls invalid. The file had the Library's
// server as the only one, with a trailing comma, as OpenCode and editors
// allow. Each toggle leaves a file OpenCode reads, LF and CRLF.
func TestHubToggleKeepsOpenCodeJSONCValid(t *testing.T) {
	for _, eol := range []string{"\n", "\r\n"} {
		t.Run(map[string]string{"\n": "lf", "\r\n": "crlf"}[eol], func(t *testing.T) {
			hubSandbox(t)
			p := targetByID("opencode").MCP.Path
			ok(t)(SaveServer("", Server{Name: "fs", Transport: "stdio", Command: "npx", Args: []string{"-y", "fs"}, Agents: []string{"opencode"}}))
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			// as the user keeps it: the server's member ends in a comma,
			// and so does mcp's, with a comment of theirs and their line
			// ends
			s := strings.Replace(string(b), "\"mcp\": {", "// mine\n  \"mcp\": {", 1)
			i := strings.Index(s, "\"fs\": {")
			j := i + strings.Index(s[i:], "\n    }") + len("\n    }")
			s = s[:j] + "," + s[j:]
			s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", eol)
			if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
				t.Fatal(err)
			}
			if !edit.ValidJSONC([]byte(s)) || !strings.Contains(s, "},"+eol+"  }") {
				t.Fatalf("fixture:\n%s", s)
			}
			check := func(when string, want ...string) {
				t.Helper()
				b, _ := os.ReadFile(p)
				if !edit.ValidJSONC(b) {
					t.Fatalf("%s: OpenCode can't read it:\n%s", when, b)
				}
				for _, w := range want {
					if !strings.Contains(string(b), w) {
						t.Fatalf("%s: no %q in:\n%s", when, w, b)
					}
				}
			}
			for range 2 {
				ok(t)(SetMCPHub(true))
				check("hub on", `"uni-switch"`, "// mine")
				ok(t)(SetMCPHub(false))
				check("hub off", `"fs"`, "// mine")
			}
		})
	}
}
