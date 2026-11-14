//go:build windows

package library

import (
	"os"
	"path/filepath"
	"testing"
)

// #973: a junction, what Windows links with when symlinks aren't allowed,
// is a link to the library's skill: uni-switch's own, and taking it away
// leaves the skill where it is.
func TestJunctionIsALink(t *testing.T) {
	h := sandbox(t)
	skill(t, skillDir("pdf"), "pdf", "Read PDFs")
	p := filepath.Join(h, ".claude/skills/pdf")
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := junction(skillDir("pdf"), p); err != nil {
		t.Fatal(err)
	}
	if to, ok := linkTarget(p); !ok || filepath.Clean(to) != filepath.Clean(skillDir("pdf")) {
		t.Errorf("link target %q %v", to, ok)
	}
	if !ours(p, "pdf") {
		t.Error("uni-switch's junction isn't its own")
	}
	if _, err := os.Stat(filepath.Join(p, "SKILL.md")); err != nil {
		t.Errorf("the skill isn't read through the junction: %v", err)
	}
	if err := unlink(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Errorf("the junction is still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(skillDir("pdf"), "SKILL.md")); err != nil {
		t.Errorf("taking the junction away took the skill: %v", err)
	}
	// a junction can't point at a share; nothing is left behind
	q := filepath.Join(h, "share")
	if junction(`\\server\share\pdf`, q) == nil {
		t.Error("a junction to a share")
	}
	if _, err := os.Lstat(q); !os.IsNotExist(err) {
		t.Errorf("a failed junction left %s: %v", q, err)
	}
}
