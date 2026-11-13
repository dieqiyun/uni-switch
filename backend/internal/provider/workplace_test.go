package provider

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// gitCase is a folder and what git finds from there: whether it is in a
// git repository, and the folder that holds the repository's config,
// objects and refs, "" for none.
type gitCase struct{ name, dir, want, repo string }

// gitFixtures lays out under root, by hand, a .git of each kind git's own
// search meets, and gives a folder under each with what git finds there.
func gitFixtures(t *testing.T, root string) []gitCase {
	t.Helper()
	dir := func(p string) string {
		t.Helper()
		p = filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	at := func(p string) string { return filepath.Join(root, filepath.FromSlash(p)) }
	put := func(p, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir(filepath.Dir(p)), filepath.Base(p)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// a repository as git init lays one out, what git looks for in it
	repo := func(git string) {
		put(git+"/HEAD", "ref: refs/heads/main\n")
		dir(git + "/objects")
		dir(git + "/refs/heads")
	}
	repo("repo/.git")
	dir("empty/.git")
	dir("repo/inner/.git")
	// a linked worktree (git worktree add): a HEAD of its own, at a
	// commit, and the rest in the repository's folder, which commondir
	// names
	put("repo/.git/worktrees/wt/HEAD", "7c4b0b9e1d1c4f6a8e2b3d5f6a7b8c9d0e1f2a3b\n")
	put("repo/.git/worktrees/wt/commondir", "../..\n")
	put("repo/.git/worktrees/wt/gitdir", filepath.Join(root, "wt", ".git")+"\n")
	put("wt/.git", "gitdir: "+filepath.Join(root, "repo", ".git", "worktrees", "wt")+"\n")
	// a worktree left behind by its repository
	put("stale/.git", "gitdir: "+filepath.Join(root, "gone", ".git", "worktrees", "stale")+"\n")
	// a submodule: its .git names its folder in the superproject's,
	// relative to itself
	repo("super/.git")
	repo("super/.git/modules/sub")
	put("super/sub/.git", "gitdir: ../.git/modules/sub\n")
	put("super/gone/.git", "gitdir: ../.git/modules/gone\n")
	put("garbage/.git", "not a gitfile\n")
	put("repo/garbage/.git", "not a gitfile\n")
	put("tofile/target", "a file\n")
	put("tofile/.git", "gitdir: target\n")
	// a gitfile over git's 1 MB, whatever it names
	put("big/.git", "gitdir: "+filepath.Join(root, "repo", ".git")+strings.Repeat("\n", 1<<20))
	// everything but a HEAD git takes (one a crash zeroed), and a HEAD
	// with nothing beside it
	put("zeroed/.git/HEAD", "\x00\x00\x00\x00")
	dir("zeroed/.git/objects")
	dir("zeroed/.git/refs")
	put("headonly/.git/HEAD", "ref: refs/heads/main\n")
	// a bare repository kept as a folder's .git (git init --bare .git)
	repo("bare/.git")
	put("bare/.git/config", "[core]\n\trepositoryformatversion = 0\n\tbare = true\n")
	cases := []gitCase{
		{"in a repository", dir("repo/sub"), "yes", at("repo/.git")},
		{"under an empty .git", dir("empty/sub"), "no", ""},
		{"under an empty .git in a repository", dir("repo/inner/sub"), "yes", at("repo/.git")},
		{"a linked worktree", dir("wt/sub"), "yes", at("repo/.git")},
		{"a worktree whose repository has gone", dir("stale/sub"), "no", ""},
		{"a submodule", dir("super/sub/deep"), "yes", at("super/.git/modules/sub")},
		{"a submodule whose folder in the superproject's .git has gone", dir("super/gone/sub"), "no", ""},
		{"a .git file that names no repository", dir("garbage/sub"), "no", ""},
		{"a .git file that names no repository, in a repository", dir("repo/garbage/sub"), "no", ""},
		{"a .git file that names a file", dir("tofile/sub"), "no", ""},
		{"a .git file over 1 MB", dir("big/sub"), "no", ""},
		{"a .git whose HEAD git doesn't take", dir("zeroed/sub"), "no", ""},
		{"a .git with a HEAD alone", dir("headonly/sub"), "no", ""},
		{"a bare repository as .git", dir("bare/sub"), "yes", at("bare/.git")},
	}
	// a HEAD that is a link into refs/ (core.preferSymlinkRefs)
	dir("linked/.git/objects")
	dir("linked/.git/refs/heads")
	switch err := os.Symlink("refs/heads/main", filepath.Join(root, "linked", ".git", "HEAD")); {
	case err == nil:
		cases = append(cases, gitCase{"a HEAD that links into refs/", dir("linked/sub"), "yes", at("linked/.git")})
	case runtime.GOOS != "windows":
		t.Fatal(err)
	}
	return cases
}

// repoFrom is the folder that holds the repository a walk up from dir
// with GitAt comes to, as GitOnDisk's and the library's gitRepoOf's do:
// "" for none.
func repoFrom(t *testing.T, dir string) string {
	t.Helper()
	for d := dir; ; d = filepath.Dir(d) {
		git, common, err := GitAt(filepath.Join(d, ".git"))
		if err != nil {
			t.Fatalf("%s: %v", d, err)
		}
		if git != "" || filepath.Dir(d) == d {
			return common
		}
	}
}

// resolved is path with its links resolved, as git says a folder: "" for
// none.
func resolved(t *testing.T, path string) string {
	t.Helper()
	if path == "" {
		return ""
	}
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Whether a folder is in a git repository is what git finds from there
// (#3675): an empty .git, as the one in /tmp that made every folder under
// it a repository, is passed over, and so is a .git folder whose HEAD git
// doesn't take; a .git file that names no repository is none, as git
// stops there. GitAt names the folder that holds the repository: a linked
// worktree's is the common one, where the library reads its origin.
func TestGitOnDiskIsWhatGitFinds(t *testing.T) {
	for _, c := range gitFixtures(t, t.TempDir()) {
		if got := GitOnDisk(c.dir); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
		if got := repoFrom(t, c.dir); got != c.repo {
			t.Errorf("%s: the repository at %q, want %q", c.name, got, c.repo)
		}
	}
}

// git itself finds what GitOnDisk says, and the repository where GitAt
// says it is (git rev-parse --git-common-dir), from each folder of
// gitFixtures and from repositories git made: a plain one, a linked
// worktree, one left behind, a separate git dir and a bare one kept as
// .git. Neither the machine's git config nor a GIT_ variable of the
// environment the test runs in is read.
func TestGitOnDiskAgreesWithGit(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git isn't installed")
	}
	env := []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + filepath.Join(t.TempDir(), "gitconfig")}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	run := func(dir string, args ...string) (string, error) {
		cmd := exec.Command(git, append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "init.defaultBranch=main", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir, cmd.Env = dir, env
		out, err := cmd.Output()
		if exit := (*exec.ExitError)(nil); errors.As(err, &exit) {
			err = fmt.Errorf("%w: %s", err, exit.Stderr)
		}
		return string(out), err
	}
	must := func(dir string, args ...string) {
		t.Helper()
		if _, err := run(dir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	root := t.TempDir()
	cases := gitFixtures(t, filepath.Join(root, "by-hand"))
	made := filepath.Join(root, "made")
	repo := filepath.Join(made, "repo")
	if err := os.MkdirAll(filepath.Join(made, "modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	must(made, "init", "-q", repo)
	must(repo, "commit", "-q", "--allow-empty", "-m", "first")
	must(repo, "worktree", "add", "-q", "--detach", filepath.Join(made, "wt"))
	must(repo, "worktree", "add", "-q", "--detach", filepath.Join(made, "stale"))
	if err := os.RemoveAll(filepath.Join(repo, ".git", "worktrees", "stale")); err != nil {
		t.Fatal(err)
	}
	must(made, "init", "-q", "--separate-git-dir", filepath.Join(made, "modules", "sep"), filepath.Join(made, "sep"))
	must(made, "init", "-q", "--bare", filepath.Join(made, "bare", ".git"))
	for _, c := range []gitCase{
		{"git init", "repo", "yes", "repo/.git"},
		{"git worktree add", "wt", "yes", "repo/.git"},
		{"git worktree add, its record in the repository removed", "stale", "no", ""},
		{"git init --separate-git-dir", "sep", "yes", "modules/sep"},
		{"git init --bare .git", "bare", "yes", "bare/.git"},
	} {
		c.dir = filepath.Join(made, c.dir, "sub")
		if err := os.MkdirAll(c.dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if c.repo != "" {
			c.repo = filepath.Join(made, filepath.FromSlash(c.repo))
		}
		cases = append(cases, c)
	}
	for _, c := range cases {
		found, repo := "yes", ""
		out, err := run(c.dir, "rev-parse", "--git-common-dir")
		if exit := (*exec.ExitError)(nil); errors.As(err, &exit) && exit.ExitCode() == 128 {
			found = "no"
		} else if err != nil {
			t.Fatalf("%s: git rev-parse: %v", c.name, err)
		} else {
			// relative to the folder it was asked in, when it isn't whole
			if repo = strings.TrimSpace(out); !filepath.IsAbs(repo) {
				repo = filepath.Join(c.dir, repo)
			}
			repo = resolved(t, repo)
		}
		if found != c.want || repo != resolved(t, c.repo) {
			t.Errorf("%s: git finds %q at %q, want %q at %q", c.name, found, repo, c.want, c.repo)
		}
		if got := GitOnDisk(c.dir); got != found {
			t.Errorf("%s: %q, git finds %q", c.name, got, found)
		}
		if got := resolved(t, repoFrom(t, c.dir)); got != repo {
			t.Errorf("%s: the repository at %q, git finds it at %q", c.name, got, repo)
		}
	}
}

// A .git that can't be read is unknown, never "no" (nor the "yes" any
// .git was before): uni-switch may not be the user it belongs to, through a
// shared uni-switch, and can't tell an empty one from a repository it isn't
// let into.
func TestGitOnDiskUnreadIsUnknown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a folder isn't locked by its mode")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads a folder whatever its mode")
	}
	root := t.TempDir()
	gitFixtures(t, root)
	for _, c := range []struct{ name, dir string }{
		{"a .git folder it can't open", "repo/sub"},
		{"a worktree's .git file it can't read", "wt/sub"},
	} {
		dir := filepath.Join(root, filepath.FromSlash(c.dir))
		locked := filepath.Join(filepath.Dir(dir), ".git")
		fi, err := os.Stat(locked)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		got := GitOnDisk(dir)
		if err := os.Chmod(locked, fi.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
		if got != "" {
			t.Errorf("%s: %q, want unknown", c.name, got)
		}
	}
}
