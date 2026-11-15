package provider

// legacyVendor supplies explicit saved-configuration fixtures for protocol and
// pricing tests. It never registers a preset or supplies runtime defaults.
func legacyVendor(id string) (Provider, error) {
	p := Provider{ID: id, Name: id, Preset: id}
	switch id {
	case "anthropic":
		p.Anthropic, p.Catalog = "https://api.anthropic.com", "anthropic"
	case "openai":
		p.Chat, p.Responses, p.Catalog = "https://api.openai.com/v1", "https://api.openai.com/v1", "openai"
	case "bedrock":
		p.Chat = "https://bedrock-runtime.us-east-1.amazonaws.com/openai/v1"
		p.Responses, p.Anthropic = p.Chat, "https://bedrock-runtime.us-east-1.amazonaws.com/anthropic"
	case AzurePreset:
		p.Chat, p.Responses = "https://resource.openai.azure.com/openai/v1", "https://resource.openai.azure.com/openai/v1"
	case "deepseek":
		p.Chat, p.Catalog = "https://api.deepseek.com/v1", "deepseek"
	case "ollama-cloud":
		p.Chat, p.Catalog = "https://ollama.com/v1", "ollama-cloud"
	case "xiaomi":
		p.Chat, p.Catalog = "https://api.xiaomimimo.com/v1", "xiaomi"
	case "moonshot":
		p.Chat, p.Catalog = "https://api.moonshot.ai/v1", "moonshotai"
	case "kimi-code", "kimi-code-cn":
		p.Anthropic, p.Catalog = "https://api.kimi.ai/coding", "kimi-code-plan-global"
		if id == "kimi-code-cn" {
			p.Anthropic, p.Catalog = "https://api.kimi.com/coding", "kimi-code-plan-cn"
		}
	case "zhipu":
		p.Chat, p.Responses, p.Anthropic = "https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/v1", "https://open.bigmodel.cn/api/anthropic"
	case "typesafe":
		p.Decide = "https://api.typesafe.ai/v1"
	case "volcengine-decision":
		p.Decide = "https://ark.cn-beijing.volces.com/api/v1"
	case "clinepass":
		p.Chat = "https://api.cline.bot/api/v1"
	case "kilo":
		p.Chat = "https://api.kilo.ai/api/gateway"
	case "opencode-zen", "opencode-go":
		base := "https://opencode.ai/zen"
		if id == "opencode-go" {
			base += "/go"
		}
		p.Chat, p.Responses, p.Anthropic, p.Decide = base+"/v1", base+"/v1", base, base+"/v1"
		p.Catalog = "opencode"
	case "commandcode":
		p.Chat, p.Catalog = "https://api.commandcode.ai/provider/v1", "commandcode"
	case "together":
		p.Chat = "https://api.together.xyz/v1"
	case "vercel-jev":
		p.Decide = "https://ai-gateway.vercel.sh/typesafe"
	case "cloudflare-jev":
		p.Decide = "https://api.cloudflare.com/client/v4"
	case "pipellm":
		p.Chat, p.Responses, p.Anthropic = "https://api.pipellm.ai/openai/v1", "https://api.pipellm.ai/v1", "https://api.pipellm.ai"
	case "voyage":
		p.Chat, p.Models = "https://api.voyageai.com/v1", []string{"voyage-4-large", "rerank-3", "voyage-code-3"}
	default:
		return FromPreset(id)
	}
	return p, nil
}
