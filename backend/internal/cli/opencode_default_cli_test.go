package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `uni-switch opencode a/m` wires OpenCode to uni-switch; `uni-switch opencode default`
// puts back the model OpenCode was on before (#1900), as the Agents page's
// Disconnect does. DevEco Code, OpenCode's fork with its own folder, the
// same.
func TestAgentOpenCodeDefaultPutsBackTheModel(t *testing.T) {
	for _, c := range []struct{ id, file string }{
		{"opencode", "opencode/opencode.json"},
		{"deveco", "deveco/deveco.json"},
	} {
		t.Run(c.id, func(t *testing.T) {
			groupsHome(t)
			path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), c.file)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			own := `{
  "$schema": "https://opencode.ai/config.json",
  "model": "native/glm-5",
  "small_model": "native/glm-5-mini",
  "theme": "dark"
}`
			if err := os.WriteFile(path, []byte(own), 0o644); err != nil {
				t.Fatal(err)
			}
			read := func() string {
				b, _ := os.ReadFile(path)
				return string(b)
			}
			if err := run([]string{c.id, "a/m"}); err != nil {
				t.Fatal(err)
			}
			s := read()
			if !strings.Contains(s, `"model": "uni-switch/a/m"`) || !strings.Contains(s, `"provider"`) {
				t.Fatalf("not wired:\n%s", s)
			}
			if err := run([]string{c.id, "default"}); err != nil {
				t.Fatal(err)
			}
			s = read()
			if !strings.Contains(s, `"model": "native/glm-5"`) || !strings.Contains(s, `"small_model": "native/glm-5-mini"`) || strings.Contains(s, `"uni-switch"`) || strings.Contains(s, "baseURL") || !strings.Contains(s, `"theme": "dark"`) {
				t.Fatalf("default didn't put back what was there:\n%s", s)
			}
		})
	}
}
