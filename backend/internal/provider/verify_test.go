package provider

import (
	"strings"
	"testing"
)

// Cloud Code Assist refuses an account Google wants verified with a 403
// whose details say VALIDATION_REQUIRED (#152); the agent is told what to
// do, with the link when there is one.
func TestVerificationRefusal(t *testing.T) {
	withLink := []byte(`{"error":{"code":403,"message":"Verify your account to continue.","status":"PERMISSION_DENIED","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"VALIDATION_REQUIRED","domain":"cloudcode-pa.googleapis.com","metadata":{"validation_url":"https://accounts.google.com/signin/continue?sarp=1&x=2"}}]}}`)
	link, ok := Verification(withLink)
	if !ok || link != "https://accounts.google.com/signin/continue?sarp=1&x=2" {
		t.Fatalf("%q %v", link, ok)
	}
	msg := APIError(withLink, "403 Forbidden")
	if !strings.HasPrefix(msg, "Verify your account to continue.") || !strings.Contains(msg, "open "+link+" in a browser") {
		t.Fatal(msg)
	}
	// said once, it isn't said again, and its link is still found
	if again := VerifyMessage(msg, link); again != msg {
		t.Fatal(again)
	}
	if l, ok := Verification([]byte("Antigravity: " + msg)); !ok || l != link {
		t.Fatalf("%q %v", l, ok)
	}

	noLink := []byte(`{"error":{"code":403,"message":"Verify your account to continue.","status":"PERMISSION_DENIED"}}`)
	link, ok = Verification(noLink)
	if !ok || link != "" {
		t.Fatalf("%q %v", link, ok)
	}
	if msg := APIError(noLink, "403 Forbidden"); !strings.Contains(msg, "open the Antigravity app") || strings.Contains(msg, "https://") {
		t.Fatal(msg)
	}

	// a link that isn't https isn't passed on
	if link, ok := Verification([]byte(`{"error":{"details":[{"reason":"VALIDATION_REQUIRED","metadata":{"validation_url":"javascript:alert(1)"}}]}}`)); !ok || link != "" {
		t.Fatalf("%q %v", link, ok)
	}
	// any other 403 is left as it was
	other := []byte(`{"error":{"code":403,"message":"The caller does not have permission","status":"PERMISSION_DENIED"}}`)
	if _, ok := Verification(other); ok || APIError(other, "") != "The caller does not have permission" {
		t.Fatal(APIError(other, ""))
	}
}
