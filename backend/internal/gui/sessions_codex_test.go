package gui

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// usage.CodexModelEffort's answer is shared by every ask: a session or a
// summary is given its own copy of its thread's consumption, so what the
// page does with it never reaches the kept answer.
func TestCodexModelEffortCopiedPerSession(t *testing.T) {
	byThread := map[string][]sessions.ModelEffort{"t": {{Model: "m", Effort: "low", Tokens: sessions.Tokens{Input: 20}}}}
	s := sessions.Session{Agent: "codex", ID: "t"}
	addCodexModelEffort(&s, byThread)
	s.ModelEffort[0].Input = 999
	sum := sessions.Summary{Agent: "codex", ID: "t"}
	addCodexSummaryModelEffort(&sum, byThread)
	sum.ModelEffort[0].Input = 999
	if got := byThread["t"][0].Input; got != 20 {
		t.Fatalf("the shared answer was written through a session's copy: %d", got)
	}
}
