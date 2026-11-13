package gateway

import (
	"os"
	"path/filepath"

	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// plugged has plugins.json list a plugin that gives one search API, as the
// host notes it after a load, with no host.
func plugged(t *testing.T, spec string, off bool) {
	t.Helper()
	offs := ""
	if off {
		offs = `,"off":true`
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(settings.Dir(), name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(settings.Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	write("plugins.json", `{"plugins":[{"spec":"`+spec+`"`+offs+`}]}`)
	write("plugin-searches.json", `{"`+spec+`":[{"id":"mojeek","name":"Mojeek","keysURL":"https://www.mojeek.com/services/api/"}]}`)
}
