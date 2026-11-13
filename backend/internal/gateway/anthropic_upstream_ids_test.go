package gateway

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A Codex conversation that had another vendor's tool call, its call_id
// with a character Anthropic doesn't take (Devin's "Bash:0#…", #1304), went
// on to a Messages upstream with that id as the tool_use's, which Anthropic
// turns away ("tool_use.id: String should match pattern
// '^[a-zA-Z0-9_-]+$'"). It goes as one Anthropic takes, its result named
// the same, and a safe id goes as it was.
func TestAnthropicUpstreamTakesAnotherVendorsCallIDs(t *testing.T) {
	f := &fake{t: t, ctype: "application/json", reply: `{"id":"msg_1","type":"message","role":"assistant","model":"m1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`}
	setup(t, provider.Anthropic, f)
	codexPostTo(New(), `{"model":"fake/m1","input":[
	  {"type":"message","role":"user","content":[{"type":"input_text","text":"list files"}]},
	  {"type":"function_call","call_id":"`+devinRawID+`","name":"shell","arguments":"{\"cmd\":\"ls\"}"},
	  {"type":"function_call_output","call_id":"`+devinRawID+`","output":"main.go"},
	  {"type":"function_call","call_id":"toolu_01AbC","name":"shell","arguments":"{\"cmd\":\"pwd\"}"},
	  {"type":"function_call_output","call_id":"toolu_01AbC","output":"/src"}]}`)
	var q struct {
		Messages []struct {
			Content []struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				ToolUseID string `json:"tool_use_id"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(f.got, &q); err != nil {
		t.Fatalf("%v: %s", err, f.got)
	}
	ok := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	var uses, results []string
	for _, m := range q.Messages {
		for _, c := range m.Content {
			switch c.Type {
			case "tool_use":
				uses = append(uses, c.ID)
			case "tool_result":
				results = append(results, c.ToolUseID)
			}
		}
	}
	if len(uses) != 2 || len(results) != 2 {
		t.Fatalf("tool_use %v, tool_result %v: %s", uses, results, f.got)
	}
	for i := range uses {
		if !ok.MatchString(uses[i]) || uses[i] != results[i] {
			t.Errorf("tool_use %q, its result %q", uses[i], results[i])
		}
	}
	if uses[1] != "toolu_01AbC" {
		t.Errorf("safe id sent as %q", uses[1])
	}
}
