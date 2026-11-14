package provider

import (
	"strings"
	"testing"
)

// TestClaudeSignedOutKeepsWhatItParsed: claudeSignedOut and
// claudeLoginVersion run for every saved Claude account on each Providers
// poll, and read a sign-in they have read before without parsing it again,
// while a sign-in changed since is read anew.
func TestClaudeSignedOutKeepsWhatItParsed(t *testing.T) {
	auth := []byte(`{"claudeAiOauth":{"accessToken":"sk-ant-oat01-` + strings.Repeat("a", 100) +
		`","refreshToken":"sk-ant-ort01-b","expiresAt":1,"scopes":["user:inference","user:profile"],"subscriptionType":"max"},"mcpOAuth":{"x":{"accessToken":"m"}}}`)
	l := savedLogin{Agent: "claude", User: "a@example.com", Auth: auth, Lapsed: claudeAuthLapse, Refused: "old"}
	parsed := allocatedBy(func() {
		c, _ := parseClaudeCredentials(auth)
		_ = c.version()
		_, _ = parseClaudeCredentials(auth)
	})
	read := allocatedBy(func() { _ = claudeSignedOut(l) })
	if read*4 > parsed {
		t.Fatalf("claudeSignedOut allocated %d bytes, parsing the sign-in %d", read, parsed)
	}
	if got := claudeSignedOut(l); got != "" {
		t.Fatalf("a credential other than the refused one reads %q", got)
	}
	c, _ := parseClaudeCredentials(auth)
	l.Refused = c.version()
	if got := claudeSignedOut(l); got != claudeAuthLapse {
		t.Fatalf("the refused credential reads %q", got)
	}
	// signed in again: its new credential isn't the one refused
	l.Auth = []byte(strings.Replace(string(auth), "ort01-b", "ort01-c", 1))
	if got := claudeSignedOut(l); got != "" {
		t.Fatalf("a credential changed since reads %q", got)
	}
	l.Lapsed, l.Refused, l.Auth = "", "", []byte(`{"claudeAiOauth":{"accessToken":""}}`)
	if got := claudeSignedOut(l); got != claudeGoneLapse {
		t.Fatalf("an emptied sign-in reads %q", got)
	}
}
