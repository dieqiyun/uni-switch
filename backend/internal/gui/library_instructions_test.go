package gui

import (
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/library"
)

// A change to the instructions is answered with the instructions alone,
// not the whole library read again through every agent's skill folders
// (#1624). The problems the last change had with servers and skills still
// stand; those with the instructions are the new change's.
func TestInstructionChangeAnswersInstructions(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("PATH", "")
	lastProblems.Lock()
	was := lastProblems.p
	lastProblems.p = []library.Problem{
		{Agent: "codex", What: "mcp:fs", Error: "locked"},
		{Agent: "codex", What: "instructions", Error: "old"},
	}
	lastProblems.Unlock()
	t.Cleanup(func() { lastProblems.Lock(); lastProblems.p = was; lastProblems.Unlock() })

	v, err := instructionsView(&library.Result{Problems: []library.Problem{{Agent: "claude", What: "instructions", Error: "new"}}})
	if err != nil {
		t.Fatal(err)
	}
	if v.Part != "instructions" || v.Instructions == nil {
		t.Fatalf("not the instructions: %+v", v)
	}
	if len(v.Problems) != 2 || v.Problems[0].What != "mcp:fs" || v.Problems[1].Error != "new" {
		t.Errorf("problems: %+v", v.Problems)
	}
}
