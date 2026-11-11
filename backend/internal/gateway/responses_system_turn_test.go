package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Claude Code 2.1.296 and later send their environment as a message with
// role "system" inside messages (Request.envInTurns). The Chat and
// Anthropic builds fold such a message into the system prompt
// (responseInstructions); buildResponses wrote the role as it came, and a
// relay in front of the ChatGPT backend — which takes user, assistant and
// developer alone — answered every translated turn 400 "Upstream request
// failed", and the 400 failed no one over (#4632). A translated request's
// system message is the harness's, not an instruction item of a Responses
// client's own: it is said the way the Chat and Anthropic builds say it.
// A Responses client's own instruction items keep their place (#2532,
// TestResponsesInstructionRolesThroughSearch).
func TestClaudeSystemTurnSaidInTheInstructionsOnResponses(t *testing.T) {
	srv, seen := relayOfEvery(t)
	fresh(t)
	if err := provider.Save(provider.Provider{
		ID: "responses-relay", Name: "Responses relay", Key: "k",
		Responses: srv.URL + "/v1", Models: []string{"example-model"},
	}); err != nil {
		t.Fatal(err)
	}
	body := `{"model":"responses-relay/example-model","max_tokens":64,"stream":true,` +
		`"system":"You are Claude Code.",` +
		`"messages":[` +
		`{"role":"system","content":"env: working dir /home/me"},` +
		`{"role":"user","content":"hi"},` +
		`{"role":"assistant","content":"hello"},` +
		`{"role":"system","content":"the user switched rooms"}]}`
	if code, out := post(t, "/v1/messages", body); code != 200 {
		t.Fatalf("%d %s", code, out)
	}
	paths, bodies := seen()
	if len(paths) != 1 || paths[0] != "/v1/responses" {
		t.Fatalf("upstream paths %v, want one Responses request", paths)
	}
	var got struct {
		Instructions string `json:"instructions"`
		Input        []struct {
			Type    string
			Role    string
			Content json.RawMessage
		} `json:"input"`
	}
	if err := json.Unmarshal([]byte(bodies[0]), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Instructions, "You are Claude Code.") ||
		!strings.Contains(got.Instructions, "env: working dir /home/me") {
		t.Fatalf("the leading system message was to join the instructions: %q", got.Instructions)
	}
	var reminded bool
	for i, m := range got.Input {
		switch {
		case m.Role == "system" || m.Role == "developer":
			t.Errorf("input[%d]: role %q went as it came; the ChatGPT backend behind the relay takes user, assistant and developer alone: %s", i, m.Role, bodies[0])
		case m.Role == "user" && strings.Contains(text(responsesParts(m.Content)), "<system-reminder>"):
			if !strings.Contains(text(responsesParts(m.Content)), "the user switched rooms") {
				t.Errorf("input[%d]: a reminder without the late system message: %s", i, bodies[0])
			}
			reminded = true
		}
	}
	if !reminded {
		t.Fatalf("the system message after a reply was to be told as a reminder: %s", bodies[0])
	}
}
