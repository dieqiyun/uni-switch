package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A model of DeepSeek's own picked in dsh's /model, which dsh saves in its
// settings over the patch list, leaves dsh connected: uni-switch's route is
// still in its list, beside DeepSeek's (Fate on Discord: DSH went back to
// Not connected). Disconnect then takes the route, uni-switch's start and its
// key out, and the model picked in dsh stays.
func TestDshOwnPickStaysConnected(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("DSH_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := provider.Save(provider.Provider{ID: "glm", Name: "GLM", Chat: "https://glm.test/v1", Key: "k", Models: []string{"glm-5.3"}}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".dsh")
	template := "# Your patch layer for this dsh profile.\n[]\n"
	web := filepath.Join(dir, "profiles", "web", "cordis.patch.yml")
	os.MkdirAll(filepath.Dir(web), 0o755)
	os.WriteFile(web, []byte(template), 0o644)
	settings := filepath.Join(dir, "settings.yaml")
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }

	a := dsh(home)
	if a.Wired() {
		t.Fatal("wired before anything")
	}
	if err := a.Pick("model", "uni-switch/glm/glm-5.3"); err != nil {
		t.Fatal(err)
	}
	if !a.Wired() {
		t.Fatal("not wired on uni-switch's model")
	}

	// picked in dsh: DeepSeek's own
	os.WriteFile(settings, []byte("ui:\n  theme: dark\nagent-default-model:\n  provider: deepseek-official\n  model: deepseek-v4-flash\n"), 0o644)
	if got := a.Field("model").Get(); got != "deepseek-v4-flash" {
		t.Fatalf("model: %q", got)
	}
	if !a.Wired() {
		t.Fatal("dsh went back to Not connected with uni-switch's route still in its list")
	}
	if d := a.Drift(); d != nil {
		t.Fatalf("drift: %+v", d)
	}

	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if a.Wired() {
		t.Fatal("still wired after Disconnect")
	}
	if got := read(web); got != template {
		t.Fatalf("the patch list after Disconnect:\n%q", got)
	}
	if strings.Contains(read(filepath.Join(dir, ".env")), dshKeyRef) {
		t.Fatal("the key outlived the route")
	}
	if s := read(settings); !strings.Contains(s, "model: deepseek-v4-flash") || !strings.Contains(s, "theme: dark") {
		t.Fatalf("the pick made in dsh was lost:\n%s", s)
	}
	if got := a.Field("model").Get(); got != "deepseek-v4-flash" {
		t.Fatalf("model after Disconnect: %q", got)
	}
}

// Since 0.2 dsh keeps no pick in settings.yaml: its /model, and the one-time
// import that renames a settings.yaml to settings.yaml.imported, write the
// config of the profile's last agent-default-model entry — uni-switch's, mark
// and all (#1392). The entry below is as dsh 0.2.0-rc.2 left it after
// importing a settings.yaml whose pick was DeepSeek's own. Disconnect takes
// uni-switch's route and key out and leaves that pick, not the entry uni-switch
// stashed from before it was connected.
func TestDshPickSavedIntoUniSwitchsEntryOutlivesDisconnect(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("DSH_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro", "flash"}}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".dsh")
	head := "# Your patch layer for this dsh profile, applied after every bundle layer:\n"
	before := head + "- id: agent-default-model\n  name: '@deepseek-ai/dsh-agent-default-model'\n  config:\n    provider: deepseek-official\n    model: deepseek-v4-pro\n"
	headless := filepath.Join(dir, "profiles", "headless", "cordis.patch.yml")
	os.MkdirAll(filepath.Dir(headless), 0o755)
	os.WriteFile(headless, []byte(before), 0o644)
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }

	a := dsh(home)
	if err := a.Pick("model", "uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	// dsh 0.2.0-rc.2's ConfigEditor.edit: the last agent-default-model
	// entry's config replaced, its id line (and uni-switch's mark) kept
	s := read(headless)
	ours := "- id: agent-default-model # uni-switch\n  config:\n    provider: uni-switch\n    model: \"deepseek/pro\"\n"
	if !strings.Contains(s, ours) {
		t.Fatalf("uni-switch's start:\n%s", s)
	}
	s = strings.Replace(s, ours, "- id: agent-default-model # uni-switch\n  config:\n    provider: deepseek-official\n    model: deepseek-v4-flash\n", 1)
	os.WriteFile(headless, []byte(s), 0o600)
	os.WriteFile(filepath.Join(dir, "settings.yaml.imported"), []byte("agent-default-model:\n  provider: deepseek-official\n  model: deepseek-v4-flash\n"), 0o600)

	if got := a.Field("model").Get(); got != "deepseek-v4-flash" {
		t.Fatalf("model picked in dsh: %q", got)
	}
	if !a.Wired() {
		t.Fatal("not connected with uni-switch's route still in the list")
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	s = read(headless)
	if a.Wired() || strings.Contains(s, "llm-pi-ai") || strings.Contains(s, dshMark) {
		t.Fatalf("uni-switch's wiring outlived Disconnect:\n%s", s)
	}
	if got := a.Field("model").Get(); got != "deepseek-v4-flash" || !strings.Contains(s, "model: deepseek-v4-flash") || strings.Contains(s, "deepseek-v4-pro") {
		t.Fatalf("the pick made in dsh was lost (model %q):\n%s", got, s)
	}
	if strings.Contains(read(filepath.Join(dir, ".env")), dshKeyRef) {
		t.Fatal("the key outlived the route")
	}
	// the user back on uni-switch and out again: nothing of the old stash returns
	if err := a.Pick("model", "uni-switch/deepseek/flash"); err != nil {
		t.Fatal(err)
	}
	if err := a.Pick("model", ""); err != nil {
		t.Fatal(err)
	}
	if s := read(headless); strings.Contains(s, dshMark) || !strings.Contains(s, "model: deepseek-v4-flash") {
		t.Fatalf("after uni-switch's model and back:\n%s", s)
	}
}
