package agent

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// The gateway takes a client that is no agent uni-switch knows, with the plain
// token, for a script Claude Code started (tbxark on X); every agent's own
// User-Agent must still read as an agent, and a script's must not.
func TestIsAgentByUserAgent(t *testing.T) {
	for _, a := range Clients() {
		if !usage.IsAgent(a.ID) {
			t.Errorf("%s isn't an agent", a.ID)
		}
	}
	for _, ua := range []string{"claude-cli/2.1.287 (external, cli)", "codex_cli_rs/0.160.0 (Mac OS 26.6.0; arm64)", "Cursor/3.2"} {
		if got := usage.AgentOf(ua); !usage.IsAgent(got) {
			t.Errorf("%q reads as %q, no agent", ua, got)
		}
	}
	for _, ua := range []string{"Python-urllib/3.12", "anthropic-python/0.75.0", ""} {
		if got := usage.AgentOf(ua); usage.IsAgent(got) {
			t.Errorf("%q reads as the agent %q", ua, got)
		}
	}
}
