package gateway

import (
	"encoding/json"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// ccdcn on X: Codex now offers low, medium and high for a model uni-switch
// has no word on (provider.CodexEfforts, XJAI's model-test). Each reaches
// the vendor as Codex picked it, on its Responses: the levels offered are
// ones the gateway sends, not a picker that changes nothing.
func TestCodexPickedEffortReachesUnknownModel(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"id":"c1","model":"model-test","choices":[{"delta":{"role":"assistant","content":"hi"}}]}`,
		`data: {"id":"c1","choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`)}
	up := setup(t, provider.Chat, f)
	if err := provider.Save(provider.Provider{ID: "xjai", Name: "XJAI", Key: "k", Chat: up.URL + "/v1", Models: []string{"model-test"}}); err != nil {
		t.Fatal(err)
	}
	e, ok := provider.EntryOf("xjai/model-test")
	if !ok {
		t.Fatal("xjai/model-test not listed")
	}
	levels := provider.CodexEfforts(e)
	if len(levels) != 3 {
		t.Fatalf("Codex is offered %v for xjai/model-test", levels)
	}
	for _, effort := range levels {
		code, body := post(t, "/v1/responses", `{"model":"xjai/model-test","stream":true,"input":"hi","reasoning":{"effort":"`+effort+`","summary":"auto"}}`)
		if code != 200 {
			t.Fatalf("%s: status %d: %s", effort, code, body)
		}
		var got struct {
			Effort string `json:"reasoning_effort"`
		}
		json.Unmarshal(f.got, &got)
		if got.Effort != effort {
			t.Errorf("Codex picked %s; the vendor was sent %s", effort, f.got)
		}
	}
}
