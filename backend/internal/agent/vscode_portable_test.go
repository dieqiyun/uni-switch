package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/tidwall/gjson"
)

// portableVSCode lays VS Code out under home as a portable build is: its
// launcher in bin, and, when portable, its data folder beside the program —
// a zip's own, or Scoop's, whose apps\vscode\current\data is the persisted
// one (current a junction into apps\vscode, which a folder reads through
// the same). The launcher is put on PATH; with shims, a Scoop shim of it
// is, as scoop's shims folder has.
func portableVSCode(t *testing.T, home string, portable, shims bool) (user string) {
	t.Helper()
	app := filepath.Join(home, "Editor With Spaces")
	binDir := filepath.Join(app, "bin")
	if shims {
		app = filepath.Join(home, "scoop", "apps", "vscode", "current")
		binDir = filepath.Join(home, "scoop", "shims")
	}
	name := "code"
	if runtime.GOOS == "windows" {
		name += ".cmd"
		t.Setenv("PATHEXT", ".CMD")
	}
	writeAt(t, filepath.Join(app, "bin", name), "launcher\n")
	if err := os.Chmod(filepath.Join(app, "bin", name), 0o755); err != nil {
		t.Fatal(err)
	}
	if shims {
		writeAt(t, filepath.Join(binDir, name), "shim\n")
		if err := os.Chmod(filepath.Join(binDir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		writeAt(t, filepath.Join(binDir, "code.shim"), "path = "+filepath.Join(app, "bin", name)+"\n")
	}
	// a Mac's VS Code is never laid out so (macPortableVSCode); this is
	// Windows' and Linux's, read as Linux's on a Mac
	if runtime.GOOS == "darwin" {
		useVSCodeOS(t, "linux")
	}
	if portable {
		if err := os.MkdirAll(filepath.Join(app, "data"), 0o755); err != nil {
			t.Fatal(err)
		}
		user = filepath.Join(app, "data", "user-data", "User")
	}
	t.Setenv("PATH", binDir)
	return user
}

// installedUserDir is the User folder of an installed VS Code, where the OS
// puts it.
func installedUserDir(home string) string {
	switch vscodeOS {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Code", "User")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Code", "User")
	}
	return filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "Code", "User")
}

