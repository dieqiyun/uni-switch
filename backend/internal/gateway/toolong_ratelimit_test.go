package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A rate limit that counts tokens ("Too many tokens, please wait…", Bedrock's
// throttling; Cerebras's per-minute limit) is not the conversation outgrowing
// the model: said as "prompt is too long", Claude Code compacted after each
// one, and with the limit still on, compacted again within a turn or two until
// it stopped as "Autocompact is thrashing" (StringKe on Discord).
func TestRateLimitNotContextOverflow(t *testing.T) {
	for _, msg := range []string{
		"Bedrock: Too many tokens, please wait before trying again.",
		"Bedrock: Too many tokens per day, please wait before trying again.",
		"Cerebras: Tokens per minute limit exceeded - too many tokens processed.",
	} {
		for _, proto := range []provider.Protocol{provider.Anthropic, provider.Chat, provider.Responses} {
			w := httptest.NewRecorder()
			status := writeError(w, proto, 429, msg)
			var body struct {
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
					Code    any    `json:"code"`
				} `json:"error"`
			}
			json.Unmarshal(w.Body.Bytes(), &body)
			if status != 429 || body.Error.Type != "rate_limit_error" || body.Error.Message != msg || body.Error.Code != nil {
				t.Errorf("%s %q: status %d body %s", proto, msg, status, w.Body.String())
			}
		}
	}
}

// The same through the gateway: Claude Code's request, a vendor's 429.
func TestRateLimitReachesClaudeCodeAsRateLimit(t *testing.T) {
	f := &fake{code: 429, ctype: "application/json",
		reply: `{"message":"Too many tokens, please wait before trying again."}`}
	setup(t, provider.Anthropic, f)
	code, body := post(t, "/v1/messages", `{"model":"fake/m1","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if code != 429 || strings.Contains(strings.ToLower(body), "prompt is too long") || !strings.Contains(body, "rate_limit_error") {
		t.Fatalf("status %d body %s", code, body)
	}
}
