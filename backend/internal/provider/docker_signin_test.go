package provider

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// uni-switch in Docker (xugui on Discord: "docker版登不上grok和command code"):
// its image has no bash, curl or wget, so Grok Build's installer couldn't
// run and Grok's sign-in never began. Grok Build is then installed as its
// installer installs it, from the same place, without a shell.
func TestGrokInstallsWithoutShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake grok is a shell script")
	}
	home := claudeHome(t)
	t.Setenv("GROK_HOME", "")
	t.Setenv("GROK_BIN_DIR", "")
	t.Setenv("PATH", t.TempDir()) // no grok anywhere else
	platform, err := grokPlatform(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write([]byte("#!/bin/sh\necho 'grok 1.2.3 (fake)'\n"))
	zw.Close()
	var asked []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		switch r.URL.Path {
		case "/cli/stable":
			w.Write([]byte("1.2.3\n"))
		case "/cli/grok-1.2.3-" + platform + ".gz":
			w.Write(gz.Bytes())
		default:
			w.WriteHeader(404)
		}
	}))
	defer fake.Close()
	oldBases, oldShell := grokCLIBases, shellInstallerRuns
	// x.ai unreachable: the bucket behind it, as the installer falls back
	grokCLIBases = []string{"http://127.0.0.1:1/cli", fake.URL + "/cli"}
	shellInstallerRuns = func() bool { return false }
	t.Cleanup(func() { grokCLIBases, shellInstallerRuns = oldBases, oldShell })

	c, ok := missingCLI("grok")
	if !ok {
		t.Fatal("grok is installed already")
	}
	// The download lands in a file macOS has already let run, which the
	// installer writes over in place and renames. A newly written one waits
	// for the system's first-run check before its first exec, and while a
	// go test ./... starts other new programs that wait outlasts the
	// install's own 30s for `grok --version`. That check isn't under test.
	testenv.Program(t, filepath.Join(home, ".grok", "downloads", fmt.Sprintf("grok-%s.tmp.%d", platform, os.Getpid())), "#!/bin/sh\n")
	if err := installCLI(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	exe := GrokExecutable()
	if want := filepath.Join(home, ".grok", "bin", "grok"); exe != want {
		t.Fatalf("grok at %q, want %q (asked %v)", exe, want, asked)
	}
	for _, name := range []string{"grok", "agent"} {
		if l, err := os.Readlink(filepath.Join(home, ".grok", "bin", name)); err != nil || l != filepath.Join("..", "downloads", "grok-"+platform) {
			t.Fatalf("%s links to %q, %v", name, l, err)
		}
	}
	if out, err := exec.Command(exe, "--version").Output(); err != nil || !strings.Contains(string(out), "grok 1.2.3") {
		t.Fatalf("grok --version: %q, %v", out, err)
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".grok", "config.toml")); string(b) != "[cli]\ninstaller = \"internal\"\n" {
		t.Fatalf("config.toml %q", b)
	}
	// where the shell installer runs, it is still the one used
	shellInstallerRuns = func() bool { return true }
	if c, _ := cliFor("grok"); c.native == nil || !strings.Contains(c.sh, "x.ai/cli/install.sh") {
		t.Fatalf("grok's installers %+v", c)
	}
}

// a download that isn't there says so, and the CLI isn't half installed
func TestGrokInstallWithoutShellFails(t *testing.T) {
	home := claudeHome(t)
	t.Setenv("GROK_HOME", "")
	t.Setenv("GROK_BIN_DIR", "")
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cli/stable" {
			w.Write([]byte("1.2.3"))
			return
		}
		w.WriteHeader(404)
	}))
	defer fake.Close()
	old := grokCLIBases
	grokCLIBases = []string{fake.URL + "/cli"}
	t.Cleanup(func() { grokCLIBases = old })
	if _, err := grokPlatform(runtime.GOOS, runtime.GOARCH); err != nil {
		t.Skip(err)
	}
	err := installGrokBuild(context.Background())
	if err == nil || !strings.Contains(err.Error(), "downloading Grok Build 1.2.3") {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".grok", "bin", "grok")); !os.IsNotExist(err) {
		t.Fatalf("a grok was left: %v", err)
	}
}

// The command line's plugin sign-in (`uni-switch plugin login grok`) installs
// Grok Build through InstallPluginCLI, as the window's does; it said
// "install Grok Build first: curl … | bash" in Docker, which has no curl
// (#2223, evier12345). Only a plugin that signs in with a vendor's CLI
// gets one.
func TestInstallPluginCLIInstallsGrokBuild(t *testing.T) {
	home := claudeHome(t)
	exe := filepath.Join(home, ".grok", "bin", "grok")
	oldExe := GrokExecutable
	GrokExecutable = func() string {
		if isFile(exe) {
			return exe
		}
		return ""
	}
	t.Cleanup(func() { GrokExecutable = oldExe })
	var ran []string
	oldRun := runInstaller
	runInstaller = func(ctx context.Context, c agentCLI) ([]byte, error) {
		ran = append(ran, c.Name)
		os.MkdirAll(filepath.Dir(exe), 0o755)
		return nil, testenv.WriteProgram(exe, "#!/bin/sh\n")
	}
	t.Cleanup(func() { runInstaller = oldRun })

	if got := PluginCLIMissing("fakeco"); got != "" {
		t.Fatalf("a plugin that runs no CLI needs %q", got)
	}
	if err := InstallPluginCLI(context.Background(), "fakeco"); err != nil || len(ran) != 0 {
		t.Fatalf("a plugin that runs no CLI: %v, installed %v", err, ran)
	}
	if got := PluginCLIMissing("grok"); got != "Grok Build" {
		t.Fatalf("Grok's plugin without Grok Build needs %q", got)
	}
	if err := InstallPluginCLI(context.Background(), "grok"); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || ran[0] != "Grok Build" || !isFile(exe) {
		t.Fatalf("installed %v", ran)
	}
	if got := PluginCLIMissing("grok"); got != "" {
		t.Fatalf("with Grok Build there, still missing %q", got)
	}
}
