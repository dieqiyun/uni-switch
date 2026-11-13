package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/library"
)

// uni-switch library skill rm --gone (スライム on Discord): an update says the
// skills its source no longer has apart from failures, and rm --gone takes
// every one of them out, asked first; with nobody at a terminal, only with
// --yes. A skill still in its source stays.
func TestLibrarySkillRmGone(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git isn't installed")
	}
	h := t.TempDir()
	for k, v := range map[string]string{"HOME": h, "USERPROFILE": h, "XDG_CONFIG_HOME": filepath.Join(h, ".config"), "CLAUDE_CONFIG_DIR": "", "CODEX_HOME": "", "APPDATA": "", "LOCALAPPDATA": "",
		"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": filepath.Join(h, "gitconfig")} {
		t.Setenv(k, v)
	}
	write := func(p, s string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(h, ".claude/settings.json"), "")
	bare, work := filepath.Join(h, "skills.git"), filepath.Join(h, "work")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(git, append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "init.defaultBranch=main", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(h, "init", "-q", "--bare", bare)
	run(h, "init", "-q", work)
	for _, n := range []string{"pdf", "docx"} {
		write(filepath.Join(work, "skills", n, "SKILL.md"), "---\nname: "+n+"\ndescription: "+n+"\n---\n")
	}
	push := func() {
		run(work, "add", "-A")
		run(work, "commit", "-q", "-m", "skills")
		run(work, "push", "-q", bare, "HEAD:refs/heads/main")
	}
	push()
	addr := filepath.ToSlash(bare)
	if !strings.HasPrefix(addr, "/") {
		addr = "/" + addr
	}
	if _, err := library.InstallSkills("file://"+addr, []string{"skills/pdf", "skills/docx"}, []string{"claude"}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(work, "skills", "docx")); err != nil {
		t.Fatal(err)
	}
	push()

	if err := libraryCmd([]string{"library", "skill", "update"}); err != nil {
		t.Fatal(err)
	}
	gone := func() (out []string) {
		v, _ := library.Read(nil)
		for _, s := range v.Skills {
			if s.Gone != "" {
				out = append(out, s.Name)
			}
		}
		return out
	}
	if g := gone(); len(g) != 1 || g[0] != "docx" {
		t.Fatalf("marked gone after update: %v", g)
	}
	// one asked for by name is said, not failed
	if err := libraryCmd([]string{"library", "skill", "update", "docx"}); err != nil {
		t.Errorf("update of a skill removed upstream: %v", err)
	}

	err = libraryCmd([]string{"library", "skill", "rm", "--gone"})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("with no terminal and no --yes: %v", err)
	}
	if v, _ := library.Read(nil); len(v.Skills) != 2 {
		t.Fatalf("taken without asking: %d left", len(v.Skills))
	}
	if err := libraryCmd([]string{"library", "skill", "rm", "--gone", "--nope"}); err == nil {
		t.Error("an unknown flag was taken")
	}
	if err := libraryCmd([]string{"library", "skill", "rm", "--gone", "--yes"}); err != nil {
		t.Fatal(err)
	}
	v, _ := library.Read(nil)
	if len(v.Skills) != 1 || v.Skills[0].Name != "pdf" {
		t.Errorf("left: %+v", v.Skills)
	}
	if _, err := os.Lstat(filepath.Join(h, ".claude/skills/docx")); !os.IsNotExist(err) {
		t.Error("claude still has docx")
	}
	if _, err := os.Lstat(filepath.Join(h, ".claude/skills/pdf")); err != nil {
		t.Errorf("pdf, still in its source, went: %v", err)
	}
	if err := libraryCmd([]string{"library", "skill", "rm", "--gone", "--yes"}); err != nil {
		t.Errorf("nothing gone: %v", err)
	}
}
