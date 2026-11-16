package gateway

import (
	"net/http"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Native account traffic has no uni-switch attribution key. Recognize the
// client's own headers as well, including before its agent registry is ready.
func clientProvider(r *http.Request) string {
	id := agentOf(r)
	if selected := provider.AgentProvider(id); selected != "" {
		return selected
	}
	if id == "claude-desktop" {
		return ""
	}
	if fromCodex(r.Header) {
		return provider.AgentProvider("codex")
	}
	if fromClaudeCode(r.Header) {
		return provider.AgentProvider("claude")
	}
	return ""
}
