package gui

import "github.com/dieqiyun/uni-switch/backend/internal/provider"

func savedEditorFixture(id string) (provider.Provider, error) {
	p := provider.Provider{ID: id, Name: id, Preset: id}
	switch id {
	case "deepseek":
		p.Chat = "https://api.deepseek.com/v1"
	case "volcengine":
		p.Chat = "https://ark.cn-beijing.volces.com/api/coding/v3"
		p.Anthropic = "https://ark.cn-beijing.volces.com/api/coding"
	case "opencode-zen":
		p.Chat = "https://opencode.ai/zen/v1"
		p.Anthropic = "https://opencode.ai/zen"
	default:
		return provider.FromPreset(id)
	}
	return p, nil
}
