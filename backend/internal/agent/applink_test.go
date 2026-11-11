package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCommandProgram(t *testing.T) {
	for in, want := range map[string]string{
		`"C:\Users\a b\AppData\Local\Programs\Cindy\Cindy.exe" "%1"`: `C:\Users\a b\AppData\Local\Programs\Cindy\Cindy.exe`,
		`"C:\Program Files\Lody\Lody.exe" -- "%1"`:                   `C:\Program Files\Lody\Lody.exe`,
		`C:\Tools\aionui.exe %1`:                                     `C:\Tools\aionui.exe`,
		`  "C:\x\y.exe"`:                                             `C:\x\y.exe`,
		`"C:\unclosed\y.exe`:                                         `C:\unclosed\y.exe`,
		``:                                                           ``,
		`rundll32.exe url.dll,FileProtocolHandler %1`:                `rundll32.exe`,
	} {
		if got := commandProgram(in); got != want {
			t.Errorf("commandProgram(%q) = %q, want %q", in, got, want)
		}
	}
}

// sungt (10-29): Add uni-switch on Cindy did nothing, as the system starts its
// opener and never says that nothing opens the link. The link is handed
// over only when something does, and otherwise the reason is said.
func TestOpenAppLinkSaysNothingOpensIt(t *testing.T) {
	there := filepath.Join(t.TempDir(), "Cindy.exe")
	if err := os.WriteFile(there, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "gone", "Cindy.exe")
	link := CindyLink("http://127.0.0.1:3425")
	cases := []struct {
		name      string
		exe       string
		found     bool
		err       error
		opens     bool
		wantExe   string
		wantError bool
	}{
		{name: "none", wantError: true},
		{name: "program gone", exe: missing, found: true, wantError: true, wantExe: missing},
		{name: "program there", exe: there, found: true, opens: true},
		{name: "found, no path (macOS)", found: true, opens: true},
		{name: "bare name on PATH", exe: "cindy.exe", found: true, opens: true},
		{name: "unreadable is unknown", err: errors.New("access denied"), opens: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			old := linkHandler
			t.Cleanup(func() { linkHandler = old })
			var asked string
			linkHandler = func(scheme string) (string, bool, error) { asked = scheme; return c.exe, c.found, c.err }
			var opened string
			err := OpenAppLink(link, func(u string) error { opened = u; return nil })
			if asked != "cindy" {
				t.Errorf("asked about %q, want cindy", asked)
			}
			if c.opens != (opened == link) {
				t.Errorf("opened %q", opened)
			}
			var nh *NoLinkHandlerError
			if got := errors.As(err, &nh); got != c.wantError {
				t.Fatalf("err = %v", err)
			}
			if nh != nil && (nh.App != "Cindy" || nh.Scheme != "cindy" || nh.Exe != c.wantExe) {
				t.Errorf("error %+v", nh)
			}
		})
	}
	// every import scheme names its app
	old := linkHandler
	t.Cleanup(func() { linkHandler = old })
	linkHandler = func(string) (string, bool, error) { return "", false, nil }
	for link, app := range map[string]string{LodyLink("http://127.0.0.1:3425"): "Lody", AionUiScheme + "a=1": "AionUi"} {
		var nh *NoLinkHandlerError
		if err := OpenAppLink(link, func(string) error { return nil }); !errors.As(err, &nh) || nh.App != app {
			t.Errorf("%s: %v", link, err)
		}
	}
}

// The system's own answer for a scheme nothing registers: none, not an
// error, where it can be read (the registry, Launch Services, xdg-mime).
func TestSystemLinkHandlerNoSuchScheme(t *testing.T) {
	scheme := fmt.Sprintf("uni-switch-no-such-%d", time.Now().UnixNano())
	exe, found, err := systemLinkHandler(scheme)
	if err != nil {
		t.Skipf("can't be read here: %v", err)
	}
	if found || exe != "" {
		t.Fatalf("%s:// opens %q", scheme, exe)
	}
}
