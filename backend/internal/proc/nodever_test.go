package proc

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// fnmWin lays fnm out at root as it is on Windows (#2685): each version's
// programs in node-versions\<v>\installation itself, no bin, and
// aliases\default leading to def's installation folder (a junction there,
// a symlink here; a folder of its own where neither can be made). "" for
// def leaves the alias out.
func fnmWin(t *testing.T, root, def string, versions ...string) {
	t.Helper()
	for _, v := range versions {
		inst := filepath.Join(root, "node-versions", v, "installation")
		if err := os.MkdirAll(inst, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{"node.exe", "npx.cmd"} {
			if err := os.WriteFile(filepath.Join(inst, f), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if def == "" {
		return
	}
	alias := filepath.Join(root, "aliases", "default")
	if err := os.MkdirAll(filepath.Dir(alias), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "node-versions", def, "installation"), alias); err != nil {
		if err := os.MkdirAll(alias, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// fnm's Windows folders in the order a terminal reaches them: the default
// alias's, as it stands (where the reporter's npx.cmd is), then each
// installation folder by version number, v24 before v9.
func TestFnmWinBins(t *testing.T) {
	root := filepath.Join(t.TempDir(), "fnm")
	fnmWin(t, root, "v20.18.0", "v9.11.2", "v24.19.0", "v20.18.0")
	if err := os.MkdirAll(filepath.Join(root, "node-versions", "v25.0.0"), 0o755); err != nil {
		t.Fatal(err) // being installed: no installation folder yet
	}
	inst := func(v string) string { return filepath.Join(root, "node-versions", v, "installation") }
	want := []string{filepath.Join(root, "aliases", "default"), inst("v24.19.0"), inst("v20.18.0"), inst("v9.11.2")}
	if got := fnmWinBins(root); !slices.Equal(got, want) {
		t.Fatalf("fnmWinBins = %q\nwant %q", got, want)
	}
	if got, want := fnmWinUse(root), filepath.Join(root, "aliases", "default"); got != want {
		t.Fatalf("fnmWinUse = %q, want the default alias %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(fnmWinUse(root), "node.exe")); err != nil {
		t.Fatalf("no node.exe in the default alias's folder: %v", err)
	}
}

// With no default alias a terminal's fnm has no Node of its own; uni-switch
// takes the highest installed, as it does for nvm. A default that leads
// nowhere is fnm's "system" alias (Z:\_fnm_\...): the system's Node, which
// PATH has already, so none.
func TestFnmWinUseWithoutDefault(t *testing.T) {
	root := filepath.Join(t.TempDir(), "fnm")
	fnmWin(t, root, "", "v9.11.2", "v22.12.0")
	if got, want := fnmWinUse(root), filepath.Join(root, "node-versions", "v22.12.0", "installation"); got != want {
		t.Fatalf("no default: fnmWinUse = %q, want %q", got, want)
	}
	alias := filepath.Join(root, "aliases", "default")
	if err := os.MkdirAll(filepath.Dir(alias), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "_fnm_", "Nothing", "installation"), alias); err != nil {
		t.Skipf("no symlink here: %v", err)
	}
	if got := fnmWinUse(root); got != "" {
		t.Fatalf(`"system" default: fnmWinUse = %q, want none`, got)
	}
}

// fnm's root on Windows: FNM_DIR, else %APPDATA%\fnm (etcetera's
// data_dir), else the legacy ~\.fnm; the folder a terminal gets is the
// default alias's in the first of them. Given as Windows sets them, under
// a temp folder.
func TestFnmWinPath(t *testing.T) {
	home := t.TempDir()
	appdata := filepath.Join(home, "AppData", "Roaming")
	roaming := filepath.Join(appdata, "fnm")
	if got := fnmWinPath(home, appdata, ""); got != nil {
		t.Fatalf("no fnm: fnmWinPath = %q", got)
	}
	fnmWin(t, roaming, "v24.19.0", "v24.19.0")
	if got, want := fnmWinPath(home, appdata, ""), []string{filepath.Join(roaming, "aliases", "default")}; !slices.Equal(got, want) {
		t.Fatalf("APPDATA's fnm: fnmWinPath = %q, want %q", got, want)
	}
	// APPDATA unset or relative: the home's AppData\Roaming, where etcetera
	// puts it
	if got := fnmWinRoots(home, "AppData", ""); !slices.Equal(got, []string{roaming}) {
		t.Fatalf("relative APPDATA: fnmWinRoots = %q, want %q", got, []string{roaming})
	}
	legacy := filepath.Join(home, ".fnm")
	fnmWin(t, legacy, "v18.0.0", "v18.0.0")
	moved := filepath.Join(home, "D", "fnm")
	fnmWin(t, moved, "v22.12.0", "v22.12.0")
	if got, want := fnmWinRoots(home, appdata, moved), []string{moved, roaming, legacy}; !slices.Equal(got, want) {
		t.Fatalf("fnmWinRoots = %q, want %q", got, want)
	}
	if got, want := fnmWinPath(home, appdata, moved), []string{filepath.Join(moved, "aliases", "default")}; !slices.Equal(got, want) {
		t.Fatalf("FNM_DIR: fnmWinPath = %q, want %q", got, want)
	}
}
