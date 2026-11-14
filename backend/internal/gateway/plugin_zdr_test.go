package gateway

import (
	"net/http"

	"strings"

	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Command Code's own answers to x-cmd-zdr: 1 for a model no Zero Data
// Retention upstream serves, as they came on 2026-10-19 for
// poolside/laguna-s-2.1-free (upstream plugins #246, its
// zdr.test.mjs): the Provider API's 422, and the 400 the plugin makes of
// Go's /alpha/generate error line.
const (
	zdrProvider422 = `{"error":{"message":"This model has no zero-data-retention upstream. Remove the x-cmd-zdr header or choose a different model.","type":"invalid_request_error","code":"cmd_zdr_no_providers"}}`
	zdrGo400       = `{"error":{"message":"This model has no zero-data-retention upstream. Disable CMD_ZDR or pick a different model.","type":"cmd_zdr_no_providers","code":400}}`
)

// noneRested says nothing of the provider id rests or has a failure
// counted against it.
func noneRested(t *testing.T, id string) {
	t.Helper()
	restingUntil.Lock()
	for k, until := range restingUntil.m {
		if strings.HasPrefix(k, id) && time.Now().Before(until) {
			t.Errorf("%s rests until %s", k, until)
		}
	}
	restingUntil.Unlock()
	routed.Lock()
	for k, n := range routed.failures {
		if strings.HasPrefix(k, id) && n > 0 {
			t.Errorf("%s has %d failures counted", k, n)
		}
	}
	routed.Unlock()
}

// Only Command Code's own error code makes a refusal a setting's: another
// vendor's model refusal in the same words, or one in the same OpenAI
// error shape, still rests the model as before.
func TestZDRRefusalIsCommandCodesCodeOnly(t *testing.T) {
	for _, x := range []struct {
		status    int
		body      string
		zdr, rest bool // zdrRefused, modelRefused
	}{
		{422, zdrProvider422, true, false},
		{400, zdrGo400, true, false},
		{400, `Command Code: ` + zdrGo400, true, false},
		// the same words with no such code are another vendor's
		{422, `{"error":{"message":"This model has no zero-data-retention upstream. Remove the x-cmd-zdr header or choose a different model.","type":"invalid_request_error"}}`, false, true},
		{404, `{"error":{"message":"The model 'gpt-9' does not exist","type":"invalid_request_error","code":"model_not_found"}}`, false, true},
		{400, `{"error":{"message":"model \"gpt-6-sol\" is not accessible via the /chat/completions endpoint","code":"unsupported_api_for_model"}}`, false, true},
		// the code named in a message, not as the error's type or code
		{400, `{"error":{"message":"unknown model; see cmd_zdr_no_providers","type":"invalid_request_error"}}`, false, true},
		// not a refusal
		{200, zdrProvider422, false, false},
	} {
		if got := zdrRefused(x.status, []byte(x.body)); got != x.zdr {
			t.Errorf("zdrRefused(%d, %s) = %v", x.status, x.body, got)
		}
		if got := modelRefused(x.status, []byte(x.body)); got != x.rest {
			t.Errorf("modelRefused(%d, %s) = %v", x.status, x.body, got)
		}
	}
}

// Wherever a try's failure is rested — a strict conversation's own
// account, a System One decision, a count — a Zero Data Retention refusal
// rests nothing: read from the vendor's own body, or from uni-switch's note
// beside the error the agent was given in Anthropic's shape, which keeps
// no code. Another vendor's model refusal in the same words still rests
// the model.
func TestZDRRefusalNeverRests(t *testing.T) {
	fresh(t)
	s := New()
	c := candidate{p: provider.Provider{ID: "commandcode-plan", Name: "Command Code Plan", Key: "k"}, model: "m", rest: "commandcode-plan"}
	noted := http.Header{}
	keepRetry(noted, http.Header{}, []byte(zdrGo400))
	anthropicShaped := []byte(`{"error":{"message":"Command Code Plan: This model has no zero-data-retention upstream. Disable CMD_ZDR or pick a different model.","type":"invalid_request_error"},"type":"error"}`)
	for _, x := range []struct {
		status int
		header http.Header
		body   []byte
	}{
		{422, http.Header{}, []byte(zdrProvider422)},
		{400, http.Header{}, []byte(zdrGo400)},
		{400, noted, anthropicShaped},
	} {
		s.restAfter(c, x.status, x.header, x.body)
		if r, ok := restingOf(c); ok {
			t.Fatalf("%d %s rests: %+v", x.status, x.body, r)
		}
	}
	noneRested(t, "commandcode-plan")
	// without the note, the same words rest the model, as any vendor's do
	s.restAfter(c, 400, http.Header{}, anthropicShaped)
	if _, ok := restingOf(c); !ok {
		t.Fatal("a model refusal without Command Code's code rests nothing")
	}
}
