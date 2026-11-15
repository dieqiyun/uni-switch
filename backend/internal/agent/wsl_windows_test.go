package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/proc"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// On a real WSL: UNI_SWITCH_WSL_TEST_DISTRO names a throwaway distro with a
// ~/.codex of its own, which the test writes to.
func TestWSLCodexLive(t *testing.T) {
	name := os.Getenv("UNI_SWITCH_WSL_TEST_DISTRO")
	if name == "" {
		t.Skip("UNI_SWITCH_WSL_TEST_DISTRO not set")
	}
	// the real wsl.exe, before testenv's failing stand-in
	t.Setenv("PATH", filepath.Join(os.Getenv("SystemRoot"), "System32")+string(os.PathListSeparator)+os.Getenv("PATH"))
	names, _ := wslList()
	found := false
	for _, n := range names {
		found = found || n == name
	}
	if !found {
		t.Fatalf("%s not in %q", name, names)
	}
	home := t.TempDir()
	for _, k := range []string{"HOME", "USERPROFILE"} {
		t.Setenv(k, home)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	usedUp := codexUsedUp
	codexUsedUp = func() bool { return false }
	t.Cleanup(func() { codexUsedUp = usedUp })
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	ds := wslDistros()
	t.Logf("distros probed in %s", time.Since(start))
	var d *distro
	for i := range ds {
		t.Logf("%+v", ds[i])
		if ds[i].Name == name {
			d = &ds[i]
		}
	}
	if d == nil || !d.Has["dir:.codex"] {
		t.Fatalf("%s not probed or has no ~/.codex", name)
	}
	var a *Agent
	for _, x := range All() {
		if x.ID == "codex@wsl:"+name {
			a = x
		}
	}
	if a == nil || !a.Detected() {
		t.Fatal("no WSL agent in All()")
	}
	if f, err := Find(strings.ToLower(a.ID)); err != nil || f.ID != a.ID {
		t.Fatalf("find: %v", err)
	}
	cat := func(p string) string {
		b, _ := proc.Command("wsl.exe", "-d", name, "-e", "cat", p).Output()
		return string(b)
	}
	cfg := d.Home + "/.codex/config.toml"
	before := cat(cfg)
	if err := a.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	got := cat(cfg)
	t.Logf("set:\n%s", got)
	if !strings.Contains(got, `model = "fake/m1"`) || !strings.Contains(got, `base_url = "`+d.base()+`/v1"`) {
		t.Fatal("not written")
	}
	if !strings.Contains(before, "OPENAI_API_KEY") && !strings.Contains(got, `model_catalog_json = "`+d.Home+`/.codex/uni-switch-models.json"`) {
		t.Fatal("catalog path")
	}
	if !strings.Contains(cat(d.Home+"/.codex/uni-switch-models.json"), "fake/m1") {
		t.Fatal("catalog not seen inside WSL")
	}
	if c := a.Check(); c != "" {
		t.Fatal(c)
	}
	t.Log(a.Notice())
	if err := a.Fields[0].Set(""); err != nil {
		t.Fatal(err)
	}
	t.Logf("reset:\n%s", cat(cfg))
	t.Logf("gateway from WSL: %s (windows: %s)", d.base(), gateway.URL())
}
