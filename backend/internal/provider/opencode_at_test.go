package provider

import "testing"

// IsOpenCode reads its host as OpenCodeURL reads a URL, with no URL
// parsed for it: it is asked for every model of the providers' page.
func TestOpenCodeAtReadsTheHostAsAURLWould(t *testing.T) {
	for _, h := range []string{
		"opencode.ai", "OpenCode.AI", "opencode.ai.", "opencode.ai:443",
		"zen.opencode.ai", "api.opencode.ai:8443", "u:p@opencode.ai",
		"u@zen.opencode.ai:1", "notopencode.ai", "opencode.ai.evil.com",
		"evil.com", "evil.com@", "opencode.ai@evil.com", "[::1]:443",
		"[::1]", "127.0.0.1:3425", "", "localhost",
	} {
		if got, want := openCodeAt(h), OpenCodeURL("//"+h); got != want {
			t.Errorf("%q: %v, a URL says %v", h, got, want)
		}
	}
	if n := testing.AllocsPerRun(100, func() { openCodeAt("u@zen.opencode.ai:8443") }); n != 0 {
		t.Fatalf("a look allocated %v times, want 0", n)
	}
}
