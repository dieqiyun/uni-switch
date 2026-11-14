package gateway

import (
	"fmt"

	"net/http/httptest"
	"os"
	"path/filepath"

	"strings"
	"testing"
	"time"
)

// turnedAway is Antigravity's answer to Claude Code's and the Agent SDK's
// system prompt: a 429 that reads as a used-up plan whatever the quota
// left, on every model (#666: Claude Desktop's chats always 429 while its
// titles and Codex on the same account answer). uni-switch says why, and now
// also leaves the account alone, since nothing is wrong with it.
const turnedAway = `{"error":{"code":429,"message":"Resource has been exhausted (e.g. check quota).","status":"RESOURCE_EXHAUSTED"}}`

// claudeCodeSystem is Claude Code's, as 2.1.288 sends it: its billing line
// or the SDK's identity line anywhere in the system instruction.
const claudeCodeSystem = "x-anthropic-billing-header: cc_version=2.1.288.e3f; cc_entrypoint=sdk-cli;\nYou are Claude Code, Anthropic's official CLI for Claude."

// claudeTurn is a turn as Claude Code sends it on the group, with system
// as the system instruction the client asked for.
func claudeTurn(system string) string {
	return `{"model":"group/g","max_tokens":100,"system":` + quote(system) + `,"messages":[{"role":"user","content":"hi"}]}`
}

// turnOn posts a turn to this gateway and says what came back.
func turnOn(t *testing.T, s *Server, body string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)))
	return rec.Code, rec.Body.String()
}

// rests is what rests now, to say it when nothing should.
func rests() map[string]time.Time {
	restingUntil.Lock()
	defer restingUntil.Unlock()
	out := map[string]time.Time{}
	for k, v := range restingUntil.m {
		if v.After(time.Now()) {
			out[k] = v
		}
	}
	return out
}

// antigravityQuota is Antigravity's 429 when the plan's own allowance on the
// model is used up: it names the quota and when it resets, which the refusal
// of #666 never does.
const antigravityQuota = `{"error":{"code":429,"message":"You have exhausted your capacity on this model. Your quota will reset after 3h19m30s.","status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"QUOTA_EXHAUSTED","domain":"cloudcode-pa.googleapis.com"}]}}`

// directTurn is a Claude Desktop chat on Antigravity's model itself, not a
// group: the one account is the last left to ask (#1425).
func directTurn(system string, stream bool) string {
	return `{"model":"antigravity/m","max_tokens":100,"stream":` + fmt.Sprint(stream) + `,"system":` + quote(system) + `,"messages":[{"role":"user","content":"hi"}]}`
}

// The Routing page says the note apart from Antigravity's words, in its own
// language, only when it knows the very words the gateway adds.
func TestAntigravityTurnedAwayHintIsTheRoutingPages(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "gui", "assets", "plugins", "routing", "client.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), quote(antigravityTurnedAwayHint)) {
		t.Fatal("plugins/routing/client.js doesn't know the note the gateway adds to Antigravity's refusal")
	}
}