// YOUIMARK's Windows has Scoop's VS Code, a portable build: uni-switch showed
// the editor, but its Dir stayed %APPDATA%\Code\User, which doesn't exist —
// code.cmd on PATH was detection enough — so its provider, models and
// settings.json went where the build never reads (#3260). A portable
// build's User folder is in its data folder: data\user-data\User beside the
// program. An installed build, whose launcher has no data folder beside it,
// keeps the OS's.
func TestVSCodePortableUserDir(t *testing.T) {
	home, cfg := syncHome(t), os.Getenv("XDG_CONFIG_HOME")
	// the launcher is taken through filepath.EvalSymlinks, which a Mac's
	// temporary folders come out of one symlink longer
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	portableVSCode(t, home, false, false)
	if a := vscode(home, cfg); a.Dir != installedUserDir(home) {
		t.Fatalf("an installed build's User folder is %s, want %s", a.Dir, installedUserDir(home))
	}
	user := portableVSCode(t, home, true, false)
	a := vscode(home, cfg)
	if a.Dir != user {
		t.Fatalf("the portable build's User folder is %s, want %s", a.Dir, user)
	}
	// before its first run has made the folder, its launcher alone is
	// detection enough — and connecting writes there, where it reads
	if !a.Detected() {
		t.Fatal("the portable build isn't listed")
	}
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	lm := filepath.Join(a.Dir, "chatLanguageModels.json")
	if g, ok := edit.GetJSONItem(lm, vscodeGroup); !ok || gjson.Get(g, "models.0.id").String() != "relay/glm-4.6" {
		t.Fatalf("not wired in the data folder:\n%s", readFile(lm))
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if _, ok := edit.GetJSONItem(lm, vscodeGroup); ok {
		t.Fatalf("disconnected, uni-switch's group kept:\n%s", readFile(lm))
	}
}

// Scoop's code.cmd is a shim: the launcher it stands for is named by the
// .shim beside it (proc.Unwrap doesn't read those), and the data folder is
// the persisted one. A shim of a launcher with no data folder beside it is
// an installed build; its steps run in that order, as a data folder made
// stays.
func TestVSCodePortableScoopShim(t *testing.T) {
	home, cfg := syncHome(t), os.Getenv("XDG_CONFIG_HOME")
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	portableVSCode(t, home, false, true)
	if a := vscode(home, cfg); a.Dir != installedUserDir(home) {
		t.Fatalf("an installed build's User folder is %s, want %s", a.Dir, installedUserDir(home))
	}
	user := portableVSCode(t, home, true, true)
	if a := vscode(home, cfg); a.Dir != user {
		t.Fatalf("Scoop's portable VS Code reads %s, want %s", a.Dir, user)
	}
}

// useVSCodeOS reads VS Code's layout as goos's for the test.
func useVSCodeOS(t *testing.T, goos string) {
	t.Helper()
	was := vscodeOS
	vscodeOS = goos
	t.Cleanup(func() { vscodeOS = was })
}

// noMacApps is a Mac with no app in its app folders or Spotlight but those
// in apps.
func noMacApps(t *testing.T, apps ...string) {
	t.Helper()
	wasFolders, wasSpot := appFolders, spotlightApps
	appFolders = func() []string { return apps }
	spotlightApps = func(string) []string { return nil }
	t.Cleanup(func() { appFolders, spotlightApps = wasFolders, wasSpot })
}

// macVSCodeApp lays a Mac's VS Code bundle out as the app is: its
// launcher at Contents/Resources/app/bin/<bin> beside product.json. It
// returns the launcher.
func macVSCodeApp(t *testing.T, bundle, bin, product string) string {
	t.Helper()
	app := filepath.Join(bundle, "Contents", "Resources", "app")
	writeAt(t, filepath.Join(app, "product.json"), product)
	launcher := filepath.Join(app, "bin", bin)
	writeAt(t, launcher, "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(launcher, 0o755); err != nil {
		t.Fatal(err)
	}
	return launcher
}

// macPortableVSCode is faketrader's Mac (#4151): /Applications/Code holding
// Visual Studio Code.app beside code-portable-data, and the code on PATH
// /usr/local/bin/code, a symlink into the bundle. All of it is under root,
// a folder with a space in its name, read as a Mac's on any machine.
func macPortableVSCode(t *testing.T, k vscodeKind, product, dataName string) (root, data string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a Mac's symlinked launcher")
	}
	useVSCodeOS(t, "darwin")
	noMacApps(t)
	root = filepath.Join(t.TempDir(), "Macintosh HD")
	if resolved, err := filepath.EvalSymlinks(filepath.Dir(root)); err == nil {
		root = filepath.Join(resolved, "Macintosh HD")
	}
	name := "Visual Studio Code.app"
	if k.id == vscodeInsiders.id {
		name = "Visual Studio Code - Insiders.app"
	}
	bundle := filepath.Join(root, "Applications", "Code", name)
	launcher := macVSCodeApp(t, bundle, k.bin, product)
	if dataName != "" {
		data = filepath.Join(root, "Applications", "Code", dataName)
		mkdirT(t, data)
	}
	path := filepath.Join(root, "usr", "local", "bin")
	mkdirT(t, path)
	if err := os.Symlink(launcher, filepath.Join(path, k.bin)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", path)
	return root, data
}

// VS Code's configurePortable (src/bootstrap-node.ts) on a Mac: a build
// with no target in its product.json is portable when the folder its
// product.json's portable names, else <applicationName>-portable-data,
// is beside the .app. Its User folder is user-data/User there, and its
// extensions extensions; else the User folder is Application Support's.
func TestVSCodeMacPortableDirs(t *testing.T) {
	for _, tt := range []struct {
		name, product, data string
		kind                vscodeKind
		portable            bool
	}{
		{"stable", `{"applicationName":"code","nameShort":"Code"}`, "code-portable-data", vscodeStable, true},
		{"insiders", `{"applicationName":"code-insiders"}`, "code-insiders-portable-data", vscodeInsiders, true},
		{"product's portable", `{"applicationName":"code","portable":"my data"}`, "my data", vscodeStable, true},
		{"no data folder", `{"applicationName":"code"}`, "", vscodeStable, false},
		{"Windows' name for it", `{"applicationName":"code"}`, "data", vscodeStable, false},
		{"another editor", `{"applicationName":"cursor"}`, "code-portable-data", vscodeStable, false},
		{"another channel's folder", `{"applicationName":"code-insiders"}`, "code-portable-data", vscodeInsiders, false},
		{"unreadable product", `{"applicationName":"code",`, "code-portable-data", vscodeStable, false},
		{"an installer's build", `{"applicationName":"code","target":"user"}`, "code-portable-data", vscodeStable, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root, data := macPortableVSCode(t, tt.kind, tt.product, tt.data)
			home := filepath.Join(root, "Users", "me")
			user, exts := vscodeDirs(tt.kind, home, "unused")
			wantUser := filepath.Join(home, "Library", "Application Support", tt.kind.folder, "User")
			wantExts := filepath.Join(home, tt.kind.extFolder(), "extensions")
			if tt.portable {
				wantUser, wantExts = filepath.Join(data, "user-data", "User"), filepath.Join(data, "extensions")
			}
			if user != wantUser || exts != wantExts {
				t.Fatalf("folders %q, %q, want %q, %q", user, exts, wantUser, wantExts)
			}
			if _, err := os.Stat(user); !os.IsNotExist(err) {
				t.Fatalf("finding the User folder made it: %v", err)
			}
		})
	}
}

