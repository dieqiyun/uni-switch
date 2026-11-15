package gateway

import (
	"fmt"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// savedProtocolFixture models an existing, explicitly configured provider.
// It does not register presets or supply model lists. These tests exercise
// the retained wire adapters using intercepted transports or local servers;
// adding removed vendor presets is tested separately and must still fail.
func savedProtocolFixture(id string) (provider.Provider, error) {
	p := provider.Provider{ID: id, Name: id, Preset: id}
	switch id {
	case "azure", "bedrock", "bedrock-mantle", "baidu-qianfan":
		// Every test supplies its own resource URL and model list.
	case "deepseek":
		p.Chat, p.Responses, p.Anthropic = "https://api.deepseek.com/v1", "https://api.deepseek.com/v1", "https://api.deepseek.com/anthropic"
	case "clinepass":
		p.Chat = "https://api.cline.bot/api/v1"
	case "kilo":
		p.Chat = "https://api.kilocode.ai/api/openrouter"
	case "kimi-code-cn":
		p.Chat, p.Anthropic = "https://api.kimi.com/coding/v1", "https://api.kimi.com/coding"
	case "moonshot-cn":
		p.Chat = "https://api.moonshot.cn/v1"
	case "volcengine":
		p.Chat, p.Responses, p.Anthropic = "https://ark.cn-beijing.volces.com/api/coding/v3", "https://ark.cn-beijing.volces.com/api/coding/v3", "https://ark.cn-beijing.volces.com/api/coding"
	case "minimax", "minimax-cn":
		host := "https://api.minimax.io"
		if id == "minimax-cn" {
			host = "https://api.minimaxi.com"
		}
		p.Chat, p.Anthropic = host+"/v1", host+"/anthropic"
	case "voyage":
		p.Chat = "https://api.voyageai.com/v1"
	case "zhipu", "zai":
		host := "https://open.bigmodel.cn"
		if id == "zai" {
			host = "https://api.z.ai"
		}
		p.Chat, p.Responses, p.Anthropic = host+"/api/paas/v4", host+"/api/v1", host+"/api/anthropic"
	case "qwen-decision":
		p.Decide = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	case "cloudflare-jev":
		p.Decide = "https://api.cloudflare.com/client/v4/accounts/test/ai/run"
	case "tencent-tokenhub", "tencent-tokenhub-cn":
		p.Chat, p.Responses, p.Anthropic = "https://tokenhub.test/v1", "https://tokenhub.test/v1", "https://tokenhub.test"
	default:
		return provider.Provider{}, fmt.Errorf("no saved protocol fixture %q", id)
	}
	return p, nil
}
