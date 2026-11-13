package gateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// myduso on Discord: Claude's subscription kept to Claude Code, uni-switch
// only switching its sign-in. A request naming the Claude accounts runs
// no Claude Code through the bridge and is told why, with how to undo it;
// unticked, it is served again.
func TestClaudeKeptToClaudeCodeServesNothing(t *testing.T) {
	claudeMadeFirst(t, false)
	s := New()
	t.Cleanup(s.subscription.abortAll)
	calls := filepath.Join(t.TempDir(), "calls")
	claudeRefuses(t, calls, "tok-none") // no account refused

	if err := provider.SetSwitchOnly("claude", true); err != nil {
		t.Fatal(err)
	}
	rec := askClaudeModel(s, "hi")
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), "kept to Claude Code's own sign-in") {
		t.Fatalf("kept to Claude Code: %d %s", rec.Code, rec.Body.String())
	}
	if b, _ := os.ReadFile(calls); len(b) != 0 {
		t.Fatalf("Claude Code run through the bridge: %q", b)
	}

	if err := provider.SetSwitchOnly("claude", false); err != nil {
		t.Fatal(err)
	}
	if rec := askClaudeModel(s, "again"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "healthy account") {
		t.Fatalf("unticked: %d %s", rec.Code, rec.Body.String())
	}
}
