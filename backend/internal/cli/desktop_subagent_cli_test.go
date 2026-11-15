package cli

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
)

// uni-switch claude-desktop subagent_model prints no restart advice once
// Desktop is on uni-switch; the pick that connects it, and a tier, do.
func TestDesktopSubagentModelNotice(t *testing.T) {
	cliHome(t)
	a, err := agent.Find("claude-desktop")
	if err != nil {
		t.Fatal(err)
	}
	call := func(field, value string, notice bool) {
		t.Helper()
		out, err := stdoutOf(t, func() error { return set(a, field, value) })
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, a.Notice()) != notice {
			t.Fatalf("%s notice wanted %v, printed %q", field, notice, out)
		}
	}
	call("subagent_effort", "high", false)
	if a.Wired() {
		t.Fatal("saving an unwired effort connected Desktop")
	}
	call("subagent_model", "a/m", true)
	if !a.Wired() {
		t.Fatal("Live pick did not connect Desktop")
	}
	call("subagent_model", "", false)
	call("subagent_effort", "high", false)
	call("subagent_effort", "", false)
	call("sonnet", "a/m", true)
}
