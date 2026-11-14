package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// penguinAppPlist is the Info.plist electron-builder writes for
// PenguinHarness's desktop app, from its packages/desktop/electron-builder.yml
// (appId com.prismshadow.penguinharness, productName PenguinHarness) and
// package.json (0.2.17), cut to the keys around the ones read. Not read
// from a downloaded build.
const penguinAppPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>CFBundleDisplayName</key>
    <string>PenguinHarness</string>
    <key>CFBundleExecutable</key>
    <string>PenguinHarness</string>
    <key>CFBundleIdentifier</key>
    <string>com.prismshadow.penguinharness</string>
    <key>CFBundleName</key>
    <string>PenguinHarness</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>0.2.17</string>
    <key>CFBundleVersion</key>
    <string>0.2.17</string>
  </dict>
</plist>
`

// rankyurky_80676 on Discord (2026-10-27): PenguinHarness's desktop app on
// a Mac, and uni-switch didn't detect it. The app needs no penguin CLI (it
// links one only when the user lets it), so ~/.penguin with no CLI was
// taken for a leftover, and an app not opened yet had no ~/.penguin at
// all. With the app installed it is listed, its version shown, and no CLI
// install offered.
func TestPenguinDesktopAppDetected(t *testing.T) {
	judgeFor(t, "darwin")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PENGUIN_HOME", "")
	t.Setenv("PATH", t.TempDir()) // no penguin CLI
	apps, userApps := t.TempDir(), t.TempDir()
	was := appFolders
	appFolders = func() []string { return []string{apps, userApps} }
	t.Cleanup(func() { appFolders = was })
	a := penguin(home)
	// a name no machine has: proc.FindTool also looks where users' tools go
	a.Bin = "uni-switch-test-gone-penguin"
	offered := func() bool {
		for _, x := range installsOf([]*Agent{a}, "darwin", true) {
			if x.ID == "penguin" {
				return true
			}
		}
		return false
	}

	// nothing here
	if a.Detected() {
		t.Fatal("detected with nothing installed")
	}
	// the app in /Applications, never opened: no ~/.penguin yet
	installApp(t, apps, "PenguinHarness.app", penguinAppPlist)
	if !a.Detected() || a.CLIMissing() || offered() {
		t.Fatalf("app only: detected %v, missing %v, offered %v", a.Detected(), a.CLIMissing(), offered())
	}
	if c, ok := a.CLI(); !ok || c.App != "0.2.17" || c.Version != "" {
		t.Fatalf("the desktop app's version: %+v %v", c, ok)
	}
	// opened: its server made ~/.penguin/data, and that is no leftover
	if err := os.MkdirAll(filepath.Join(home, ".penguin", "data", "default_project"), 0o700); err != nil {
		t.Fatal(err)
	}
	if !a.Detected() || a.CLIMissing() || offered() {
		t.Fatalf("app opened: detected %v, missing %v, offered %v", a.Detected(), a.CLIMissing(), offered())
	}

	// another app of that name is not Penguin's: ~/.penguin without its
	// CLI is said again (#843)
	os.RemoveAll(filepath.Join(apps, "PenguinHarness.app"))
	installApp(t, userApps, "PenguinHarness.app", strings.Replace(penguinAppPlist, "com.prismshadow.penguinharness", "com.example.other", 1))
	if !a.CLIMissing() || !offered() {
		t.Fatalf("another bundle id: missing %v, offered %v", a.CLIMissing(), offered())
	}
	os.RemoveAll(filepath.Join(userApps, "PenguinHarness.app"))
	// in ~/Applications
	installApp(t, userApps, "PenguinHarness.app", penguinAppPlist)
	if a.CLIMissing() {
		t.Fatal("the app in ~/Applications not found")
	}
	os.RemoveAll(filepath.Join(userApps, "PenguinHarness.app"))

	// Windows: the per-user installer's PenguinHarness.exe
	exe := filepath.Join(apps, "PenguinHarness", "PenguinHarness.exe")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	if a.CLIMissing() || offered() {
		t.Fatalf("with the Windows app: missing %v, offered %v", a.CLIMissing(), offered())
	}

	// Penguin in a WSL distro is not the Windows app's
	if (&Agent{ID: "penguin@wsl:Ubuntu", WSL: "Ubuntu", Bin: "penguin"}).HasApp() {
		t.Fatal("WSL twin has the Windows app")
	}
}

// The app installed elsewhere on Windows is found through the key its
// installer registers, electron-builder's UUID v5 of the appId: the same
// derivation gives dsh's 1bf39983-…, read from its installer (#2279).
func TestPenguinDesktopAppRegistered(t *testing.T) {
	app := desktopApps["penguin"]
	if app.guid != "8bb9acb7-2bdc-596b-9669-7b014ab1c47f" || app.id != "com.prismshadow.penguinharness" {
		t.Fatalf("%+v", app)
	}
	dir := filepath.Join(t.TempDir(), "tools", "PenguinHarness")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "PenguinHarness.exe"), []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	was, wasFolders := windowsRegisteredApp, appFolders
	t.Cleanup(func() { windowsRegisteredApp, appFolders = was, wasFolders })
	appFolders = func() []string { return nil }
	windowsRegisteredApp = func(guid string) (string, string) {
		if guid == app.guid {
			return dir, "0.2.17"
		}
		return "", ""
	}
	if !app.installed() || app.version() != "0.2.17" {
		t.Fatalf("registered app: installed %v, version %q", app.installed(), app.version())
	}
}

// With only the app installed, never opened, Connect writes the Project
// the app's server adopts on its first start without overwriting
// (provisionInitialProject: default_project under ~/.penguin/data).
func TestPenguinDesktopOnlyConnects(t *testing.T) {
	home := syncHome(t)
	t.Setenv("PATH", t.TempDir())
	apps := t.TempDir()
	was := appFolders
	appFolders = func() []string { return []string{apps} }
	t.Cleanup(func() { appFolders = was })
	installApp(t, apps, "PenguinHarness.app", penguinAppPlist)
	a := penguinOf(t)
	if !a.Detected() {
		t.Fatal("the app alone isn't detected")
	}
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".penguin", "data", "default_project", ".project_config.toml"); a.Path != want {
		t.Fatalf("writes %s, not %s", a.Path, want)
	}
	if !a.Wired() || a.Check() != "" {
		t.Fatalf("not connected: %q\n%s", a.Check(), readFile(a.Path))
	}
}
