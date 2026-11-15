package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/library"
)

// uni-switch library skill rm --all (#449): with nobody at a terminal to ask,
// it takes nothing unless --yes says so; with --yes, every skill goes, a
// folder of the user's only unlinked.
func TestLibrarySkillRmAll(t *testing.T) {
	h := t.TempDir()
	for k, v := range map[string]string{"HOME": h, "USERPROFILE": h, "XDG_CONFIG_HOME": filepath.Join(h, ".config"), "CLAUDE_CONFIG_DIR": "", "CODEX_HOME": "", "APPDATA": "", "LOCALAPPDATA": ""} {
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
	src := filepath.Join(h, "src")
	for _, n := range []string{"pdf", "xlsx"} {
		write(filepath.Join(src, n, "SKILL.md"), "---\nname: "+n+"\ndescription: "+n+"\n---\n")
	}
	if _, err := library.InstallSkills(src, []string{"pdf", "xlsx"}, []string{"claude"}); err != nil {
		t.Fatal(err)
	}

	err := libraryCmd([]string{"library", "skill", "rm", "--all"})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("with no terminal and no --yes: %v", err)
	}
	if v, _ := library.Read(nil); len(v.Skills) != 2 {
		t.Fatalf("taken without asking: %d left", len(v.Skills))
	}
	if err := libraryCmd([]string{"library", "skill", "rm", "--all", "--nope"}); err == nil {
		t.Error("an unknown flag was taken")
	}

	if err := libraryCmd([]string{"library", "skill", "rm", "--all", "--yes"}); err != nil {
		t.Fatal(err)
	}
	if v, _ := library.Read(nil); len(v.Skills) != 0 {
		t.Errorf("left: %d", len(v.Skills))
	}
	for _, n := range []string{"pdf", "xlsx"} {
		if _, err := os.Lstat(filepath.Join(h, ".claude/skills", n)); !os.IsNotExist(err) {
			t.Errorf("claude still has %s", n)
		}
		if _, err := os.Stat(filepath.Join(src, n, "SKILL.md")); err != nil {
			t.Errorf("the user's %s went with it", n)
		}
	}
	if err := libraryCmd([]string{"library", "skill", "rm", "--all", "--yes"}); err != nil {
		t.Errorf("an empty library: %v", err)
	}
}

// uni-switch library mcp add without agents= gives the server to no agent,
// and says so: it said "every agent already has it" (found writing the
// uni-switch-setup skill). With agents= it names the agents written into.
func TestLibraryMCPAddNoAgents(t *testing.T) {
	h := t.TempDir()
	for k, v := range map[string]string{"HOME": h, "USERPROFILE": h, "XDG_CONFIG_HOME": filepath.Join(h, ".config"), "CLAUDE_CONFIG_DIR": "", "CODEX_HOME": "", "APPDATA": "", "LOCALAPPDATA": ""} {
		t.Setenv(k, v)
	}
	if err := os.MkdirAll(filepath.Join(h, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h, ".claude/settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := func(args ...string) string {
		t.Helper()
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		old := os.Stdout
		os.Stdout = w
		done := make(chan string)
		go func() { b, _ := io.ReadAll(r); done <- string(b) }()
		err = libraryCmd(append([]string{"library", "mcp"}, args...))
		w.Close()
		os.Stdout = old
		s := <-done
		r.Close()
		if err != nil {
			t.Fatalf("library mcp %v: %v", args, err)
		}
		return s
	}
	got := out("add", "ctx7", "https://mcp.example.com/mcp")
	if strings.Contains(got, "every agent") || !strings.Contains(got, "given to no agent yet") || !strings.Contains(got, "uni-switch library mcp agents ctx7") {
		t.Fatalf("added to none: %q", got)
	}
	if got := out("agents", "ctx7", "claude"); !strings.Contains(got, "written into claude") {
		t.Fatalf("given to claude: %q", got)
	}
	if got := out("add", "other", "https://mcp.example.com/other", "agents=claude"); !strings.Contains(got, "written into claude") {
		t.Fatalf("added to claude: %q", got)
	}
}
