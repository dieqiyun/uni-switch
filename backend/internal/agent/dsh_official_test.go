package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// dshOfficialPlist is the Info.plist of DeepSeek Harness 0.2.1-rc.2's macOS
// build (deepseek-harness-0.2.1-rc.2-mac-arm64.zip, from
// download.deepseek.com/dsh-desk/feeds/mac-arm64/nightly-mac.yml, sha512 as
// the feed says, signed by team NAN929V4UM), cut to the keys around the
// ones read. Its appId and productName are github.com/deepseek-ai/
// deepseek-harness's apps/desktop/scripts/electron-builder-config.mjs.
const dshOfficialPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>CFBundleDisplayName</key>
    <string>DeepSeek Harness</string>
    <key>CFBundleExecutable</key>
    <string>DeepSeek Harness</string>
    <key>CFBundleIdentifier</key>
    <string>com.deepseek.dsh</string>
    <key>CFBundleName</key>
    <string>DeepSeek Harness</string>
    <key>CFBundleShortVersionString</key>
    <string>0.2.1-rc.2</string>
  </dict>
</plist>
`

// dshOfficialHome is a home with no dsh CLI, no ~/.dsh and no app, apps the
// folder apps are installed into and reg what the Windows registry holds.
func dshOfficialHome(t *testing.T) (home, dir, apps string, reg map[string][2]string) {
	t.Helper()
	home, dir, _ = dshRouteHome(t)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir()) // no dsh CLI
	apps = t.TempDir()
	reg = map[string][2]string{}
	was, wasReg := appFolders, windowsRegisteredApp
	appFolders = func() []string { return []string{apps} }
	windowsRegisteredApp = func(guid string) (string, string) { r := reg[guid]; return r[0], r[1] }
	t.Cleanup(func() { appFolders, windowsRegisteredApp = was, wasReg })
	return home, dir, apps, reg
}

// WuSix on Discord (Windows 11): DeepSeek's desktop app is
// github.com/deepseek-ai/deepseek-harness's DeepSeek Harness, not the
// community "DeepSeek Hermes" (motongv/deepseek-hermes-desktop) v0.1.1337
// took it for. That one is not dsh's app: uni-switch doesn't list dsh for it,
// nor write a profile or key for it. The official app still is.
func TestDshOnlyTheOfficialApp(t *testing.T) {
	judgeFor(t, "darwin")
	home, dir, apps, reg := dshOfficialHome(t)
	installApp(t, apps, "DeepSeek Hermes.app", strings.NewReplacer(
		"com.deepseek.dsh", "com.deepseek.hermes.desktop",
		"DeepSeek Harness", "DeepSeek Hermes",
		"0.2.1-rc.2", "1.0.0").Replace(dshOfficialPlist))
	writeT(t, filepath.Join(apps, "DeepSeek Hermes", "DeepSeek Hermes.exe"), "MZ")
	picked := t.TempDir()
	writeT(t, filepath.Join(picked, "DeepSeek Hermes.exe"), "MZ")
	reg["df9f5892-ad1c-5623-a260-30fb0acc7e55"] = [2]string{picked, "1.0.0"}
	if a := dsh(home); a.Detected() || a.HasApp() {
		t.Fatalf("DeepSeek Hermes taken for DeepSeek Harness: detected %v, app %v", a.Detected(), a.HasApp())
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("~/.dsh made for DeepSeek Hermes: %v", err)
	}

	installApp(t, apps, "DeepSeek Harness.app", dshOfficialPlist)
	a := dsh(home)
	a.Bin = "uni-switch-test-gone-dsh"
	if !a.Detected() || a.CLIMissing() || !a.HasApp() {
		t.Fatalf("DeepSeek Harness 0.2.1-rc.2: detected %v, CLI missing %v, app %v", a.Detected(), a.CLIMissing(), a.HasApp())
	}
	if v := a.AppVersion(); v != "0.2.1-rc.2" {
		t.Fatalf("app version %q", v)
	}
	if a.Name != "DeepSeek Harness" {
		t.Fatalf("listed as %q", a.Name)
	}
	if want := filepath.Join(dir, "profiles", "desktop", "cordis.patch.yml"); a.Path != want {
		t.Fatalf("names %s, not the desktop profile", a.Path)
	}
}

// The registry as DeepSeek Harness 0.2.1-rc.2's Windows installer
// (deepseek-harness-0.2.1-rc.2-win-x64.exe, run with /S on Windows 11
// 26200) writes it, read back with Get-ItemProperty: HKCU only, the exe
// straight in the folder. The key is electron-builder's GUID for the
// appId, a UUID v5 of com.deepseek.dsh in its namespace.
func TestDshWindowsAsTheInstallerRegistersIt(t *testing.T) {
	const appID = "com.deepseek.dsh"
	ns := uuid.MustParse("50e065bc-3134-11e6-9bab-38c9862bdaf3") // electron-builder's
	guid := desktopApps["dsh"].guid
	if got := uuid.NewSHA1(ns, []byte(appID)).String(); got != guid || guid != "1bf39983-50d0-5fe0-9ef4-cece76f67c5e" {
		t.Fatalf("guid %s, the installer's %s", guid, got)
	}
	if desktopApps["dsh"].id != appID {
		t.Fatalf("bundle id %s", desktopApps["dsh"].id)
	}

	at := `C:\Users\yeton\AppData\Local\Programs\DeepSeek Harness`
	values := map[string]string{
		`HKCU|Software\` + guid + `|InstallLocation`:                                            at,
		`HKCU|Software\` + guid + `|ShortcutName`:                                               "DeepSeek Harness",
		`HKCU|Software\Microsoft\Windows\CurrentVersion\Uninstall\` + guid + `|DisplayName`:     "DeepSeek Harness 0.2.1-rc.2",
		`HKCU|Software\Microsoft\Windows\CurrentVersion\Uninstall\` + guid + `|DisplayVersion`:  "0.2.1-rc.2",
		`HKCU|Software\Microsoft\Windows\CurrentVersion\Uninstall\` + guid + `|DisplayIcon`:     at + `\DeepSeek Harness.exe,0`,
		`HKCU|Software\Microsoft\Windows\CurrentVersion\Uninstall\` + guid + `|UninstallString`: `"` + at + `\Uninstall DeepSeek Harness.exe" /currentuser`,
		`HKCU|Software\Microsoft\Windows\CurrentVersion\Uninstall\` + guid + `|InstallLocation`: at,
	}
	read := func(hive, key, name string) string { return values[hive+"|"+key+"|"+name] }
	if dir, v := registeredIn(guid, read); dir != at || v != "0.2.1-rc.2" {
		t.Fatalf("read %q %q", dir, v)
	}
	// with only the Uninstall entry's DisplayIcon left
	for k := range values {
		if !strings.HasSuffix(k, "|DisplayIcon") && !strings.HasSuffix(k, "|DisplayVersion") {
			delete(values, k)
		}
	}
	if dir, _ := registeredIn(guid, read); dir != at {
		t.Fatalf("from DisplayIcon: %q", dir)
	}

	// a folder the user picked, as the installer lays it out
	home, _, _, reg := dshOfficialHome(t)
	picked := t.TempDir()
	reg[guid] = [2]string{picked, "0.2.1-rc.2"}
	if dsh(home).Detected() {
		t.Fatal("a registered folder without the app taken for it")
	}
	writeT(t, filepath.Join(picked, "DeepSeek Harness.exe"), "MZ")
	writeT(t, filepath.Join(picked, "Uninstall DeepSeek Harness.exe"), "MZ")
	if a := dsh(home); !a.Detected() || !a.HasApp() {
		t.Fatal("DeepSeek Harness in the registered folder not found")
	}
}
