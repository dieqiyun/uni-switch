package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// codexDistroHome is a distro's / in a temp dir with a user whose ~/.codex
// holds config (none when ""); uni-switch's own HOME is another temp dir.
func codexDistroHome(t *testing.T, config string) (root, home string) {
	t.Helper()
	syncHome(t)
	root = t.TempDir()
	home = filepath.Join(root, "home", "me")
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	if config != "" {
		os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(config), 0o644)
	}
	return root, home
}

// A Codex in a running distro, routed through uni-switch, stands in for a model
// it names that uni-switch doesn't serve, as this machine's does and a Claude
// Code in the distro does (wslClaudeStandIn): the first distro running now
// whose config.toml has uni-switch as its provider. One whose config isn't
// uni-switch's has none.
func TestWSLCodexStandIn(t *testing.T) {
	root, home := codexDistroHome(t, "")
	cfg := filepath.Join(home, ".codex", "config.toml")
	fakeWSL(t, "Debian\r\n", "Debian\r\n", map[string]string{
		"Debian": "home:/home/me\ndir:.codex\nroute:default via 172.20.0.1 dev eth0\n",
	}, map[string]string{"Debian": root})
	ds := wslDistros()
	if len(ds) != 1 || !ds[0].Running {
		t.Fatalf("%+v", ds)
	}
	os.WriteFile(cfg, []byte("model_provider = \"uni-switch\"\nmodel = \"relay/main\"\n"), 0o644)
	if got := wslCodexStandIn(); got != "relay/main" {
		t.Fatalf("stand-in: %q", got)
	}
	// StandIn asks the distro only on Windows, and only when this
	// machine's Codex names none (its HOME here has no config.toml)
	want := ""
	if runtime.GOOS == "windows" {
		want = "relay/main"
	}
	if got := StandIn("codex", "codex-unserved"); got != want {
		t.Fatalf("StandIn on %s: %q, want %q", runtime.GOOS, got, want)
	}
	os.WriteFile(cfg, []byte("model = \"gpt-5.5\"\n"), 0o644)
	if got := wslCodexStandIn(); got != "" {
		t.Fatalf("not uni-switch's provider: %q", got)
	}
}

// A distro that has stopped since uni-switch saw it running is never opened for
// a stand-in, however wired its config on disk is (da5f7a811: a `wsl
// --shutdown` is not undone by background work).
func TestWSLCodexStandInSkipsStopped(t *testing.T) {
	root, home := codexDistroHome(t, "model_provider = \"uni-switch\"\nmodel = \"stopped/model\"\n")
	cfg := filepath.Join(home, ".codex", "config.toml")
	before, err := os.Stat(cfg)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]*distro{
		"Debian": {Name: "Debian", Home: "/home/me", Root: root, Has: map[string]bool{"dir:.codex": true},
			Values: map[string]string{"model": "stopped/model"}},
	}
	b, err := json.Marshal(seen)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(wslStatePath()), 0o755)
	os.WriteFile(wslStatePath(), b, 0o600)
	other := t.TempDir()
	fakeWSL(t, "Debian\r\nOther\r\n", "Other\r\n", map[string]string{
		"Other": "home:/home/o\ndir:.codex\n",
	}, map[string]string{"Other": other})
	ds := wslDistros()
	if len(ds) != 2 || ds[0].Name != "Debian" || ds[0].Running || ds[1].Name != "Other" || !ds[1].Running {
		t.Fatalf("%+v", ds)
	}
	if got := wslCodexStandIn(); got != "" {
		t.Fatalf("stand-in from a stopped distro: %q", got)
	}
	after, err := os.Stat(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) || readFile(cfg) != "model_provider = \"uni-switch\"\nmodel = \"stopped/model\"\n" {
		t.Fatal("a stopped distro's config was touched")
	}
}
