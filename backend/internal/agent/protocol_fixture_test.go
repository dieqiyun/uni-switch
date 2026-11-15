package agent

import (
	"fmt"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func savedAgentProtocol(id string) (provider.Provider, error) {
	p := provider.Provider{ID: id, Name: id, Preset: id}
	switch id {
	case "xiaomi":
		p.Anthropic = "https://api.xiaomimimo.com/anthropic"
		p.Chat = "https://api.xiaomimimo.com/v1"
	case "opencode-zen":
		p.Anthropic = "https://opencode.ai/zen/v1"
		p.Chat = "https://opencode.ai/zen/v1"
		p.Responses = "https://opencode.ai/zen/v1"
	default:
		return p, fmt.Errorf("no saved protocol fixture %s", id)
	}
	return p, nil
}
