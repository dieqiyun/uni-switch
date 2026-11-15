package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dshAppPlist is DeepSeek Harness Desktop's Info.plist as its 0.2.0-rc.2
// macOS build ships it (download.deepseek.com's dsh-latest-macos-arm64.dmg),
// cut to the keys around the ones read, in their order and indentation.
const dshAppPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>CFBundleDisplayName</key>
    <string>DeepSeek Harness</string>
    <key>CFBundleExecutable</key>
    <string>DeepSeek Harness</string>
    <key>CFBundleIconFile</key>
    <string>icon.icns</string>
    <key>CFBundleIdentifier</key>
    <string>com.deepseek.dsh</string>
    <key>CFBundleInfoDictionaryVersion</key>
    <string>6.0</string>
    <key>CFBundleName</key>
    <string>DeepSeek Harness</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>0.2.0-rc.2</string>
    <key>CFBundleVersion</key>
    <string>0.2.0-rc.2</string>
  </dict>
</plist>
`

// dshDesktopPatch is the patch list dsh writes for a new profile
// (PROFILE_PATCH_TEMPLATE in the app's dsh-app-boot), which is what the
// desktop app's profiles/desktop/cordis.patch.yml holds after its first start.
const dshDesktopPatch = "# Your patch layer for this dsh profile, applied after every bundle layer:\n" +
	"# a top-level YAML array of loader patch entries (id-targeted config\n" +
	"# overrides, disables, and insert lists; `!!js` expressions allowed).\n" +
	"[]\n"

// star on Discord (2026-10-09): 有一个bug DeepSeek Harness 就是现在已经有桌面版
// 了，但是他还是非要找CLI. With DeepSeek Harness Desktop in use and no dsh
// CLI, the Agents page said 未找到 CLI and offered dsh's npm install. The
// desktop app keeps ~/.dsh as the CLI does and needs no CLI, so with it
// installed the folder is no sign of an uninstall: its version is shown
// instead, on macOS and Windows alike.
func TestDshDesktopAppIsNoMissingCLI(t *testing.T) {
	// the Mac app: on Windows ~\.dsh is never gone (#2300), whatever is here
	judgeFor(t, "darwin")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("DSH_HOME", "")
	t.Setenv("PATH", t.TempDir()) // no dsh CLI
	apps, userApps := t.TempDir(), t.TempDir()
	was := appFolders
	appFolders = func() []string { return []string{apps, userApps} }
	t.Cleanup(func() { appFolders = was })
	desktop := filepath.Join(home, ".dsh", "profiles", "desktop", "cordis.patch.yml")
	if err := os.MkdirAll(filepath.Dir(desktop), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(desktop, []byte(dshDesktopPatch), 0o644); err != nil {
		t.Fatal(err)
	}
	a := dsh(home)
	// a name no machine has: proc.FindTool also looks where users' tools go
	a.Bin = "uni-switch-test-gone-dsh"
	offered := func() bool {
		for _, x := range installsOf([]*Agent{a}, "darwin", true) {
			if x.ID == "dsh" {
				return true
			}
		}
		return false
	}

	// no app: ~/.dsh left without its CLI is still said (#843)
	if !a.Detected() || !a.CLIMissing() || !offered() {
		t.Fatalf("no app, no CLI: detected %v, missing %v, offered %v", a.Detected(), a.CLIMissing(), offered())
	}

	// the macOS app in ~/Applications
	installApp(t, userApps, "DeepSeek Harness.app", dshAppPlist)
	if a.CLIMissing() || offered() {
		t.Fatalf("with the desktop app: missing %v, offered %v", a.CLIMissing(), offered())
	}
	if c, ok := a.CLI(); !ok || c.App != "0.2.0-rc.2" || c.Version != "" {
		t.Fatalf("the desktop app's version: %+v %v", c, ok)
	}

	// another app under that name is not dsh's
	os.RemoveAll(filepath.Join(userApps, "DeepSeek Harness.app"))
	installApp(t, apps, "DeepSeek Harness.app", strings.Replace(dshAppPlist, "com.deepseek.dsh", "com.example.other", 1))
	if !a.CLIMissing() {
		t.Fatal("another bundle id taken for the desktop app")
	}

	// Windows: the per-user installer's DeepSeek Harness.exe
	exe := filepath.Join(apps, "DeepSeek Harness", "DeepSeek Harness.exe")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	if a.CLIMissing() || offered() {
		t.Fatalf("with the Windows app: missing %v, offered %v", a.CLIMissing(), offered())
	}

	// dsh in a WSL distro is not the Windows app's
	if (&Agent{ID: "dsh@wsl:Ubuntu", WSL: "Ubuntu", Bin: "dsh"}).HasApp() {
		t.Fatal("WSL twin has the Windows app")
	}
}

// The desktop app alone, its profile as its first start leaves it: uni-switch's
// model set goes into that profile and dsh's key store, and nothing reads it
// as no longer going through uni-switch.
func TestDshDesktopOnlyWired(t *testing.T) {
	home, dir, _ := dshRouteHome(t)
	t.Setenv("PATH", t.TempDir())
	desktop := filepath.Join(dir, "profiles", "desktop", "cordis.patch.yml")
	if err := os.MkdirAll(filepath.Dir(desktop), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(desktop, []byte(dshDesktopPatch), 0o644); err != nil {
		t.Fatal(err)
	}
	a := dsh(home)
	if a.Path != desktop {
		t.Fatalf("edits %s, not the desktop app's profile", a.Path)
	}
	if err := a.Field("model").Set("uni-switch/deepseek/flash"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(desktop)
	if s := string(b); !strings.Contains(s, "apiKeyEnv: "+dshKeyRef) || !strings.Contains(s, "provider: "+dshRoute) {
		t.Fatalf("the desktop profile has no route or start:\n%s", s)
	}
	if v, _ := os.ReadFile(filepath.Join(dir, ".credentials.yaml")); !strings.Contains(string(v), dshKeyRef+": ") {
		t.Fatalf("no key in dsh's store:\n%s", v)
	}
	if got := a.Field("model").Get(); got != "uni-switch/deepseek/flash" {
		t.Fatalf("model reads %q", got)
	}
	if d := a.Check(); d != "" {
		t.Fatalf("check: %s", d)
	}
	if d := a.Drift(); d != nil {
		t.Fatalf("drift: %+v", d)
	}
}

// The desktop app first opened while uni-switch runs, after the CLI's web
// profile was wired: its new profile, dsh's empty template, listed none of
// uni-switch's models, and the Agents page said DeepSeek Harness no longer goes
// through uni-switch until Reapply or uni-switch's next start (only the catalog sync
// filled new profiles). The serving round now fills a profile dsh has just
// made; one with entries of its own, the user's, is still left to Reapply.
func TestDshDesktopProfileMadeWhileServing(t *testing.T) {
	home, dir, web := dshRouteHome(t)
	if err := os.WriteFile(web, []byte(dshDesktopPatch), 0o644); err != nil {
		t.Fatal(err)
	}
	a := dsh(home)
	if err := a.Field("model").Set("uni-switch/deepseek/flash"); err != nil {
		t.Fatal(err)
	}
	if d := a.Check(); d != "" {
		t.Fatalf("web wired: %s", d)
	}
	desktop := filepath.Join(dir, "profiles", "desktop", "cordis.patch.yml")
	if err := os.MkdirAll(filepath.Dir(desktop), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(desktop, []byte(dshDesktopPatch), 0o644); err != nil {
		t.Fatal(err)
	}
	if d := a.Check(); !strings.Contains(d, "desktop profile") {
		t.Fatalf("the new desktop profile not said: %q", d)
	}
	if trouble := dshWiredOnce(); trouble != "" {
		t.Fatal(trouble)
	}
	b, _ := os.ReadFile(desktop)
	if s := string(b); !strings.Contains(s, "apiKeyEnv: "+dshKeyRef) || !strings.Contains(s, "provider: "+dshRoute) {
		t.Fatalf("the round left the desktop profile bare:\n%s", s)
	}
	if d := a.Check(); d != "" {
		t.Fatalf("after the round: %s", d)
	}

	// a profile with the user's own entries and no route is theirs: the
	// round doesn't put uni-switch in it
	own := "- id: agent-default-model\n  config:\n    provider: deepseek-official\n    model: deepseek-v4-pro\n"
	if err := os.WriteFile(desktop, []byte(own), 0o644); err != nil {
		t.Fatal(err)
	}
	if trouble := dshWiredOnce(); trouble != "" {
		t.Fatal(trouble)
	}
	if b, _ := os.ReadFile(desktop); string(b) != own {
		t.Fatalf("the user's profile written:\n%s", b)
	}
}

// #2279 (liangjinyuan-sihan): DeepSeek Harness Desktop installed and not yet
// opened has no ~/.dsh, so the Agents page had no DeepSeek Harness row and
// uni-switch nowhere to write: with no profile, a model picked went to
// config.yaml, which no dsh since 0.1.5 reads. The app now counts as dsh
// here, and its profile is made as the app's first start makes it — dsh's
// template — with uni-switch's route in it. The app keeps a patch list it
// finds (initProfile writes only missing files), so its first start adds
// its package.json beside it. Disconnect before then takes it out again;
// the user's own key in dsh's store survives both.
func TestDshDesktopNeverOpened(t *testing.T) {
	home, dir, _ := dshRouteHome(t)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir()) // no dsh CLI
	apps := t.TempDir()
	was := appFolders
	appFolders = func() []string { return []string{apps} }
	t.Cleanup(func() { appFolders = was })
	desktop := filepath.Join(dir, "profiles", "desktop", "cordis.patch.yml")
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }

	if dsh(home).Detected() {
		t.Fatal("neither app nor CLI, yet detected")
	}
	installApp(t, apps, "DeepSeek Harness.app", dshAppPlist)
	a := dsh(home)
	if !a.Detected() || a.CLIMissing() {
		t.Fatalf("the desktop app alone: detected %v, CLI missing %v", a.Detected(), a.CLIMissing())
	}
	if a.Path != desktop {
		t.Fatalf("names %s, not the desktop app's profile", a.Path)
	}
	if dshProfileTemplate != dshDesktopPatch {
		t.Fatal("the template uni-switch writes isn't dsh's")
	}

	// the user's own key in dsh's store, set before uni-switch came
	creds := filepath.Join(dir, ".credentials.yaml")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(creds, []byte("version: 1\nrefs:\n  MINE_API_KEY: sk-mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := a.Pick("model", "uni-switch/deepseek/flash"); err != nil {
		t.Fatal(err)
	}
	s := read(desktop)
	if !strings.HasPrefix(s, strings.TrimSuffix(dshDesktopPatch, "[]\n")) || !strings.Contains(s, "apiKeyEnv: "+dshKeyRef) || !strings.Contains(s, "provider: "+dshRoute) {
		t.Fatalf("the desktop profile uni-switch made:\n%s", s)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.yaml")); err == nil {
		t.Fatal("wrote config.yaml, which dsh 0.2 doesn't read")
	}
	if c := read(creds); !strings.Contains(c, "MINE_API_KEY: sk-mine") || !strings.Contains(c, dshKeyRef+": ") {
		t.Fatalf("dsh's key store:\n%s", c)
	}
	a = dsh(home)
	if !a.Wired() || a.Check() != "" || a.Drift() != nil {
		t.Fatalf("not wired: wired %v, check %q, drift %+v", a.Wired(), a.Check(), a.Drift())
	}
	if n := a.Notice(); !strings.Contains(n, noticeDsh5.String()) {
		t.Fatalf("notice: %q", n)
	}

	// Disconnect before the app ever opened: what uni-switch made goes, the
	// user's key stays
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "profiles")); !os.IsNotExist(err) {
		t.Fatalf("the profile uni-switch made outlived Disconnect: %v\n%s", err, read(desktop))
	}
	if c := read(creds); !strings.Contains(c, "MINE_API_KEY: sk-mine") || strings.Contains(c, dshKeyRef) {
		t.Fatalf("dsh's key store after Disconnect:\n%s", c)
	}

	// connected again, and the app's first start: its files beside
	// uni-switch's patch list, which it keeps
	a = dsh(home)
	if err := a.Pick("model", "uni-switch/deepseek/flash"); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"package.json": "{\n  \"name\": \"dsh-profile-desktop\"\n}\n", "pnpm-workspace.yaml": "packages:\n  - .\n"} {
		if err := os.WriteFile(filepath.Join(filepath.Dir(desktop), name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a = dsh(home)
	if n := a.Notice(); strings.Contains(n, noticeDsh5.String()) {
		t.Fatalf("an opened app said never opened: %q", n)
	}
	// a model the user picks in the app is theirs, and stays on Disconnect
	own := strings.Replace(read(desktop), "provider: "+dshRoute, "provider: deepseek-official", 1)
	own = strings.Replace(own, `model: "deepseek/flash"`, "model: deepseek-v4-pro", 1)
	if err := os.WriteFile(desktop, []byte(own), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	s = read(desktop)
	if !strings.Contains(s, "model: deepseek-v4-pro") || strings.Contains(s, "apiKeyEnv: "+dshKeyRef) {
		t.Fatalf("the opened app's profile after Disconnect:\n%s", s)
	}
	if !isFile(filepath.Join(filepath.Dir(desktop), "package.json")) {
		t.Fatal("the app's own files went")
	}
}

// #2279: DeepSeek Harness Desktop's Windows installer lets the user pick
// its folder, and registers it under HKCU\Software\<its guid>; an app there
// is found, with the version its Uninstall entry gives.
func TestDshDesktopRegisteredFolder(t *testing.T) {
	apps, picked := t.TempDir(), t.TempDir()
	wasFolders, wasReg := appFolders, windowsRegisteredApp
	appFolders = func() []string { return []string{apps} }
	reg := map[string][2]string{}
	windowsRegisteredApp = func(guid string) (string, string) { r := reg[guid]; return r[0], r[1] }
	t.Cleanup(func() { appFolders, windowsRegisteredApp = wasFolders, wasReg })
	a := &Agent{ID: "dsh", Name: "DeepSeek Harness", Bin: "dsh"}
	if a.HasApp() {
		t.Fatal("nothing installed, yet an app")
	}
	// registered, its folder since emptied by hand: not installed
	reg["1bf39983-50d0-5fe0-9ef4-cece76f67c5e"] = [2]string{picked, "0.2.0-rc.2"}
	if a.HasApp() || a.AppVersion() != "" {
		t.Fatal("a registered folder without the app taken for it")
	}
	if err := os.WriteFile(filepath.Join(picked, "DeepSeek Harness.exe"), []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !a.HasApp() || a.AppVersion() != "0.2.0-rc.2" {
		t.Fatalf("the app in its registered folder: has %v, version %q", a.HasApp(), a.AppVersion())
	}
	if (&Agent{ID: "dsh@wsl:Ubuntu", WSL: "Ubuntu", Bin: "dsh"}).HasApp() {
		t.Fatal("WSL twin has the Windows app")
	}
}

// #2325: DeepSeek Harness Desktop installed to D:\tools\dsh was not found.
// Its installer registers the folder under HKCU for the current user and
// under HKLM for all users, and its Uninstall entry names the exe too.
func TestDshDesktopRegisteredAnyHive(t *testing.T) {
	const guid = "1bf39983-50d0-5fe0-9ef4-cece76f67c5e"
	own := `Software\` + guid
	uninstall := `Software\Microsoft\Windows\CurrentVersion\Uninstall\` + guid
	for _, c := range []struct {
		name     string
		reg      map[string]string // hive|key|name
		dir, ver string
	}{
		{"nothing registered", nil, "", ""},
		{"per user", map[string]string{"HKCU|" + own + "|InstallLocation": `D:\tools\dsh`, "HKCU|" + uninstall + "|DisplayVersion": "0.2.0"}, `D:\tools\dsh`, "0.2.0"},
		{"for all users", map[string]string{"HKLM|" + own + "|InstallLocation": `D:\tools\dsh`, "HKLM|" + uninstall + "|DisplayVersion": "0.2.1"}, `D:\tools\dsh`, "0.2.1"},
		{"the Uninstall entry's own folder", map[string]string{"HKLM|" + uninstall + "|InstallLocation": `"D:\tools\dsh"`}, `D:\tools\dsh`, ""},
		{"the Uninstall entry's icon", map[string]string{"HKCU|" + uninstall + "|DisplayIcon": `D:\tools\dsh\DeepSeek Harness.exe,0`}, `D:\tools\dsh`, ""},
		{"the Uninstall entry's uninstaller", map[string]string{"HKCU|" + uninstall + "|UninstallString": `"D:\tools\dsh\Uninstall DeepSeek Harness.exe" /currentuser`}, `D:\tools\dsh`, ""},
		{"an icon that is no exe", map[string]string{"HKCU|" + uninstall + "|DisplayIcon": `D:\tools\dsh\icon.ico`}, "", ""},
	} {
		read := func(hive, key, name string) string { return c.reg[hive+"|"+key+"|"+name] }
		dir, ver := registeredIn(guid, read)
		if dir != c.dir || ver != c.ver {
			t.Errorf("%s: got %q %q, want %q %q", c.name, dir, ver, c.dir, c.ver)
		}
	}
}

// sungoku (#2325, macOS 15.8.1, uni-switch 0.1.1253): DeepSeek Harness.app in
// /Applications/AI, a folder of the user's own, and no dsh CLI; the Agents
// page said 未找到 CLI. The app is found one folder down in an app folder,
// and wherever Spotlight has its bundle id; a bundle of another id there
// is still not dsh's, nor is an app inside another app's bundle.
func TestDshDesktopAppInASubfolder(t *testing.T) {
	// the Mac app: on Windows ~\.dsh is never gone (#2300), whatever is here
	judgeFor(t, "darwin")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("DSH_HOME", "")
	t.Setenv("PATH", t.TempDir()) // no dsh CLI
	apps := t.TempDir()
	var spotlit []string
	wasFolders, wasSpot := appFolders, spotlightApps
	appFolders = func() []string { return []string{apps} }
	spotlightApps = func(id string) []string {
		if id != "com.deepseek.dsh" {
			return nil
		}
		return spotlit
	}
	t.Cleanup(func() { appFolders, spotlightApps = wasFolders, wasSpot })
	desktop := filepath.Join(home, ".dsh", "profiles", "desktop", "cordis.patch.yml")
	if err := os.MkdirAll(filepath.Dir(desktop), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(desktop, []byte(dshDesktopPatch), 0o644); err != nil {
		t.Fatal(err)
	}
	a := dsh(home)
	a.Bin = "uni-switch-test-gone-dsh"
	if !a.CLIMissing() {
		t.Fatal("no app, no CLI: not missing")
	}

	// another app's bundle holding one of that name is no folder of apps
	installApp(t, filepath.Join(apps, "Other.app"), "DeepSeek Harness.app", dshAppPlist)
	if !a.CLIMissing() {
		t.Fatal("an app inside another app's bundle taken for the desktop app")
	}

	// another bundle id in /Applications/AI is not dsh's
	installApp(t, filepath.Join(apps, "AI"), "DeepSeek Harness.app", strings.Replace(dshAppPlist, "com.deepseek.dsh", "com.example.other", 1))
	if !a.CLIMissing() {
		t.Fatal("another bundle id in a subfolder taken for the desktop app")
	}

	// sungoku's: /Applications/AI/DeepSeek Harness.app
	os.RemoveAll(filepath.Join(apps, "AI"))
	installApp(t, filepath.Join(apps, "AI"), "DeepSeek Harness.app", dshAppPlist)
	if a.CLIMissing() || a.AppVersion() != "0.2.0-rc.2" {
		t.Fatalf("app in /Applications/AI: missing %v, version %q", a.CLIMissing(), a.AppVersion())
	}

	// deeper, or on another disk: where Spotlight has it, under any name
	os.RemoveAll(filepath.Join(apps, "AI"))
	elsewhere := t.TempDir()
	installApp(t, filepath.Join(elsewhere, "Tools", "AI"), "DSH.app", dshAppPlist)
	if !a.CLIMissing() {
		t.Fatal("found with Spotlight saying nothing")
	}
	spotlit = []string{filepath.Join(elsewhere, "Tools", "AI", "DSH.app")}
	if a.CLIMissing() || a.AppVersion() != "0.2.0-rc.2" {
		t.Fatalf("app Spotlight knows: missing %v, version %q", a.CLIMissing(), a.AppVersion())
	}
}
