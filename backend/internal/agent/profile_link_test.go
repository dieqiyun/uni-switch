package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// moveBehindLink moves the folder at live elsewhere and leaves a link to it
// in its place: a junction on Windows, which Go lists as irregular rather
// than as a folder or a symlink, and a symlink elsewhere.
func moveBehindLink(t *testing.T, live string) {
	t.Helper()
	elsewhere := filepath.Join(t.TempDir(), filepath.Base(live))
	if err := os.Rename(live, elsewhere); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", live, elsewhere).CombinedOutput(); err != nil {
			t.Fatalf("mklink /J: %v %s", err, out)
		}
		return
	}
	if err := os.Symlink(elsewhere, live); err != nil {
		t.Fatal(err)
	}
}

// A Hermes profile folder that is a link is still a profile whose config
// uni-switch writes (#2268).
func TestHermesProfileBehindALink(t *testing.T) {
	home := syncHome(t)
	root := filepath.Join(home, ".hermes")
	for _, p := range []string{"config.yaml", "profiles/work/config.yaml"} {
		writeFile(t, filepath.Join(root, p), "model:\n  default: x\n")
	}
	t.Setenv("HERMES_HOME", root)
	if got := hermesProfiles(here(home)); len(got) != 2 {
		t.Fatalf("profiles: %v", got)
	}
	moveBehindLink(t, filepath.Join(root, "profiles", "work"))
	if got := hermesProfiles(here(home)); len(got) != 2 || got[1] != filepath.Join(root, "profiles", "work", "config.yaml") {
		t.Fatalf("profiles behind a link: %v", got)
	}
}

// An omp profile folder that is a link is still a profile row (#2268).
func TestOmpProfileBehindALink(t *testing.T) {
	home := syncHome(t)
	ompProfileAt(t, home, "work", "b/model-b")
	ids := func() map[string]bool {
		got := map[string]bool{}
		for _, a := range ompProfiles(home) {
			got[a.ID] = true
		}
		return got
	}
	if got := ids(); !got["omp#work"] {
		t.Fatalf("profiles %v", got)
	}
	moveBehindLink(t, filepath.Join(home, ".omp", "profiles", "work"))
	if got := ids(); len(got) != 1 || !got["omp#work"] {
		t.Fatalf("profiles behind a link %v", got)
	}
}