// /usr/local/bin/code may be a symlink to a symlink (Homebrew's, a
// user's): each is followed to the bundle.
func TestVSCodeMacPortableLinkChain(t *testing.T) {
	root, data := macPortableVSCode(t, vscodeStable, `{"applicationName":"code"}`, "code-portable-data")
	path := filepath.Join(root, "usr", "local", "bin")
	other := filepath.Join(root, "opt", "homebrew bin")
	mkdirT(t, other)
	if err := os.Rename(filepath.Join(path, "code"), filepath.Join(other, "code")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(other, "code"), filepath.Join(path, "code-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("code-link", filepath.Join(path, "code")); err != nil {
		t.Fatal(err)
	}
	if user, _ := vscodeDirs(vscodeStable, filepath.Join(root, "home"), "unused"); user != filepath.Join(data, "user-data", "User") {
		t.Fatalf("through three links the User folder is %q", user)
	}
}

// The reporter's case end to end: connecting, a profile's sync and
// disconnecting all write the portable build's files, Application
// Support's are left as they were, Zoo Code reads the same User folder
// and extensions, and no preview runs a process that would find the
// build's own files through the launcher on PATH.
func TestVSCodeMacPortableWiring(t *testing.T) {
	home := syncHome(t)
	_, data := macPortableVSCode(t, vscodeStable, `{"applicationName":"code"}`, "code-portable-data")
	dir := filepath.Join(data, "user-data", "User")
	settings := filepath.Join(dir, "settings.json")
	writeFile(t, settings, `{"chat.defaultModel":"auto","editor.fontSize":17}`)
	profile := filepath.Join(dir, "profiles", "work")
	writeFile(t, filepath.Join(profile, "settings.json"), `{"chat.defaultModel":"auto"}`)
	writeFile(t, filepath.Join(dir, "globalStorage", "storage.json"), `{"userDataProfiles":[{"location":"work"}]}`)
	standard := filepath.Join(home, "Library", "Application Support", "Code", "User", "settings.json")
	writeFile(t, standard, `{"editor.fontSize":99}`)
	a := vscode(home, "unused")
	if a.Path != settings || !a.Detected() {
		t.Fatalf("VS Code at %q, detected %v", a.Path, a.Detected())
	}
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	for _, at := range []string{dir, profile} {
		if _, ok := edit.GetJSONItem(filepath.Join(at, "chatLanguageModels.json"), vscodeGroup); !ok {
			t.Errorf("no uni-switch group in %s", at)
		}
	}
	if _, err := DisconnectPreview(a, "no-such-uni-switch"); err == nil || !strings.Contains(err.Error(), "portable VS Code") {
		t.Fatalf("a preview of the portable build: %v", err)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	for _, at := range []string{dir, profile} {
		if v, _ := edit.GetJSON(filepath.Join(at, "settings.json"), vscodeDefault); v != "auto" {
			t.Errorf("%s's default model %q, want auto back", at, v)
		}
		if _, ok := edit.GetJSONItem(filepath.Join(at, "chatLanguageModels.json"), vscodeGroup); ok {
			t.Errorf("uni-switch's group kept in %s", at)
		}
	}
	if got := readFile(standard); got != `{"editor.fontSize":99}` {
		t.Fatalf("Application Support's settings changed: %s", got)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(standard), "chatLanguageModels.json")); !os.IsNotExist(err) {
		t.Fatalf("Application Support got a chatLanguageModels.json: %v", err)
	}
	writeFile(t, filepath.Join(data, "extensions", zooExtension+"-3.90.0", "package.json"), `{"name":"zoo-code","version":"3.90.0"}`)
	zoo := zooCode(home, "unused")
	if zoo.Path != settings || !zoo.Detected() {
		t.Fatalf("Zoo Code at %q, detected %v", zoo.Path, zoo.Detected())
	}
	if _, err := DisconnectPreview(zoo, "no-such-uni-switch"); err == nil || !strings.Contains(err.Error(), "portable VS Code") {
		t.Fatalf("a preview of Zoo Code in the portable build: %v", err)
	}
}

// With no launcher on PATH, the Mac's app folders say: a portable bundle
// one folder down (/Applications/Code) is found, and when another
// install is there too, which the user runs is unknown, so nothing moves.
func TestVSCodeMacPortableWithoutLauncher(t *testing.T) {
	root, data := macPortableVSCode(t, vscodeStable, `{"applicationName":"code"}`, "code-portable-data")
	home := filepath.Join(root, "home")
	t.Setenv("PATH", t.TempDir())
	apps := filepath.Join(root, "Applications")
	noMacApps(t, apps)
	if user, _ := vscodeDirs(vscodeStable, home, "unused"); user != filepath.Join(data, "user-data", "User") {
		t.Fatalf("a portable bundle without a launcher: %q", user)
	}
	macVSCodeApp(t, filepath.Join(apps, "Visual Studio Code.app"), "code", `{"applicationName":"code"}`)
	want := filepath.Join(home, "Library", "Application Support", "Code", "User")
	if user, _ := vscodeDirs(vscodeStable, home, "unused"); user != want {
		t.Fatalf("beside an installed bundle, with no launcher to tell them apart: %q", user)
	}
}

// The launcher picks the install: one into an installed bundle keeps
// Application Support's folder though a portable copy is in the app
// folders, and Insiders' portable build doesn't move Stable's.
func TestVSCodeMacPortableLauncherPicks(t *testing.T) {
	root, _ := macPortableVSCode(t, vscodeStable, `{"applicationName":"code"}`, "")
	home := filepath.Join(root, "home")
	apps := filepath.Join(root, "Other Apps")
	macVSCodeApp(t, filepath.Join(apps, "Portable", "Visual Studio Code.app"), "code", `{"applicationName":"code"}`)
	mkdirT(t, filepath.Join(apps, "Portable", "code-portable-data"))
	macVSCodeApp(t, filepath.Join(apps, "Visual Studio Code - Insiders.app"), "code-insiders", `{"applicationName":"code-insiders"}`)
	mkdirT(t, filepath.Join(apps, "code-insiders-portable-data"))
	noMacApps(t, apps)
	if user, _ := vscodeDirs(vscodeStable, home, "unused"); user != filepath.Join(home, "Library", "Application Support", "Code", "User") {
		t.Fatalf("another portable copy took the launcher's install: %q", user)
	}
	if user, _ := vscodeDirs(vscodeInsiders, home, "unused"); user != filepath.Join(apps, "code-insiders-portable-data", "user-data", "User") {
		t.Fatalf("Insiders' portable bundle: %q", user)
	}
}

// A fork's bin has a launcher named code too (Cursor's); one on PATH is
// not VS Code's, so VS Code's own bundle is still looked for.
func TestVSCodeMacPortableForkLauncher(t *testing.T) {
	root, _ := macPortableVSCode(t, vscodeStable, `{"applicationName":"code"}`, "")
	home := filepath.Join(root, "home")
	path := filepath.Join(root, "fork bin")
	mkdirT(t, path)
	fork := macVSCodeApp(t, filepath.Join(root, "Applications", "Cursor.app"), "code", `{"applicationName":"cursor"}`)
	if err := os.Symlink(fork, filepath.Join(path, "code")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", path)
	apps := filepath.Join(root, "Applications")
	noMacApps(t, apps)
	mkdirT(t, filepath.Join(apps, "Code", "code-portable-data"))
	if user, _ := vscodeDirs(vscodeStable, home, "unused"); user != filepath.Join(apps, "Code", "code-portable-data", "user-data", "User") {
		t.Fatalf("past Cursor's code, VS Code's portable bundle: %q", user)
	}
}

// Windows and Linux follow the same rule: an installer's build (target in
// its product.json) is never portable, nor is a fork whose bin has a code,
// and a portable build's extensions (Zoo Code's) are in its data folder.
func TestVSCodePortableProduct(t *testing.T) {
	home, cfg := syncHome(t), os.Getenv("XDG_CONFIG_HOME")
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	user := portableVSCode(t, home, true, false)
	app := filepath.Join(home, "Editor With Spaces")
	data := filepath.Join(app, "data")
	product := filepath.Join(app, "resources", "app", "product.json")
	for _, tt := range []struct{ product, want string }{
		{`{"applicationName":"code"}`, user},
		{`{"applicationName":"code","target":"user"}`, installedUserDir(home)},
		{`{"applicationName":"cursor"}`, installedUserDir(home)},
		{`{"applicationName":`, user},
	} {
		writeAt(t, product, tt.product)
		if got, _ := vscodeDirs(vscodeStable, home, cfg); got != tt.want {
			t.Errorf("with product.json %s the User folder is %q, want %q", tt.product, got, tt.want)
		}
	}
	// an Insiders-style build that updates into <install>/<version>
	// keeps its product.json there; the data folder is <install>'s still
	if err := os.Remove(product); err != nil {
		t.Fatal(err)
	}
	versioned := filepath.Join(app, "4f2a1c9", "resources", "app", "product.json")
	for _, tt := range []struct{ product, want string }{
		{`{"applicationName":"code","win32VersionedUpdate":true,"target":"user"}`, installedUserDir(home)},
		{`{"applicationName":"code","win32VersionedUpdate":true}`, user},
	} {
		writeAt(t, versioned, tt.product)
		if got, _ := vscodeDirs(vscodeStable, home, cfg); got != tt.want {
			t.Errorf("with a versioned product.json %s the User folder is %q, want %q", tt.product, got, tt.want)
		}
	}
	writeAt(t, product, `{"applicationName":"code"}`)
	writeFile(t, filepath.Join(data, "extensions", zooExtension+"-3.90.0", "package.json"), `{"name":"zoo-code","version":"3.90.0"}`)
	zoo := zooCode(home, cfg)
	if zoo.Path != filepath.Join(user, "settings.json") || !zoo.Detected() {
		t.Fatalf("Zoo Code at %q, detected %v", zoo.Path, zoo.Detected())
	}
	// Scoop's data folder is under home, which a preview copies; the
	// process it starts would still find the real one through PATH
	if _, err := DisconnectPreview(vscode(home, cfg), "no-such-uni-switch"); err == nil || !strings.Contains(err.Error(), "portable VS Code") {
		t.Fatalf("a preview of the portable build: %v", err)
	}
}
