package agent

import (
	"strings"
	"testing"
)

// kiloSrc is the shape of uni-switch-community/plugins' Kilo agent (#90): one
// provider block whose models' effort variants go weakest first, the order
// Kilo lists them in.
const kiloSrc = `
export const agent = { id: "kilo", name: "Kilo Code", config: "~/.config/kilo/kilo.jsonc", format: "json", model: "model", prefix: "uni-switch/" };
export function connect({ gateway, models }) {
  const catalog = {};
  for (const m of models) {
    catalog[m.id] = { name: m.name ?? m.id, variants: { low: { reasoningEffort: "low" }, medium: { reasoningEffort: "medium" }, high: { reasoningEffort: "high" } } };
  }
  return { "provider.uni-switch": { name: "uni-switch", npm: "@ai-sdk/openai-compatible", options: { baseURL: gateway.v1, apiKey: gateway.key }, models: catalog } };
}
`

// inOrder fails unless each of words is in s, each after the one before.
func inOrder(t *testing.T, s string, words ...string) {
	t.Helper()
	at := -1
	for _, w := range words {
		i := strings.Index(s[at+1:], w)
		if i < 0 {
			t.Fatalf("%s missing or out of order (want %q):\n%s", w, words, s)
		}
		at += 1 + i
	}
}
