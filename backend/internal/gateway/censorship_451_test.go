package gateway

import (
	"strings"
	"testing"
)

// The refusal read from the error object itself, as settle reads it.
func TestCensorshipBlockedIsAPolicyRefusal(t *testing.T) {
	note, ok := policyRefusal([]byte(`{"error":{"type":"censorship_blocked","message":"The content you provided or machine outputted is blocked."}}`))
	if !ok || !strings.Contains(note, "censorship_blocked") {
		t.Fatalf("policyRefusal: %q, %v", note, ok)
	}
}
