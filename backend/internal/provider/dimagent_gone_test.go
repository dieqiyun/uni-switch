package provider

import (
	"os"
	"strings"
	"testing"
)

// A DimAgent account signed in before uni-switch dropped DimAgent is neither
// listed nor kept: the next write of logins.json leaves it out.
func TestDimAgentLoginDropped(t *testing.T) {
	signIn(t)
	writeFile(t, loginsPath(), []map[string]any{
		{"agent": "dimagent", "user": "u1", "on": true, "auth": map[string]any{"accessToken": "x", "refreshToken": "y"}},
		{"agent": "grok", "user": "g@x", "auth": map[string]any{}},
	})
	for _, l := range readLogins() {
		if l.Agent == "dimagent" {
			t.Fatalf("read %+v", l)
		}
	}
	if err := writeLogins(readLogins()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(loginsPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "dimagent") || !strings.Contains(string(b), "g@x") {
		t.Fatalf("logins.json:\n%s", b)
	}
}
