package agent

import (
	"encoding/json"
	"os"

	"testing"
)

// pigSrc is an agent plugin as agent-pig is: PiG (a Go port of Pi) keeps
// its providers in models.json and the model a session starts on in
// settings.json, as defaultProvider and defaultModel (PiG's
// docs/site/docs/settings.md), so connect writes models.json and startup
// names the model in settings.json.
const pigSrc = `
export const agent = {
  id: "pig", name: "PiG", bin: "pig", dir: "~/.pig/agent",
  config: "~/.pig/agent/models.json", format: "json",
  startup: { config: "~/.pig/agent/settings.json", model: "defaultModel", keys: { defaultProvider: "uni-switch" } },
};
export function connect({ gateway, models }) {
  return { "providers.uni-switch": { name: "uni-switch", baseUrl: gateway.v1, apiKey: gateway.key, api: "openai-completions", models: models.map((m) => ({ id: m.id })) } };
}
`

// pigSettingsMine and pigModelsMine are PiG's two files as its user wrote
// them, on a provider of their own.
const pigSettingsMine = `{
  "defaultProvider": "anthropic",
  "defaultModel": "claude-sonnet-4",
  "theme": "dark"
}
`

const pigModelsMine = `{
  "providers": {
    "ollama": { "baseUrl": "http://localhost:11434/v1", "api": "openai-completions", "models": [{ "id": "qwen3" }] }
  }
}
`

func readJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	var m map[string]any
	if err != nil || json.Unmarshal(b, &m) != nil {
		t.Fatalf("%s: %v\n%s", path, err, b)
	}
	return m
}
