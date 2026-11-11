package provider

import (
	"net/url"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// uni-switch's own gateway is on the port Settings has (Magic_zero on
// Discord): an app's entry there is uni-switch's, not one to import, and one
// on 3425 is another program's once uni-switch has moved off it.
func TestGatewayURLOnSettingsPort(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("UNI_SWITCH_ADDR", "")
	is := func(u string) bool { p, _ := url.Parse(u); return gatewayURL(p) }
	if !is("http://127.0.0.1:3425/v1") || is("http://127.0.0.1:3591/v1") {
		t.Fatal("the default port")
	}
	s := settings.Load()
	s.Port = 3591
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	if !is("http://127.0.0.1:3591/v1") || !is("http://localhost:3591") || is("http://127.0.0.1:3425/v1") || is("http://10.0.0.2:3591") {
		t.Fatal("Settings' port")
	}
	t.Setenv("UNI_SWITCH_ADDR", "127.0.0.1:3426")
	if !is("http://127.0.0.1:3426") {
		t.Fatal("UNI_SWITCH_ADDR's port")
	}
}
