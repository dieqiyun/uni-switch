package library

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
)

// wslLinked is sandbox with a running WSL distro, Ubuntu-24.04, whose
// ~/.codex and ~/.claude are links to /mnt/c/Users/me/… (#4502), as
// \\wsl.localhost shows them to Windows: a link nothing can be followed
// through. In the fake distro's root each is a link to /mnt/c/…, which
// isn't there, so — as through \\wsl.localhost — nothing below it can be
// made, written or read, and a mkdir of it says it exists. Windows' C: is
// a folder of its own, with the agents' real folders. probe is what the
// distro answers of the links; it gives back the home as uni-switch opens it
// and C:\Users\me.
func wslLinked(t *testing.T, links string) (home, c string) {
	t.Helper()
	sandbox(t)
	root, drive := t.TempDir(), t.TempDir()
	home = filepath.Join(root, "home", "me")
	c = filepath.Join(drive, "Users", "me")
	write(t, filepath.Join(c, ".codex", "config.toml"), "model = \"gpt-5.5\"\n")
	write(t, filepath.Join(c, ".claude", "settings.json"), "{}\n")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{".codex", ".claude"} {
		if err := os.Symlink("/mnt/c/Users/me/"+d, filepath.Join(home, d)); err != nil {
			t.Skip("no symlinks here:", err)
		}
	}
	t.Cleanup(agent.FakeWSL(map[string]string{
		"Ubuntu-24.04": "home:/home/me\ndir:.codex\ndir:.claude\n" + links + "win:/mnt/c C:\nwin:/mnt/d D:\n",
	}, map[string]string{"Ubuntu-24.04": root}))
	t.Cleanup(agent.FakeWSLDrives(map[string]string{"C:": drive}))
	return home, c
}

// A skill added to Codex and Claude Code in a distro whose ~/.codex and
// ~/.claude lead to Windows' C: lands where they lead (#4502:
// linusxiong's mkdir \\wsl.localhost\…\.codex "Cannot create a file when
// that file already exists"), and so do their instructions; what the
// agents are told is the distro's own path, through the link.
func TestWSLFolderIsALinkToC(t *testing.T) {
	h, c := wslLinked(t, "link:.codex /mnt/c/Users/me/.codex\nlink:.claude /mnt/c/Users/me/.claude/\n")
	src := filepath.Join(home(), "src", "skills")
	skill(t, filepath.Join(src, "pdf"), "pdf", "Read PDFs")
	ok(t)(InstallSkills(src, []string{"pdf"}, []string{wslCodex, wslClaude}))
	for _, d := range []string{".codex", ".claude"} {
		if _, err := os.Stat(filepath.Join(c, d, "skills", "pdf", "SKILL.md")); err != nil {
			t.Errorf("the skill isn't in C:'s %s, where the distro's link leads: %v", d, err)
		}
	}
	if tg := targetByID(wslCodex); tg == nil || tg.Instructions != filepath.Join(c, ".codex", "AGENTS.md") || tg.Skills != filepath.Join(c, ".codex", "skills") {
		t.Fatalf("Codex's target: %+v", tg)
	}
	if fi, err := os.Lstat(filepath.Join(h, ".codex")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the distro's link was replaced: %v %v", fi, err)
	}
}

// A link the distro couldn't resolve is written through by nothing: each
// agent behind one says why, once, and the others are written as ever.
func TestWSLFolderLinkUnresolved(t *testing.T) {
	_, c := wslLinked(t, "link:.codex\nlink:.claude /mnt/c/Users/me/.claude\n")
	src := filepath.Join(home(), "src", "skills")
	skill(t, filepath.Join(src, "pdf"), "pdf", "Read PDFs")
	r, err := InstallSkills(src, []string{"pdf"}, []string{wslCodex, wslClaude})
	if err != nil {
		t.Fatal(err)
	}
	var got []Problem
	for _, p := range r.Problems {
		if p.Agent == wslCodex {
			got = append(got, p)
		}
	}
	if len(got) != 1 || got[0].What != "wsl" || !strings.Contains(got[0].Error, "~/.codex in WSL Ubuntu-24.04 is a link uni-switch couldn't follow") {
		t.Fatalf("Codex behind a link not resolved: %+v", r.Problems)
	}
	if _, err := os.Stat(filepath.Join(c, ".claude", "skills", "pdf", "SKILL.md")); err != nil {
		t.Errorf("Claude Code's skill, beside it: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c, ".codex", "skills")); err == nil {
		t.Error("written to where the link was guessed to lead")
	}
}

// RTK for Claude Code in a distro whose ~/.claude leads to C: (#4502's
// second report): switched on, its folder is made where the link leads,
// and the hook rtk writes there (in the distro, where the link is
// followed) is read back from C:. A link not resolved fails with why.
func TestRTKWSLFolderIsALink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake distro runs sh")
	}
	_, c := wslLinked(t, "link:.claude /mnt/c/Users/me/.claude\n")
	// in the distro the link is followed: rtk runs in a home whose
	// ~/.claude is C:'s
	inside := filepath.Join(t.TempDir(), "home", "me")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(c, ".claude"), filepath.Join(inside, ".claude")); err != nil {
		t.Fatal(err)
	}
	fakeDistroRTK(t, inside)
	if _, err := InstallRTKIn("Ubuntu-24.04"); err != nil {
		t.Fatal(err)
	}
	if _, err := SetRTK(wslClaude, true); err != nil {
		t.Fatalf("RTK for Claude Code behind a link: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(c, ".claude", "settings.json"))
	if !strings.Contains(string(b), "rtk hook claude") {
		t.Fatalf("no hook in C:'s .claude/settings.json: %s", b)
	}
	if a := rtkRow(t, ReadRTK(), wslClaude); !a.On {
		t.Fatalf("switched on, yet the row is off: %+v", a)
	}
}

func TestRTKWSLFolderLinkUnresolved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake distro runs sh")
	}
	wslLinked(t, "link:.claude\n")
	fakeDistroRTK(t, t.TempDir())
	if _, err := InstallRTKIn("Ubuntu-24.04"); err != nil {
		t.Fatal(err)
	}
	_, err := SetRTK(wslClaude, true)
	if err == nil || !strings.Contains(err.Error(), "~/.claude in WSL Ubuntu-24.04 is a link uni-switch couldn't follow") {
		t.Fatalf("RTK behind a link not resolved: %v", err)
	}
}
