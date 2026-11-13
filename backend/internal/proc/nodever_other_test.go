//go:build !windows

package proc

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// nvmHome lays out nvm as it is on disk: versions/node/vX.Y.Z/bin/node
// for each of versions, and alias/<name> holding what `nvm alias` writes
// (the target and a newline), lts/<name> among them as `nvm ls-remote`
// writes them. It gives the home, with NVM_DIR unset and PATH launchd's.
func nvmHome(t *testing.T, versions []string, aliases map[string]string) (home, nvm string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("NVM_DIR", "")
	t.Setenv("FNM_DIR", "")
	t.Setenv("PATH", "/usr/bin:/bin")
	nvm = filepath.Join(home, ".nvm")
	for _, v := range versions {
		bin := filepath.Join(nvm, "versions", "node", v, "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		testenv.Program(t, filepath.Join(bin, "node"), "#!/bin/sh\necho "+v+"\n")
		testenv.Program(t, filepath.Join(bin, "codex"), "#!/bin/sh\necho codex under "+v+"\n")
	}
	for name, to := range aliases {
		p := filepath.Join(nvm, "alias", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(to+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home, nvm
}

// lts is what `nvm install --lts` leaves in alias/: lts/* naming the newest
// LTS line, each line naming its newest release, and the default it set.
var lts = map[string]string{
	"default":      "lts/*",
	"lts/*":        "lts/jod",
	"lts/hydrogen": "v18.20.4",
	"lts/iron":     "v20.18.0",
	"lts/jod":      "v22.12.0",
}

func with(m map[string]string, k, v string) map[string]string {
	out := map[string]string{k: v}
	for a, b := range m {
		if a != k {
			out[a] = b
		}
	}
	return out
}

// uni-switch's PATH gets the nvm Node a terminal gets — the one nvm's default
// alias resolves to, as nvm.sh resolves it — not the folder whose name
// sorts last: in string order v9.11.2 comes after v22.12.0, so a codex
// under a Node the terminal doesn't use was found and run.
func TestUserPathNvmDefault(t *testing.T) {
	all := []string{"v9.11.2", "v18.20.4", "v20.18.0", "v22.3.0", "v22.12.0"}
	for _, c := range []struct {
		name     string
		versions []string
		aliases  map[string]string
		want     string // "" for no nvm Node at all
	}{
		{"no default: the highest, v22 over v9", []string{"v9.11.2", "v22.12.0"}, nil, "v22.12.0"},
		{"lts/* through lts/jod", all, lts, "v22.12.0"},
		{"lts/iron", all, with(lts, "default", "lts/iron"), "v20.18.0"},
		{"lts/-1, the line before the newest", all, with(lts, "default", "lts/-1"), "v20.18.0"},
		{"a partial version: the newest 22.x, not v22.3.0", all, map[string]string{"default": "22"}, "v22.12.0"},
		{"v20, prefixed", all, map[string]string{"default": "v20"}, "v20.18.0"},
		{"an exact old version", all, map[string]string{"default": "v9.11.2"}, "v9.11.2"},
		{"node: the highest", all, map[string]string{"default": "node"}, "v22.12.0"},
		{"stable: the highest", all, map[string]string{"default": "stable"}, "v22.12.0"},
		{"a user alias in between", all, map[string]string{"default": "work", "work": "18"}, "v18.20.4"},
		{"a default that isn't installed: the highest", []string{"v9.11.2", "v22.12.0"}, map[string]string{"default": "v18.20.4"}, "v22.12.0"},
		{"lts/* naming a line not installed: the highest", []string{"v9.11.2", "v20.18.0"}, lts, "v20.18.0"},
		{"an alias cycle: the highest", all, map[string]string{"default": "a", "a": "b", "b": "a"}, "v22.12.0"},
		{"system: none of nvm's", all, map[string]string{"default": "system"}, ""},
		{"system through an alias", all, map[string]string{"default": "mine", "mine": "system"}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, nvm := nvmHome(t, c.versions, c.aliases)
			var got []string
			for _, d := range userDirs(false) {
				if strings.HasPrefix(d, nvm) {
					got = append(got, d)
				}
			}
			var want []string
			if c.want != "" {
				want = []string{filepath.Join(nvm, "versions", "node", c.want, "bin")}
			}
			if !slices.Equal(got, want) {
				t.Fatalf("nvm's on PATH: %q, want %q", got, want)
			}
		})
	}
}

// A codex found off PATH is the one under nvm's default Node; the other
// Nodes follow it highest first, so a codex only an older Node has is
// still found.
func TestFindToolNvmDefaultFirst(t *testing.T) {
	_, nvm := nvmHome(t, []string{"v9.11.2", "v20.18.0", "v22.3.0", "v22.12.0"}, with(lts, "default", "lts/iron"))
	bin := func(v string) string { return filepath.Join(nvm, "versions", "node", v, "bin") }
	if got, want := FindTool("codex"), filepath.Join(bin("v20.18.0"), "codex"); got != want {
		t.Fatalf("found %q, want the default's %q", got, want)
	}
	var got []string
	for _, d := range UserBinDirs() {
		if strings.HasPrefix(d, nvm) {
			got = append(got, d)
		}
	}
	want := []string{bin("v20.18.0"), bin("v22.12.0"), bin("v22.3.0"), bin("v9.11.2")}
	if !slices.Equal(got, want) {
		t.Fatalf("nvm's folders in order %q, want %q", got, want)
	}
}

// NVM_DIR names nvm's folder when the profile sets it.
func TestUserPathNvmDir(t *testing.T) {
	home, nvm := nvmHome(t, []string{"v9.11.2", "v22.12.0"}, map[string]string{"default": "9"})
	moved := filepath.Join(home, ".config", "nvm")
	os.MkdirAll(filepath.Dir(moved), 0o755)
	if err := os.Rename(nvm, moved); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NVM_DIR", moved)
	want := filepath.Join(moved, "versions", "node", "v9.11.2", "bin")
	if !slices.Contains(userDirs(false), want) {
		t.Fatalf("%q not in %q", want, userDirs(false))
	}
}

// fnm's default alias (a link to the version's folder) comes first, and
// mise's and fnm's other versions by number, not name.
func TestUserBinDirsFnmMise(t *testing.T) {
	home, _ := nvmHome(t, nil, nil)
	fnm := filepath.Join(home, ".local", "share", "fnm")
	fbin := func(v string) string { return filepath.Join(fnm, "node-versions", v, "installation", "bin") }
	mise := filepath.Join(home, ".local", "share", "mise", "installs", "node")
	for _, d := range []string{fbin("v9.11.2"), fbin("v20.18.0"), fbin("v22.12.0"),
		filepath.Join(mise, "9.11.2", "bin"), filepath.Join(mise, "22.12.0", "bin"), filepath.Join(fnm, "aliases")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// as fnm and mise make them
	os.Symlink(filepath.Join(fnm, "node-versions", "v20.18.0", "installation"), filepath.Join(fnm, "aliases", "default"))
	os.Symlink("22.12.0", filepath.Join(mise, "lts"))
	var got []string
	for _, d := range UserBinDirs() {
		if strings.HasPrefix(d, filepath.Join(home, ".local", "share")) {
			got = append(got, d)
		}
	}
	want := []string{filepath.Join(mise, "22.12.0", "bin"), filepath.Join(mise, "9.11.2", "bin"), filepath.Join(mise, "lts", "bin"),
		fbin("v20.18.0"), fbin("v22.12.0"), fbin("v9.11.2")}
	if !slices.Equal(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}
