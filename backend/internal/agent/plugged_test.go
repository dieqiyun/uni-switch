package agent

const aiderSrc = `
export const agent = { id: "aider", name: "Aider", bin: "aider", config: "~/.aider.conf.yml", model: "model", prefix: "openai/", ua: ["aider"] };
export function connect({ gateway, model }) {
  return { "openai-api-base": gateway.v1, "openai-api-key": gateway.key, "map-tokens": model.id.endsWith("pro") ? 2048 : 4096 };
}
`

func containsOption(opts []Option, v string) bool {
	for _, o := range opts {
		if o.Value == v {
			return true
		}
	}
	return false
}
