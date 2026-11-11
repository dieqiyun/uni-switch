package pluginrt

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"testing"
)

func home(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("UNI_SWITCH_PORTABLE", "")
}

var three = []Manifest{
	{ID: "core", Essential: true, Defaults: map[string]any{"n": 1, "deep": map[string]any{"a": 1, "b": 2}}},
	{ID: "extra"},
	{ID: "used", InUse: func() (int, error) { return 2, nil }},
}

func byID(cs []Composed) map[string]Composed {
	m := map[string]Composed{}
	for _, c := range cs {
		m[c.ID] = c
	}
	return m
}

// The layers, field by field: the default, the preset, the user's rows,
// and in use over them all; each value says where it came from.
func TestCompose(t *testing.T) {
	no := false
	cs := byID(Compose(three, File{Preset: PresetEssentials, Plugins: []Row{
		{ID: "core", Config: map[string]any{"deep": map[string]any{"b": 3}}},
		{ID: "used", Enabled: &no},
	}}))
	if c := cs["core"]; !c.Enabled || c.Source != FromDefault {
		t.Errorf("core: %+v", c)
	}
	if c := cs["core"]; !reflect.DeepEqual(c.Config, map[string]any{"n": 1, "deep": map[string]any{"a": 1, "b": 3}}) || c.Sources["deep"] != FromUser || c.Sources["n"] != FromDefault {
		t.Errorf("core's config: %+v %+v", c.Config, c.Sources)
	}
	if c := cs["extra"]; c.Enabled || c.Source != FromPreset {
		t.Errorf("extra under Essentials: %+v", c)
	}
	if c := cs["used"]; !c.Enabled || c.Chosen || c.Source != FromInUse || c.InUse != 2 {
		t.Errorf("used, turned off: %+v", c)
	}
	// All, or no file at all: every one on
	for _, f := range []File{{Preset: PresetAll}, {}} {
		for _, c := range Compose(three, f) {
			if !c.Enabled {
				t.Errorf("%q: %s off", f.Preset, c.ID)
			}
		}
	}
}

// Every install is written All; the file there is never written over by
// Settle once settled; Set keeps the other rows and the preset, and the
// file it replaces is kept beside it.
func TestSettleAndSet(t *testing.T) {
	home(t)
	if wrote, err := Settle(); err != nil || !wrote {
		t.Fatalf("settle: %v %v", wrote, err)
	}
	if f, there, err := Load(); err != nil || !there || f.Preset != PresetAll || f.Settled != settled {
		t.Fatalf("after settle: %+v %v %v", f, there, err)
	}
	for _, c := range Compose(three, File{Preset: PresetAll}) {
		if !c.Enabled {
			t.Fatalf("a new install: %s off", c.ID)
		}
	}
	if wrote, _ := Settle(); wrote {
		t.Fatal("settled over a plugins.yml there")
	}
	if err := SetPreset(PresetEssentials); err != nil {
		t.Fatal(err)
	}
	if err := Set("extra", true); err != nil {
		t.Fatal(err)
	}
	if err := Set("used", false); err != nil {
		t.Fatal(err)
	}
	if err := Set("extra", false); err != nil {
		t.Fatal(err)
	}
	f, _, err := Load()
	if err != nil || f.Preset != PresetEssentials || len(f.Plugins) != 2 || *f.Plugins[0].Enabled || f.Plugins[0].ID != "extra" {
		t.Fatalf("after sets: %+v %v", f, err)
	}
	if _, err := os.Stat(Path() + ".bak"); err != nil {
		t.Fatalf("no .bak: %v", err)
	}
	// Essentials the user picked is theirs: the next start keeps it
	if wrote, _ := Settle(); wrote {
		t.Fatal("settled over the user's own Essentials")
	}
	if f, _, _ := Load(); f.Preset != PresetEssentials {
		t.Fatalf("the user's Essentials: %+v", f)
	}
}

// The Essentials a new install was written before settled 1 becomes
// All on the next start, and what the user turned off stays off.
func TestSettleBringsUpAnEssentialsInstall(t *testing.T) {
	home(t)
	if err := os.MkdirAll(filepath.Dir(Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	old := "preset: essentials\nplugins:\n  - id: extra\n    enabled: false\n"
	if err := os.WriteFile(Path(), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if wrote, err := Settle(); err != nil || !wrote {
		t.Fatalf("settle: %v %v", wrote, err)
	}
	f, _, err := Load()
	if err != nil || f.Preset != PresetAll || f.Settled != settled {
		t.Fatalf("after settle: %+v %v", f, err)
	}
	for _, c := range Compose(three, f) {
		if c.Enabled != (c.ID != "extra") {
			t.Fatalf("%s: %+v", c.ID, c)
		}
	}
	if b, _ := os.ReadFile(Path() + ".bak"); string(b) != old {
		t.Fatalf("the old file not kept: %q", b)
	}
}

// A plugins.yml that doesn't parse is an error, never an empty file, and
// is never written over.
func TestBrokenFileIsKept(t *testing.T) {
	home(t)
	if err := os.MkdirAll(filepath.Dir(Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	broken := []byte("plugins: [ {id: x\n")
	if err := os.WriteFile(Path(), broken, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(); err == nil {
		t.Fatal("a broken file read")
	}
	if err := Set("x", true); err == nil {
		t.Fatal("a broken file written")
	}
	if wrote, _ := Settle(); wrote {
		t.Fatal("a broken file settled over")
	}
	if b, _ := os.ReadFile(Path()); string(b) != string(broken) {
		t.Fatalf("the broken file changed: %q", b)
	}
	if err := os.WriteFile(Path(), []byte("preset: slim\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(); err == nil || !strings.Contains(err.Error(), "slim") {
		t.Fatalf("an unknown preset: %v", err)
	}
}

func TestUniSwitchRuntimeUsesCompiledModules(t *testing.T) {
	home(t)
	Register(Manifest{ID: "static-test"})
	if err := Set("static-test", false); err != nil {
		t.Fatal(err)
	}
	list, err := Refresh()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		if c.ID == "static-test" {
			if !c.Enabled || !Running().Active(c.ID) {
				t.Fatal("old extension configuration disabled a compiled module")
			}
			return
		}
	}
	t.Fatal("compiled module missing")
}
