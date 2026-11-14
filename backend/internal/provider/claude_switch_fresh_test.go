package provider

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 0xAncientTwo on X: the Claude subscription was signed out again and again.
// A switch saves the account it switches away from with the credentials
// Claude Code has for it now: one Claude Code refreshed a moment ago (its
// refresh token rotated, the old one dead) is saved as refreshed, not as
// uni-switch's look at the credentials up to 10 s before, which Anthropic then
// refuses when the account is used again.
func TestClaudeSwitchSavesTheFreshCredential(t *testing.T) {
	home := claudeHome(t)
	cred := claudeSignIn(t, home, time.Now().Add(time.Hour))
	profile := filepath.Join(home, ".claude.json")
	writeFile(t, profile, map[string]any{"oauthAccount": map[string]any{"emailAddress": "a@example.com", "accountUuid": "u-a"}})
	rememberLogins(true)
	b := func(rt string) {
		writeFile(t, cred, map[string]any{"claudeAiOauth": map[string]any{"accessToken": "sk-ant-oat01-" + rt, "refreshToken": "sk-ant-ort01-" + rt,
			"expiresAt": time.Now().Add(time.Hour).UnixMilli(), "subscriptionType": "pro"}})
	}
	b("b")
	writeFile(t, profile, map[string]any{"oauthAccount": map[string]any{"emailAddress": "b@example.com", "accountUuid": "u-b"}})
	forgetClaudeCredential()
	rememberLogins(true) // uni-switch has looked at b's credentials just now

	b("b2") // Claude Code refreshes b's sign-in
	old := ClaudeHandedOver
	defer func() { ClaudeHandedOver = old }()
	handed := ""
	ClaudeHandedOver = func(user string) { handed = user }
	if err := SwitchLogin("claude", "a@example.com"); err != nil {
		t.Fatal(err)
	}
	// the gateway is told, to let go of its runs in a's directory
	if handed != "a@example.com" {
		t.Fatalf("handed over %q", handed)
	}
	for _, l := range readLogins() {
		if l.Agent == "claude" && l.User == "b@example.com" {
			if !strings.Contains(string(l.Auth), "sk-ant-ort01-b2") {
				t.Fatalf("b saved with a refresh token Claude Code had replaced: %s", l.Auth)
			}
			return
		}
	}
	t.Fatal("b not saved")
}
