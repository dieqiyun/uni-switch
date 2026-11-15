package cli

import (
	"bytes"
	"os"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func TestUpdateAutoKeepsCorruptSettings(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"truncated", `{"theme":"dark","githubToken":"SYNTHETIC_PRIVATE_TOKEN"`},
		{"theme type", `{"theme":7,"githubToken":"SYNTHETIC_PRIVATE_TOKEN"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groupsHome(t)
			if err := os.MkdirAll(settings.Dir(), 0o755); err != nil {
				t.Fatal(err)
			}
			original := []byte(tc.body)
			if err := os.WriteFile(settings.Path(), original, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := updateCmd([]string{"update", "auto", "off"}); err == nil {
				t.Error("the command reported success with corrupt settings")
			}
			if after, err := os.ReadFile(settings.Path()); err != nil || !bytes.Equal(after, original) {
				t.Errorf("the command overwrote the corrupt settings: %v", err)
			}
		})
	}
}

func TestUpdateAutoRecoversEmptySettings(t *testing.T) {
	for _, body := range []string{"", " \t\r\n", "\ufeff \t\r\n"} {
		t.Run(body, func(t *testing.T) {
			groupsHome(t)
			if err := os.MkdirAll(settings.Dir(), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(settings.Path(), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := updateCmd([]string{"update", "auto", "off"}); err != nil {
				t.Fatal(err)
			}
			if !settings.Load().NoAutoUpdate {
				t.Fatal("the update preference was not saved")
			}
		})
	}
}

func TestUpdateAutoWithBOMSettings(t *testing.T) {
	groupsHome(t)
	if err := os.MkdirAll(settings.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	const token = "SYNTHETIC_PRIVATE_TOKEN"
	body := "\ufeff" + `{"theme":"dark","proxy":"direct","lang":"zh","githubToken":"` + token + `"}`
	if err := os.WriteFile(settings.Path(), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := updateCmd([]string{"update", "auto", "off"}); err != nil {
		t.Fatal(err)
	}
	got := settings.Load()
	if !got.NoAutoUpdate || got.Theme != "dark" || got.Proxy != "direct" || got.Lang != "zh" || got.GitHubToken != token {
		t.Fatal("the update command lost fields from the BOM file")
	}
}

// uni-switch update auto turns the app's own update checks off and on and sets
// how often they run, from the presets only (#472).
func TestUpdateAutoCmd(t *testing.T) {
	groupsHome(t)
	if err := updateCmd([]string{"update", "auto"}); err != nil {
		t.Fatal(err)
	}
	if s := settings.Load(); s.NoAutoUpdate || s.UpdateEvery != 360 {
		t.Fatalf("default: off %v, every %d", s.NoAutoUpdate, s.UpdateEvery)
	}
	if err := updateCmd([]string{"update", "auto", "off", "30m"}); err != nil {
		t.Fatal(err)
	}
	if s := settings.Load(); !s.NoAutoUpdate || s.UpdateEvery != 30 {
		t.Fatalf("off 30m: off %v, every %d", s.NoAutoUpdate, s.UpdateEvery)
	}
	if err := updateCmd([]string{"update", "auto", "on"}); err != nil {
		t.Fatal(err)
	}
	if s := settings.Load(); s.NoAutoUpdate || s.UpdateEvery != 30 {
		t.Fatalf("on: off %v, every %d", s.NoAutoUpdate, s.UpdateEvery)
	}
	for _, a := range []string{"1h", "24h", "6h"} {
		if err := updateCmd([]string{"update", "auto", a}); err != nil {
			t.Fatal(err)
		}
		if got := settings.Load().UpdateEvery; got != updateEveries[a] {
			t.Fatalf("%s: every %d", a, got)
		}
	}
	// notify: a version found waits for a click to be downloaded (#2296),
	// the other choices left as they are; download puts it back
	if err := updateCmd([]string{"update", "auto", "notify"}); err != nil {
		t.Fatal(err)
	}
	if s := settings.Load(); !s.NoAutoDownload || s.NoAutoUpdate || s.UpdateEvery != 360 {
		t.Fatalf("notify: download held %v, off %v, every %d", s.NoAutoDownload, s.NoAutoUpdate, s.UpdateEvery)
	}
	if err := updateCmd([]string{"update", "auto", "download"}); err != nil {
		t.Fatal(err)
	}
	if s := settings.Load(); s.NoAutoDownload {
		t.Fatal("download: still held")
	}
	if err := updateCmd([]string{"update", "auto", "2h"}); err == nil {
		t.Fatal("2h taken")
	}
	if got := settings.Load().UpdateEvery; got != 360 {
		t.Fatalf("2h refused, yet every %d", got)
	}
}
