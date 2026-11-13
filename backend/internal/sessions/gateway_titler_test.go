package sessions

import (
	"os"
	"testing"
	"time"
)

// A listing's titler asks once whether uni-switch recorded anything: with
// nothing recorded, each of its many sessions costs nothing, not a Stat and
// a folder name (#2278). Once something is recorded, a titler made after
// finds the title as GatewayTitle does.
func TestGatewayTitlerAsksOnce(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := os.Stat(gatewayDir()); err == nil {
		t.Fatal("a fresh config folder has recordings")
	}
	title := GatewayTitler()
	if n := testing.AllocsPerRun(100, func() { _ = title("pi", "ses_1") }); n != 0 {
		t.Fatalf("a session's title with nothing recorded made %v allocations", n)
	}
	if err := SetGatewayRecording(true, false); err != nil {
		t.Fatal(err)
	}
	turn := GatewayTurn{Agent: "pi", Session: "ses_1", Time: time.Now().UTC(),
		Input: []Part{{Role: "user", Kind: "text", Text: "Deploy the staging server"}}}
	if err := SaveGatewayTurn(turn); err != nil {
		t.Fatal(err)
	}
	title = GatewayTitler()
	if got := title("pi", "ses_1"); got != "Deploy the staging server" || got != GatewayTitle("pi", "ses_1") {
		t.Fatalf("titler's title %q, GatewayTitle's %q", got, GatewayTitle("pi", "ses_1"))
	}
	if got := title("pi", "ses_2"); got != "" {
		t.Fatalf("another session's title %q", got)
	}
}
