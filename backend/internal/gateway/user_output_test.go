package gateway

import (
	"net/http/httptest"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// YaCo on Discord: Claude Code asks claude-opus-5-5 for its full 128000
// (its own registry's max_output_tokens) and uni-switch writes no
// CLAUDE_CODE_MAX_OUTPUT_TOKENS for a claude-* model, so a key whose quota
// is held per request as max_tokens times the price was turned away on
// every turn. The Max output the user sets on the provider's model lowers
// such a request outside a group too; with none set the agent's own goes
// as it is, and a request under the user's limit isn't raised.
func TestUserMaxOutputLowersAPlainProvidersRequest(t *testing.T) {
	fresh(t)
	up := &adaptiveVendor{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	if err := provider.Save(provider.Provider{ID: "anth", Name: "Anth", Key: "k", Anthropic: srv.URL,
		Models: []string{"claude-opus-5-5", "claude-sonnet-5-5"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelOutput("anth/claude-opus-5-5", 32000); err != nil {
		t.Fatal(err)
	}
	ask := func(model string, max int) float64 {
		t.Helper()
		body := `{"model":"anth/` + model + `","max_tokens":` + itoa(max) + `,"thinking":{"type":"adaptive","display":"omitted"},"output_config":{"effort":"medium"},"stream":false,"messages":[{"role":"user","content":"hi"}]}`
		if code, out := post(t, "/v1/messages", body); code != 200 {
			t.Fatalf("status %d: %s", code, out)
		}
		n, _ := up.last()["max_tokens"].(float64)
		return n
	}
	if n := ask("claude-opus-5-5", 128000); n != 32000 {
		t.Errorf("Claude Code's 128000 went as %v, want the user's 32000", n)
	}
	if n := ask("claude-opus-5-5", 8000); n != 8000 {
		t.Errorf("8000 went as %v, want as asked", n)
	}
	if n := ask("claude-sonnet-5-5", 128000); n != 128000 {
		t.Errorf("a model with no Max output set went as %v, want the agent's 128000", n)
	}
}
