package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func linkIn(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skip("no symlinks here:", err)
	}
}

func stillLink(t *testing.T, p string) {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil || !linkEntry(fi) {
		t.Fatalf("%s is no longer a symlink (%v)", p, err)
	}
}

// library.json, library/instructions.md and an agent's own AGENTS.md
// linked in from a dotfiles repo (#323): saving writes the files they
// point at, and clearing the text empties them, the links staying.
func TestLinkedFilesStayLinked(t *testing.T) {
	h := sandbox(t)
	repo := filepath.Join(h, "dotfiles")
	write(t, filepath.Join(repo, "library.json"), "{}\n")
	write(t, filepath.Join(repo, "instructions.md"), "Old.\n")
	write(t, filepath.Join(repo, "AGENTS.md"), "")
	linkIn(t, filepath.Join(repo, "library.json"), path())
	linkIn(t, filepath.Join(repo, "instructions.md"), setPath(defaultSet))
	cx := filepath.Join(h, ".codex/AGENTS.md")
	linkIn(t, filepath.Join(repo, "AGENTS.md"), cx)

	shared := "Use tabs."
	ok(t)(SaveInstructions(InstructionsChange{Shared: &shared, Agents: []string{"codex"}}))
	for _, p := range []string{path(), setPath(defaultSet), cx} {
		stillLink(t, p)
	}
	if s := read(t, filepath.Join(repo, "instructions.md")); s != "Use tabs.\n" {
		t.Errorf("instructions.md target: %q", s)
	}
	if s := read(t, filepath.Join(repo, "library.json")); !strings.Contains(s, "codex") {
		t.Errorf("library.json target: %s", s)
	}
	if s := read(t, filepath.Join(repo, "AGENTS.md")); !strings.Contains(s, "Use tabs.") {
		t.Errorf("AGENTS.md target: %q", s)
	}

	empty := ""
	ok(t)(SaveInstructions(InstructionsChange{Shared: &empty, Agents: []string{}}))
	for _, p := range []string{path(), setPath(defaultSet), cx} {
		stillLink(t, p)
	}
	if s := read(t, filepath.Join(repo, "instructions.md")); s != "" {
		t.Errorf("cleared instructions.md target kept %q", s)
	}
	if s := read(t, filepath.Join(repo, "AGENTS.md")); s != "" {
		t.Errorf("AGENTS.md target kept %q", s)
	}
}
