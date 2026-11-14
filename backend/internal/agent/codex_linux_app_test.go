package agent

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// linuxCodexMetadata is resources/linux-package-metadata.json as OpenAI's
// chatgpt 26.1027.30824 deb installs it (seen in the WSL distro
// uni-switch-desktop, Ubuntu 24.04).
const linuxCodexMetadata = `{
  "codexAppBrand": "chatgpt",
  "codexBuildFlavor": "prod",
  "version": "26.1027.30824"
}
`

// installLinuxCodexApp puts OpenAI's Linux package's files uni-switch reads
// under the sandbox's /usr/lib/chatgpt, and says where.
func installLinuxCodexApp(t *testing.T) string {
	t.Helper()
	dir := desktopApps["codex"].linuxDir()
	if dir != "" {
		t.Fatalf("an empty sandbox has the Linux Codex app at %s", dir)
	}
	app := desktopApps["codex"]
	if app.linux == "" {
		t.Fatal("no Linux folder for the Codex app")
	}
	dir = proc.SystemDirs(app.linux)[0]
	res := filepath.Join(dir, "resources")
	if err := os.MkdirAll(res, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.WriteFile(filepath.Join(res, "linux-package-metadata.json"), []byte(linuxCodexMetadata), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// pota on Discord: uni-switch had no account sign-in or switch for the
// ChatGPT desktop app on Linux, which macOS and Windows have. OpenAI's
// Linux package (chatgpt, in preview since 08-11) installs the app to
// /usr/lib/chatgpt; the Agents row shows its version, from the package's
// metadata, with or without a CLI, and Codex's folder isn't a leftover
// while it is installed.
func TestCodexLinuxAppVersion(t *testing.T) {
	was, wasStore := appFolders, windowsStorePackages
	appFolders = func() []string { return nil }
	windowsStorePackages = func() []string { return nil }
	t.Cleanup(func() { appFolders, windowsStorePackages = was, wasStore })
	t.Setenv("PATH", t.TempDir()) // no codex CLI
	codex := &Agent{ID: "codex", Name: "Codex", Bin: "codex"}
	if v := codex.AppVersion(); v != "" || codex.HasApp() {
		t.Fatalf("no app installed: %q, %v", v, codex.HasApp())
	}

	dir := installLinuxCodexApp(t)
	if v := codex.AppVersion(); v != "26.1027.30824" {
		t.Fatalf("the Linux package's version: %q", v)
	}
	if !codex.HasApp() {
		t.Fatal("the Linux app not installed")
	}
	if c, ok := codex.CLI(); !ok || c.App != "26.1027.30824" {
		t.Fatalf("the app alone, no CLI: %+v %v", c, ok)
	}

	// metadata that doesn't say: installed, no version
	if err := os.WriteFile(filepath.Join(dir, "resources", "linux-package-metadata.json"), []byte(`{"codexAppBrand":"chatgpt"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if v := codex.AppVersion(); v != "" || !codex.HasApp() {
		t.Fatalf("metadata without a version: %q, %v", v, codex.HasApp())
	}
	// Codex's twin in WSL has none of this machine's
	if (&Agent{ID: "codex@wsl:Ubuntu", WSL: "Ubuntu", Bin: "codex"}).HasApp() {
		t.Fatal("WSL twin")
	}
}

// After a switch uni-switch restarts the app's app-server, and when the app
// starts none it says to quit and open it again, unless the app was quit
// meanwhile: whether it is open is known on Linux too, from any process of
// /usr/lib/chatgpt/ChatGPT (the main one and Electron's helpers, as ps
// listed them for 26.1027), never from its codex alone or another
// program's ChatGPT.
func TestCodexLinuxAppOpen(t *testing.T) {
	restartSandbox(t)
	var cmds []string
	running = func(pat string) []runningProc {
		re := regexp.MustCompile(pat)
		var out []runningProc
		for _, c := range cmds {
			if re.MatchString(c) {
				out = append(out, runningProc{cmd: c})
			}
		}
		return out
	}
	if _, known := codexAppOpen(context.Background()); known {
		t.Fatal("known on no system")
	}
	restartOnLinux = true
	for _, tc := range []struct {
		cmds []string
		open bool
	}{
		{[]string{"/usr/lib/chatgpt/ChatGPT", "/usr/lib/chatgpt/ChatGPT --type=zygote --no-zygote-sandbox", codexLinuxAppServerCmd}, true},
		{[]string{"/usr/lib/chatgpt/ChatGPT --type=renderer"}, true},
		{nil, false},
		{[]string{codexLinuxAppServerCmd, "/usr/lib/chatgpt/chrome_crashpad_handler --monitor-self", "/opt/ChatGPT/ChatGPT", "/usr/lib/chatgpt/ChatGPTx"}, false},
	} {
		cmds = tc.cmds
		if open, known := codexAppOpen(context.Background()); open != tc.open || !known {
			t.Errorf("%q: open %v, known %v; want %v", tc.cmds, open, known, tc.open)
		}
	}
}

const codexLinuxAppServerCmd = "/usr/lib/chatgpt/resources/codex -c features.code_mode_host=true app-server --analytics-default-enabled"

// The Linux app's codex reads Codex's config as the CLI does: it is one
// of the readers whose version decides what uni-switch writes there, read
// from its --version (codex-cli 0.166.0-alpha.5.1 in chatgpt 26.1027).
func TestCodexLinuxAppReadsTheConfig(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the Linux package is read on Linux only")
	}
	t.Setenv("PATH", t.TempDir()) // no codex CLI
	home := t.TempDir()
	if got := codexReaders(place{home: home}); slices.ContainsFunc(got, func(r CodexReader) bool { return r.Kind == "app" }) {
		t.Fatalf("no app installed: %+v", got)
	}
	dir := installLinuxCodexApp(t)
	bin := filepath.Join(dir, "resources", "codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho codex-cli 0.166.0-alpha.5.1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	wasRun := runVersion
	runVersion = func(b string) string {
		if b != bin {
			t.Errorf("version read from %s, want %s", b, bin)
		}
		return "codex-cli 0.166.0-alpha.5.1"
	}
	t.Cleanup(func() { runVersion = wasRun })
	got := codexReaders(place{home: home})
	if !slices.Contains(got, CodexReader{Kind: "app", Version: "0.166.0-alpha.5.1"}) {
		t.Fatalf("the Linux app's codex not read: %+v", got)
	}
}
