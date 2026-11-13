package cli

import (
	"os"

	"testing"
)

// goneAgentSrc is an agent plugin as agent-kilo is: a JSONC file, the
// model under "model", a provider block of its own.
const goneAgentSrc = `
export const agent = { id: "kilo", name: "Kilo", config: "~/.config/kilo/kilo.jsonc", model: "model", prefix: "uni-switch/" };
export function connect({ gateway }) {
  return { "provider.uni-switch": { npm: "@ai-sdk/openai-compatible", options: { baseURL: gateway.v1, apiKey: gateway.key } } };
}
`

const goneAgentMine = "{\n  // mine\n  \"model\": \"anthropic/claude-sonnet-4-5\", // mine\n  \"provider\": {\n    \"anthropic\": {}\n  }\n}\n"

func fileIs(t *testing.T, path, want string) {
	t.Helper()
	if b, _ := os.ReadFile(path); string(b) != want {
		t.Fatalf("%s isn't as the user had it:\n%s", path, b)
	}
}
