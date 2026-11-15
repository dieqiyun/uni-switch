package library

import (
	"encoding/json"
	"os"
	"path/filepath"

	"testing"
)

// setUpLibrary fills a sandbox's library: two sets, the second on, an
// agent's own additions, a server with a key, a skill kept in the library
// and one linked to a folder of the user's.
func setUpLibrary(t *testing.T, h string) {
	t.Helper()
	first, work := "Use tabs.", "Company rules."
	ok(t)(SaveInstructions(InstructionsChange{Shared: &first, Agents: []string{"claude", "codex"}}))
	ok(t)(SaveInstructions(InstructionsChange{Create: &InstrSet{ID: "work", Name: "Work"}, Texts: map[string]*string{"work": &work}, Activate: "work"}))
	extra := "Answer in French."
	ok(t)(SaveInstructions(InstructionsChange{Extra: map[string]*string{"codex": &extra}}))
	ok(t)(SaveServer("", Server{Name: "fs", Transport: "stdio", Command: "npx", Args: []string{"-y", "@mcp/fs"},
		Env: map[string]string{"GITHUB_TOKEN": "ghp-secret", "MODE": "ro"}, Agents: []string{"claude", "codex"}}))
	skill(t, filepath.Join(h, "src/pdf"), "pdf", "Read PDFs")
	os.Chmod(filepath.Join(h, "src/pdf/scripts/run.sh"), 0o755)
	ok(t)(InstallSkills(filepath.Join(h, "src"), []string{"pdf"}, []string{"claude"}))
}

func keysOf[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
