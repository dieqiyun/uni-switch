//go:build windows && !nogui

package gui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// application.New is one per process, so each case runs in a child of this
// test binary (ufec's way in #1806).
const testWebView2Env = "UNI_SWITCH_TEST_WEBVIEW2"

func TestWebView2Child(t *testing.T) {
	mode := os.Getenv(testWebView2Env)
	if mode == "" {
		t.Skip("not the child")
	}
	opts := application.Options{Name: "webview2-check"}
	if mode != "system" {
		// a runtime folder that isn't one: Wails' lookup fails as it does
		// on a machine with none
		opts.Windows.WebviewBrowserPath = mode
	}
	if webView2Missing(application.New(opts).Env.Info().PlatformInfo) {
		os.Exit(1)
	}
	os.Exit(0)
}

func webView2Child(t *testing.T, mode string) int {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "-test.run=^TestWebView2Child$")
	cmd.Env = append(os.Environ(), testWebView2Env+"="+mode)
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ee):
		return ee.ExitCode()
	}
	t.Fatalf("child: %v\n%s", err, out)
	return -1
}

// Wails' own lookup, as Run reads it, says missing for a runtime that isn't
// there, and not for this machine's.
func TestWebView2MissingFromWails(t *testing.T) {
	if got := webView2Child(t, filepath.Join(t.TempDir(), "no-runtime")); got != 1 {
		t.Fatalf("no runtime: child exit %d, want 1 (missing)", got)
	}
	if got := webView2Child(t, "system"); got != 0 {
		t.Skipf("this machine has no WebView2 runtime itself (exit %d)", got)
	}
}
