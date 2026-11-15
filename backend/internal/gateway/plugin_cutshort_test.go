package gateway

import (
	"testing"
)

// A plugin that reads a model's text-form tool calls ends a turn whose
// stream died mid-call as a failure, so the gateway retries or fails over
// (upstream plugins #38, #2873): a 200 whose stream stops with an
// error event, the call it was reading unsent. The words are the
// contract's own, shared by the plugins that read text-form calls
// (packages/trae, packages/qoder's CUT_SHORT).
const cutShortSaid = "Trae CN: the model's tool call, written as text, was cut short; it is not sent on as the answer"

var cutShortStream = sse(
	`data: {"id":"c","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
	`data: {"error":{"message":"`+cutShortSaid+`","type":"api_error","code":null}}`,
	`data: [DONE]`)

func chatStreamSaid(said string) string {
	return sse(
		`data: {"id":"c","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"`+said+`"},"finish_reason":"stop"}]}`,
		`data: [DONE]`)
}

// A plugin's other errors keep their own lanes: the contract's unreadable
// call (#2414), a model its vendor doesn't serve, a sign-in it turned
// away — none is a cut, and each rests or fails over as it did.
func TestPluginCutShortLeavesOtherErrorsAlone(t *testing.T) {
	for _, x := range []struct {
		status int
		body   string
		want   string
	}{
		// the contract's UNREADABLE: the block is whole but can't be read
		// — the model's doing, told apart from a stream that died
		{502, `{"error":{"message":"Trae CN: the model's tool call, written as text, can't be read; it is not sent on as the answer","type":"api_error"}}`, failOther},
		// a plugin whose vendor doesn't serve the model asked for
		{400, `{"error":{"message":"the model fake-x does not exist"}}`, failOther},
		// a sign-in its vendor turned away
		{401, `{"error":{"message":"token expired"}}`, failOther},
		// the vendor's own cut-short words, a reply of its own kind
		{502, `{"error":{"message":"upstream connect error"}}`, failOther},
	} {
		if got := failure(x.status, []byte(x.body)); got != x.want {
			t.Errorf("failure(%d, %s) = %s, want %s", x.status, x.body, got, x.want)
		}
	}
	if got := failure(502, []byte(`{"error":{"message":"`+cutShortSaid+`"}}`)); got != failCut {
		t.Errorf("the contract's cut-short error is %s, want %s", got, failCut)
	}
}
