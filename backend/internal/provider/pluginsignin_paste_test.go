package provider

import (
	"strings"
	"testing"
)

// The ports a sign-in page names as where it comes back to, as the
// community plugins' pages name them.
func TestLoopbackPorts(t *testing.T) {
	for page, want := range map[string]string{
		"https://app.devin.ai/auth/cli/continue?redirect_uri=http%3A%2F%2F127.0.0.1%3A5123%2Fcallback&state=s": "5123",
		"https://app.kiro.dev/signin?state=s&redirect_uri=http%3A%2F%2Flocalhost%3A3128&redirect_from=KiroIDE": "3128",
		"https://www.trae.ai/authorization?auth_callback_url=http%3A%2F%2F127.0.0.1%3A6001%2Fauthorize":        "6001",
		"https://zed.dev/native_app_signin?native_app_port=7002&native_app_public_key=k":                       "7002",
		"https://zcode.z.ai/x?redirect_uri=https%3A%2F%2Fzcode.z.ai%2Fapp%2Foauth%2Flogin":                     "",
		"https://github.com/login/device": "",
	} {
		if got := strings.Join(loopbackPorts(page), ","); got != want {
			t.Errorf("loopbackPorts(%s) = %q, want %q", page, got, want)
		}
	}
	if PluginPastesCallback("commandcode-plan", "https://commandcode.ai/studio/auth/cli?callback=http%3A%2F%2F127.0.0.1%3A4000%2Fcallback") {
		t.Error("Command Code's page posts its key: there is no address to paste")
	}
}
