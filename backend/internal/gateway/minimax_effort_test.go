package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// What goes to a Claude and to another vendor's model on an Anthropic
// endpoint is as before: the budget alone, no output_config.
func TestNativeEffortLeavesOthersAlone(t *testing.T) {
	fresh(t)
	up := &adaptiveVendor{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Anthropic: srv.URL,
		Models: []string{"claude-sonnet-4-5", "kimi-k3", "MiniMax-M3"}}); err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"claude-sonnet-4-5", "kimi-k3", "MiniMax-M3"} {
		for _, ask := range []struct{ path, body string }{
			{"/v1/responses", `{"model":"relay/` + model + `","input":"Reply exactly OK.","reasoning":{"effort":"high"},"max_output_tokens":8192,"stream":false}`},
			{"/v1/chat/completions", `{"model":"relay/` + model + `","messages":[{"role":"user","content":"Reply exactly OK."}],"reasoning_effort":"high","max_tokens":8192,"stream":false}`},
		} {
			postProto(t, New(), ask.path, ask.body)
			got := up.last()
			b, _ := json.Marshal(map[string]any{"thinking": got["thinking"], "output_config": got["output_config"]})
			if want := `{"output_config":null,"thinking":{"budget_tokens":7168,"type":"enabled"}}`; string(b) != want {
				t.Errorf("%s %s: %s", ask.path, model, b)
			}
		}
	}
}
