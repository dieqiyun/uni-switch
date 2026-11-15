//go:build !nogui

package gui

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/webview2env"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// wailsBrowserArgs is the command line Wails v3.0.0-beta.24 makes from the
// app's Windows options (setupChromium, webview_window_windows.go): its own
// msSmartScreenProtection first among the disabled features, each list
// once as one switch, then the other arguments as given.
func wailsBrowserArgs(o application.WindowsOptions) []string {
	var disabled []string
	for _, f := range append([]string{"msSmartScreenProtection"}, o.DisabledFeatures...) {
		if !slices.Contains(disabled, f) {
			disabled = append(disabled, f)
		}
	}
	args := []string{"--disable-features=" + strings.Join(disabled, ",")}
	if len(o.EnabledFeatures) > 0 {
		args = append(args, "--enable-features="+strings.Join(o.EnabledFeatures, ","))
	}
	return append(args, o.AdditionalBrowserArgs...)
}

// #2753 (LuYquan): uni-switch's own --disable-features made WebView2 skip the
// user's WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS, so RendererCodeIntegrity
// stayed on and an injected DLL got the renderer killed. Now the user's
// arguments are merged into uni-switch's: one --disable-features holding both
// lists (Chromium keeps only the last of two), one --enable-features, and
// every other argument as written.
func TestWebView2ArgsMergeTheUsers(t *testing.T) {
	for _, c := range []struct {
		name, env string
		want      []string
	}{
		{"none set", "", []string{"--disable-features=msSmartScreenProtection"}},
		{"the reporter's", "--disable-features=RendererCodeIntegrity",
			[]string{"--disable-features=msSmartScreenProtection,RendererCodeIntegrity"}},
		{"twice, with uni-switch's own and a repeat", `--disable-features=A,msSmartScreenProtection --disable-features="B, A"`,
			[]string{"--disable-features=msSmartScreenProtection,A,B"}},
		{"enable and others kept", "  --enable-features=X,Y\t--remote-debugging-port=9333 --proxy-server=\"http://h:1 x\" --enable-features=Y,Z --lang=de ",
			[]string{"--disable-features=msSmartScreenProtection", "--enable-features=X,Y,Z", "--remote-debugging-port=9333", `--proxy-server="http://h:1 x"`, "--lang=de"}},
		{"a bare switch", "--disable-features", []string{"--disable-features=msSmartScreenProtection"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			was := webview2env.Args
			t.Cleanup(func() { webview2env.Args = was })
			webview2env.Args = c.env
			got := wailsBrowserArgs(windowsOptions())
			if fmt.Sprint(got) != fmt.Sprint(c.want) {
				t.Fatalf("WebView2 is started with %q, want %q", got, c.want)
			}
			n := 0
			for _, a := range got {
				if strings.HasPrefix(a, "--disable-features") {
					n++
				}
			}
			if n != 1 {
				t.Fatalf("%d --disable-features in %q, Chromium keeps only the last", n, got)
			}
		})
	}
}

// Nothing in the defaults turns renderer code integrity off: only a user
// who asks for it gets it (#2753).
func TestWebView2KeepsCodeIntegrityByDefault(t *testing.T) {
	was := webview2env.Args
	t.Cleanup(func() { webview2env.Args = was })
	webview2env.Args = ""
	if got := strings.Join(wailsBrowserArgs(windowsOptions()), " "); strings.Contains(got, "RendererCodeIntegrity") {
		t.Fatalf("default arguments %q turn RendererCodeIntegrity off", got)
	}
}

// On Windows Wails' WebView2 loader blanks the variable in its init, which
// runs before uni-switch's code (#2753: the reporter's flag never arrived).
// The value uni-switch was started with still reaches the options: this test
// starts its own binary with the variable set, where the loader's init has
// run as in the app.
func TestWebView2ArgsSurviveWailsInit(t *testing.T) {
	if os.Getenv("UNI_SWITCH_TEST_WEBVIEW2_CHILD") == "1" {
		fmt.Printf("os=%q disabled=%q\n", os.Getenv(webview2env.Name), windowsOptions().DisabledFeatures)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestWebView2ArgsSurviveWailsInit$", "-test.count=1")
	cmd.Env = append(os.Environ(), "UNI_SWITCH_TEST_WEBVIEW2_CHILD=1", webview2env.Name+"=--disable-features=RendererCodeIntegrity")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `disabled=["RendererCodeIntegrity"]`) {
		t.Fatalf("the started value didn't reach the options:\n%s", out)
	}
	t.Logf("%s", strings.SplitN(string(out), "\n", 2)[0])
}
