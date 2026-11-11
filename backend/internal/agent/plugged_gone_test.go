package agent

import (
	"encoding/json"
	"os"

	"testing"
)

// kiloGoneSrc is an agent plugin as agent-kilo is: a JSONC file, the model
// under "model" with the provider's prefix, and a provider block of its own.
const kiloGoneSrc = `
export const agent = { id: "kilo", name: "Kilo", config: "~/.config/kilo/kilo.jsonc", model: "model", prefix: "uni-switch/" };
export function connect({ gateway, models }) {
  return { "provider.uni-switch": { npm: "@ai-sdk/openai-compatible", options: { baseURL: gateway.v1, apiKey: gateway.key }, models: Object.fromEntries(models.map(m => [m.id, { name: m.name }])) } };
}
`

// kiloMine is a kilo.jsonc as its user wrote it: comments, a model of
// their own and a provider block of their own.
const kiloMine = `{
  // my Kilo
  "$schema": "https://app.kilo.ai/config.json",
  "model": "anthropic/claude-sonnet-4-5", // mine
  "provider": {
    "anthropic": { "options": { "apiKey": "sk-mine" } }
  }
}
`

func fileAsMine(t *testing.T, path, want string) {
	t.Helper()
	if b, err := os.ReadFile(path); err != nil || string(b) != want {
		t.Fatalf("%s is not as the user had it (%v):\n%s", path, err, b)
	}
}

// oldStash takes the spec and format out of the stash under key, as a
// uni-switch before #3810 wrote it.
func oldStash(t *testing.T, key string) {
	t.Helper()
	var st map[string]any
	if err := json.Unmarshal([]byte(stashLoad()[key]), &st); err != nil {
		t.Fatal(err)
	}
	if st["spec"] == nil || st["format"] == nil {
		t.Fatalf("stash has no spec or format: %v", st)
	}
	delete(st, "spec")
	delete(st, "format")
	b, _ := json.Marshal(st)
	stash(map[string]string{key: string(b)})
}
