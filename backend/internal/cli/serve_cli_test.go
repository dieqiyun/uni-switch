package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// Legacy sharing settings and unsafe addresses cannot change the local banner.
func TestKeyNote(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, c := range []struct{ addr, want string }{
		{"", "only listens on localhost"},
		{"127.0.0.1:3426", "only listens on localhost"},
		{"localhost:3426", "only listens on localhost"},
		{"[::1]:3426", "only listens on localhost"},
		{"0.0.0.0:3425", "only listens on localhost"},
		{":3425", "only listens on localhost"},
		{"192.168.1.5:3425", "only listens on localhost"},
	} {
		t.Setenv("UNI_SWITCH_ADDR", c.addr)
		if n := keyNote(); !strings.Contains(n, c.want) || strings.Contains(n, "anyone") {
			t.Errorf("UNI_SWITCH_ADDR=%q: %q", c.addr, n)
		}
	}
	for _, key := range []string{"", "sk-uni-switch-k"} {
		if err := settings.Save(settings.Settings{LANKey: key}); err != nil {
			t.Fatal(err)
		}
		for _, addr := range []string{"", "0.0.0.0:3425"} {
			t.Setenv("UNI_SWITCH_ADDR", addr)
			if n := keyNote(); !strings.Contains(n, "only listens on localhost") {
				t.Error("legacy configuration:", n)
			}
		}
	}
}
