package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// discoveryOut runs `uni-switch accounts discovery` with args and returns what
// it printed.
func discoveryOut(t *testing.T, args ...string) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() { b, _ := io.ReadAll(r); done <- string(b) }()
	err = run(append([]string{"accounts", "discovery"}, args...))
	w.Close()
	os.Stdout = old
	out := <-done
	r.Close()
	if err != nil {
		t.Fatalf("accounts discovery %v: %v", args, err)
	}
	return out
}

// `uni-switch accounts discovery off` saves the opt-out (#1535), `on` takes it
// back, and with no argument it says which is in effect; it leaves every
// other setting as it was.
func TestAccountsDiscoveryCmd(t *testing.T) {
	t.Setenv("UNI_SWITCH_NO_ACCOUNTS", "")
	if err := settings.Save(settings.Settings{Currency: "cny"}); err != nil {
		t.Fatal(err)
	}
	if out := discoveryOut(t); !strings.Contains(out, "is on") {
		t.Errorf("default: %q", out)
	}
	if out := discoveryOut(t, "off"); !strings.Contains(out, "is off:") {
		t.Errorf("off: %q", out)
	}
	if s := settings.Load(); !s.NoAccountDiscovery || s.Currency != "cny" {
		t.Errorf("saved %+v", s)
	}
	discoveryOut(t, "on")
	if settings.Load().NoAccountDiscovery {
		t.Error("on left it off")
	}
	t.Setenv("UNI_SWITCH_NO_ACCOUNTS", "1")
	if out := discoveryOut(t); !strings.Contains(out, "UNI_SWITCH_NO_ACCOUNTS") {
		t.Errorf("under the variable: %q", out)
	}
	if err := run([]string{"accounts", "discovery", "maybe"}); err == nil {
		t.Error("a word other than on or off taken")
	}
}
