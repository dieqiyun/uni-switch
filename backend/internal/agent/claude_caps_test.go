package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// TestClaudeCapabilitiesSync: a model added to the catalog later reaches
// Claude Code's capabilities with the rest of the catalog.
func TestClaudeCapabilitiesSync(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := provider.Save(provider.Provider{ID: "v", Name: "V", Chat: "http://127.0.0.1:1/v1", Key: "k", Models: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelEfforts("v/a", []string{"high", "xhigh"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".claude", "settings.json")
	writeFile(t, path, `{}`)
	a := claude(home)
	if err := a.Field("model").Set("v/a"); err != nil {
		t.Fatal(err)
	}
	caps := func() string { v, _ := edit.GetJSON(path, "env."+claudeCapsEnv); return v }
	if caps() != "v/a=effort,xhigh_effort" {
		t.Fatalf("v/a: %q", caps())
	}
	if err := provider.Save(provider.Provider{ID: "w", Name: "W", Chat: "http://127.0.0.1:1/v1", Key: "k", Models: []string{"b"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelEfforts("w/b", []string{"low", "max"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if caps() != "v/a=effort,xhigh_effort;w/b=effort,xhigh_effort,max_effort" {
		t.Fatalf("after w/b was added: %q", caps())
	}
}

// A model picked for two of Claude Desktop's tiers is listed for the
// second by an id of its own (gateway.DesktopTierAliases); Claude Code is
// told that id's reasoning levels too, as it is the first's.
func TestClaudeCapabilitiesDesktopSharedTier(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := provider.Save(provider.Provider{ID: "v", Name: "V", Chat: "http://127.0.0.1:1/v1", Key: "k", Models: []string{"Deep", "mid"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelEfforts("v/Deep", []string{"low", "medium", "high", "xhigh", "max"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".claude", "settings.json")
	writeFile(t, path, `{"theme":"dark"}`)
	if err := claude(home).Field("model").Set("v/mid"); err != nil {
		t.Fatal(err)
	}
	d := claudeDesktop(home)
	for _, kv := range [][2]string{{"provider", "uni-switch"}, {"sonnet", "v/Deep"}, {"haiku", "v/Deep"}} {
		if err := d.Field(kv[0]).Set(kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := claude(home).Sync(); err != nil {
		t.Fatal(err)
	}
	caps, _ := edit.GetJSON(path, "env."+claudeCapsEnv)
	segs := map[string]string{}
	for _, s := range strings.Split(caps, ";") {
		if k, v, ok := strings.Cut(s, "="); ok {
			segs[k] = v
		}
	}
	again := gateway.DesktopTierAliases()["v/Deep"]
	if len(again) != 1 || !strings.HasPrefix(again[0], "mythos-uni-switch-") {
		t.Fatalf("haiku's id for Deep: %v", again)
	}
	if segs[again[0]] != "effort,xhigh_effort,max_effort" {
		t.Fatalf("haiku's %s: %q (%s)", again[0], segs[again[0]], caps)
	}
}
