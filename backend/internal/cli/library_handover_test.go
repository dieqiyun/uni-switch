package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/library"
)

// uni-switch library skill hand-over / hand-back (tnt1223): a skill kept in
// ~/.agents/skills moves into the library's folder and back again; --all
// takes the library's skills kept there, and nothing else.
func TestLibrarySkillHandOver(t *testing.T) {
	h := t.TempDir()
	for k, v := range map[string]string{"HOME": h, "USERPROFILE": h, "XDG_CONFIG_HOME": filepath.Join(h, ".config"), "CLAUDE_CONFIG_DIR": "", "CODEX_HOME": "", "APPDATA": "", "LOCALAPPDATA": "", "PATH": filepath.Join(h, "bin")} {
		t.Setenv(k, v)
	}
	shared := filepath.Join(h, ".agents/skills", "orca")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "SKILL.md"), []byte("---\nname: orca\ndescription: Orca\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// found, not brought in: --all is the library's alone
	if err := libraryCmd([]string{"library", "skill", "hand-over", "--all"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(shared, "SKILL.md")); err != nil {
		t.Fatal("--all moved a skill the library doesn't have")
	}
	if err := libraryCmd([]string{"library", "skill", "hand-over", "--all", "--yes"}); err == nil {
		t.Error("an unknown flag was taken")
	}
	if _, err := library.ImportSkill("orca"); err != nil {
		t.Fatal(err)
	}
	if err := libraryCmd([]string{"library", "skill", "hand-over", "--all"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(shared); !os.IsNotExist(err) {
		t.Fatalf("still in ~/.agents/skills: %v", err)
	}
	if fi, err := os.Lstat(library.SkillPath("orca")); err != nil || !fi.IsDir() {
		t.Fatalf("not a folder in the library: %v", err)
	}
	if err := libraryCmd([]string{"library", "skill", "hand-back", "orca"}); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(shared); err != nil || !fi.IsDir() {
		t.Fatalf("not back in ~/.agents/skills: %v", err)
	}
	if err := libraryCmd([]string{"library", "skill", "hand-over", "nope"}); err == nil {
		t.Error("a skill that isn't there was handed over")
	}
}
