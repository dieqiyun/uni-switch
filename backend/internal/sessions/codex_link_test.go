package sessions

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// linkFolder makes path a link to the folder target: on Windows a junction,
// which is what the Codex CLI itself makes of ~/.codex/sessions when .codex
// is kept on another drive, and a symlink elsewhere.
func linkFolder(t *testing.T, target, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", path, target).CombinedOutput(); err != nil {
			t.Skipf("mklink /J: %v %s", err, out)
		}
		return
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlink: %v", err)
	}
}

// A Codex folder whose sessions folder is a link is read all the same, and
// each rollout is still named under the Codex folder itself: the Codex CLI
// makes ~/.codex/sessions a junction to the drive .codex is kept on, and the
// sessions behind it were not listed at all (#2233). The folder is the one
// whose config.toml and state databases are read back out of a rollout's
// path, which the link's own target doesn't hold.
func TestCodexRolloutsBehindALink(t *testing.T) {
	_, codex := setup(t)
	before := codexFiles()
	if len(before) == 0 {
		t.Fatal("the fixture has no rollouts")
	}
	listed := ListAgent("codex")
	if len(listed) == 0 {
		t.Fatal("the fixture has no Codex session")
	}
	if err := os.WriteFile(filepath.Join(codex, "config.toml"), []byte("model_provider = \"uni-switch\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	live := filepath.Join(codex, "sessions")
	elsewhere := filepath.Join(t.TempDir(), "sessions")
	if err := os.Rename(live, elsewhere); err != nil {
		t.Fatal(err)
	}
	linkFolder(t, elsewhere, live)

	after := codexFiles()
	want := map[string]int64{}
	for _, f := range before {
		want[f.key] = f.size
	}
	got := map[string]int64{}
	for _, f := range after {
		got[f.key] = f.size
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rollouts behind a link: %v, want %v", got, want)
	}
	if got := len(ListAgent("codex")); got != len(listed) {
		t.Fatalf("Codex sessions behind a link: %d, want %d", got, len(listed))
	}
	if home := codexHomeOf(after[0].path); home != codex {
		t.Fatalf("a rollout behind a link reads as %s, want %s", home, codex)
	}
	for _, m := range ListAgent("codex") {
		if m.UsesProvider != "uni-switch" {
			t.Fatalf("session %s uses %q, want uni-switch", m.ID, m.UsesProvider)
		}
	}
}

// The same holds for the other agents whose sessions are walked from one
// root: WorkBuddy's (and CodeBuddy Code's) projects folder, each session
// named under the agent's own folder.
func TestBuddyProjectsBehindALink(t *testing.T) {
	setupAgents(t)
	before := workbuddyFiles()
	if len(before) == 0 {
		t.Fatal("the fixture has no WorkBuddy session")
	}
	live := filepath.Join(WorkBuddyDir(), "projects")
	elsewhere := filepath.Join(t.TempDir(), "projects")
	if err := os.Rename(live, elsewhere); err != nil {
		t.Fatal(err)
	}
	linkFolder(t, elsewhere, live)
	after := workbuddyFiles()
	if len(after) != len(before) {
		t.Fatalf("WorkBuddy sessions behind a link: %d, want %d", len(after), len(before))
	}
	for _, f := range after {
		if !strings.HasPrefix(f.path, live+string(filepath.Separator)) {
			t.Fatalf("a WorkBuddy session behind a link is named %s, want it under %s", f.path, live)
		}
	}
}
