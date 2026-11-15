package proc

import (
	"path/filepath"
	"testing"
)

// Windows' rundll32 is named by its path in the system folder: by its bare
// name a PATHEXT without .EXE (WSL passed one on as ".CPL") found nothing,
// and the sign-in's page never opened (#2043, xiaobaifly7).
func TestBrowserNamesRundll32ByItsPath(t *testing.T) {
	url := "https://mcp.cloudflare.com/authorize?state=a&b=c"
	name, args := browserCommand("windows", `C:\WINDOWS`, url)
	if want := filepath.Join(`C:\WINDOWS`, "System32", "rundll32.exe"); name != want {
		t.Fatalf("launcher %q, want %q", name, want)
	}
	if len(args) != 2 || args[0] != "url.dll,FileProtocolHandler" || args[1] != url {
		t.Fatalf("args %q", args)
	}
	// no SystemRoot to go by: the bare name, as before
	if name, _ := browserCommand("windows", "", url); name != "rundll32" {
		t.Fatalf("without SystemRoot: %q", name)
	}
	for goos, want := range map[string]string{"darwin": "open", "linux": "xdg-open", "android": "termux-open-url"} {
		if name, args := browserCommand(goos, "", url); name != want || len(args) != 1 || args[0] != url {
			t.Errorf("%s: %q %q", goos, name, args)
		}
	}
}
