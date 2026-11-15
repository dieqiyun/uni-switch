package gateway

import (
	"testing"
)

// contextFull is how Kiro-backed relays say a conversation is past the
// model's window: kiro.rs (src/anthropic/handlers.rs, map_provider_error)
// and kiro2claude (error-mapper.ts) turn Kiro's
// CONTENT_LENGTH_EXCEEDS_THRESHOLD into it, as a 400
// invalid_request_error, and a new-api relay in front of one adds its
// request id. ergou0954_54426 on Discord had it from a "kiro opus" model
// of an API-key relay in an intent-routed group: not read as an overflow,
// it went to the agent as a plain 400, which never compacted, and no other
// member was asked.
const contextFull = "Context window is full. Reduce conversation history, system prompt, or tools."

// The words are an overflow on every request-error status a relay sends
// them with, and in a stream's error event; neither a used-up plan nor
// a fault another account of the model would answer differently. An
// ordinary 400 is still not one.
func TestContextWindowFullIsAnOverflow(t *testing.T) {
	said := contextFull + " (request id: 2026110112345678901234567)"
	bodies := []string{
		`{"type":"error","error":{"type":"invalid_request_error","message":"` + contextFull + `"}}`,
		`{"error":{"message":"` + said + `","type":"invalid_request_error","param":"","code":null}}`,
		// its sibling in kiro.rs, for one input past Kiro's own limit
		`{"type":"error","error":{"type":"invalid_request_error","message":"Input is too long. Reduce the size of your messages."}}`,
	}
	for _, status := range []int{400, 413, 422} {
		for _, b := range bodies {
			if !overflowed(status, []byte(b)) {
				t.Errorf("%d %s isn't an overflow", status, b)
			}
			if retryable(status, []byte(b)) {
				t.Errorf("%d %s is asked of another account", status, b)
			}
			if f := failure(status, []byte(b)); f != failOther {
				t.Errorf("%d %s is a %s failure", status, b, f)
			}
		}
	}
	// in a stream, as an error event: an overflow's 400, not a vendor's
	// passing 502 that rests the account and is asked again
	if got := streamStatus(said); got != 400 {
		t.Errorf("a stream's %q stands for %d", said, got)
	}
	if lateRests(said) {
		t.Errorf("a reply cut by %q rests the account", said)
	}
	for _, msg := range []string{
		`{"error":{"message":"invalid model: kiro-opus-9 (request id: 20261101)","type":"invalid_request_error"}}`,
		`{"error":{"message":"model not found","type":"invalid_request_error"}}`,
		`{"error":{"message":"Invalid request: messages is required","type":"invalid_request_error"}}`,
	} {
		if overflowed(400, []byte(msg)) {
			t.Errorf("an ordinary 400 %s is taken for an overflow", msg)
		}
	}
}
