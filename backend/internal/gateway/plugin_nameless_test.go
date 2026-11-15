package gateway

import (
	"testing"
)

// The Qoder plugin's 502 for a reply whose only answer was a native tool
// call the model never named, as its errorResponse sends it once its own
// second ask came back the same (upstream plugins #579, packages/
// qoder's NAMELESS): qfmodel's glitch, which the next ask mostly isn't.
const namelessSaid = `{"error":{"message":"the model's native tool call has no name; it is not sent on as a tool","type":"qoder_error","code":502}}`

// The words alone decide it, in a body or a stream's error event, and
// words that are near them are not them.
func TestPluginNamelessWords(t *testing.T) {
	for _, x := range []struct {
		status int
		body   string
		want   string
	}{
		{502, namelessSaid, failCut},
		{200, `data: {"error":{"message":"the model's native tool call has no name; it is not sent on as a tool","code":502}}`, failCut},
		{502, `{"error":{"message":"tool call has no name"}}`, failOther},
		{502, `{"error":{"message":"the tool has no name"}}`, failOther},
	} {
		if got := failure(x.status, []byte(x.body)); got != x.want {
			t.Errorf("failure(%d, %s) = %s, want %s", x.status, x.body, got, x.want)
		}
	}
}
