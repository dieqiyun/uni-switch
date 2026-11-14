package sessions

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"testing"
	"time"
)

// The tests below move a folder a reader goes into elsewhere and leave a
// link where it was: a junction on Windows, where Go lists one as
// irregular rather than as a folder or a symlink, and a symlink elsewhere
// (#2268, JasonBuildAI; its first three tests are the issue's).

// ccFilePaths is every session file ccFiles found, by path: one that stops
// being listed, or is listed from somewhere else, stands out.
func ccFilePaths(fs []file) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.path)
	}
	sort.Strings(out)
	return out
}

// moveBehindLink moves the folder at live elsewhere and leaves a link to it
// in its place.
func moveBehindLink(t *testing.T, live string) {
	t.Helper()
	elsewhere := filepath.Join(t.TempDir(), filepath.Base(live))
	if err := os.Rename(live, elsewhere); err != nil {
		t.Fatal(err)
	}
	linkFolder(t, elsewhere, live)
}

// A Claude Code project folder inside projects/ that is a link is read all
// the same: Sessions listed none of its sessions and Usage none of its
// calls. ccFiles reads Qoder's, Qoder CN's and Cowork's stores too.
func TestClaudeProjectFolderBehindALink(t *testing.T) {
	claude, _ := setup(t)
	before := ccFiles("claude", claude)
	if len(before) == 0 {
		t.Fatal("the fixture has no Claude session files")
	}
	listed := len(ListAgent("claude"))
	moveBehindLink(t, filepath.Join(claude, "projects", "-work-app"))

	after := ccFiles("claude", claude)
	if !reflect.DeepEqual(ccFilePaths(after), ccFilePaths(before)) {
		t.Fatalf("Claude session files behind a link: %v, want %v", ccFilePaths(after), ccFilePaths(before))
	}
	if got := len(ListAgent("claude")); got != listed {
		t.Fatalf("Claude sessions behind a link: %d, want %d", got, listed)
	}
}

// A session's own folder, where its subagents live, being a link is read
// too.
func TestClaudeSessionFolderBehindALink(t *testing.T) {
	claude, _ := setup(t)
	before := ccFiles("claude", claude)
	if len(before) == 0 {
		t.Fatal("the fixture has no Claude session files")
	}
	moveBehindLink(t, filepath.Join(claude, "projects", "-work-app", "11111111-2222-3333-4444-555555555555"))

	after := ccFiles("claude", claude)
	if !reflect.DeepEqual(ccFilePaths(after), ccFilePaths(before)) {
		t.Fatalf("a Claude session folder behind a link: %v, want %v", ccFilePaths(after), ccFilePaths(before))
	}
}

// A workflow run's folder (ultracode's agents) being a link is read too.
func TestClaudeWorkflowRunBehindALink(t *testing.T) {
	claude, _ := setup(t)
	subs := filepath.Join(claude, "projects", "-work-app", "11111111-2222-3333-4444-555555555555", "subagents")
	b, err := os.ReadFile(filepath.Join(subs, "agent-a1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	run := filepath.Join(subs, "workflows", "run-1")
	if err := os.MkdirAll(run, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run, "agent-w1.jsonl"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	before := ccFilePaths(ccFiles("claude", claude))
	if !slices.Contains(before, filepath.Join(run, "agent-w1.jsonl")) {
		t.Fatalf("the workflow's agent isn't read: %v", before)
	}
	moveBehindLink(t, run)
	if after := ccFilePaths(ccFiles("claude", claude)); !reflect.DeepEqual(after, before) {
		t.Fatalf("a workflow run behind a link: %v, want %v", after, before)
	}
}

// A Hermes profile folder that is a link is read too: HermesDirs skipped it
// and the profile's sessions were gone from Sessions and Usage.
func TestHermesProfileBehindALink(t *testing.T) {
	setup(t)
	root := filepath.Join(t.TempDir(), "hermes")
	t.Setenv("HERMES_HOME", root)
	hermesFixture(t, root, "", "link-default", true)
	hermesFixture(t, root, "work", "link-work", true)
	if got := hermesSessions(); got != 2 {
		t.Fatalf("Hermes sessions: %d, want 2", got)
	}
	moveBehindLink(t, filepath.Join(root, "profiles", "work"))
	if got := hermesSessions(); got != 2 {
		t.Fatalf("Hermes sessions behind a link: %d, want 2", got)
	}
}

func hermesSessions() int {
	n := 0
	for _, s := range List(0) {
		if s.Agent == "hermes" {
			n++
		}
	}
	return n
}

// An Antigravity CLI conversation folder that is a link keeps its calls.
func TestAgyConversationBehindALink(t *testing.T) {
	claude, _ := setup(t)
	agy := filepath.Join(filepath.Dir(claude), "home", ".gemini", "antigravity-cli")
	copyTree(t, "testdata/agy", agy)
	calls := func() int {
		n := 0
		for _, c := range Calls(time.Time{}) {
			if c.Agent == "agy" {
				n++
			}
		}
		return n
	}
	before := calls()
	if before == 0 {
		t.Fatal("the fixture has no agy calls")
	}
	moveBehindLink(t, filepath.Join(agy, "brain", "2b0e6c1a-1d3e-4f6a-9a51-0f1c2d3e4f5a"))
	if got := calls(); got != before {
		t.Fatalf("agy calls behind a link: %d, want %d", got, before)
	}
}
