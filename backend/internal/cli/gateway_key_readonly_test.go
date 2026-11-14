package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// gateway-key add --read-only and gateway-key access (Steven on Discord: a
// desk display's key reads stats and uses no model).
func TestGatewayKeyCLIReadOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := settings.Save(settings.Settings{}); err != nil {
		t.Fatal(err)
	}
	call := func(args ...string) string {
		t.Helper()
		var out bytes.Buffer
		if err := gatewayKeysTo(&out, append([]string{"gateway-key"}, args...)); err != nil {
			t.Fatal(args, err)
		}
		return strings.TrimSpace(out.String())
	}
	display := call("add", "Desk display", "--read-only")
	laptop := call("add", "--read-only", "Laptop", "--key", "cpa-laptop-key-0042")
	if laptop != "cpa-laptop-key-0042" {
		t.Fatal("--read-only beside --key:", laptop)
	}
	for _, s := range []string{display, laptop} {
		if who, ok := access.Authenticate(s); !ok || !who.ReadOnly {
			t.Fatal("added read-only:", who, ok)
		}
	}
	keys, _ := access.List()
	id := keys[1].ID
	if got := call("list"); !strings.Contains(got, "ACCESS") || strings.Count(got, "read-only") != 2 {
		t.Fatal("list:", got)
	}
	if got := call("access", id); !strings.Contains(got, "Laptop: read-only") {
		t.Fatal("access:", got)
	}
	if got := call("access", id, "full"); !strings.Contains(got, "Laptop: full access") {
		t.Fatal("access full:", got)
	}
	if who, _ := access.Authenticate(laptop); who.ReadOnly {
		t.Fatal("still read-only:", who)
	}
	if got := call("list"); strings.Count(got, "read-only") != 1 || !strings.Contains(got, "full") {
		t.Fatal("list after:", got)
	}
	call("access", id, "read-only")
	if who, _ := access.Authenticate(laptop); !who.ReadOnly {
		t.Fatal("not read-only:", who)
	}
	for _, args := range [][]string{{"access"}, {"access", id, "write"}, {"access", id, "full", "x"}, {"access", "missing", "full"}} {
		if err := gatewayKeysTo(&bytes.Buffer{}, append([]string{"gateway-key"}, args...)); err == nil {
			t.Error("accepted", args)
		}
	}
	help := helpFor([]string{"gateway-key", "--help"})
	if !strings.Contains(help, "--read-only") || !strings.Contains(help, "gateway-key access <id> [read-only|full]") {
		t.Fatal("help:", help)
	}
}
