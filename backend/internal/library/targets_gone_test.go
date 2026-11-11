package library

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// An agent uninstalled outside uni-switch leaves its folder behind (#2177,
// #2173): the Agents page calls it gone (CLIMissing) and offers none of
// its settings, since nothing reads them. The Library kept it as a place
// to give skills, MCP servers and instructions to, and sync carried on
// writing there.
func TestTargetsLeaveGoneAgentsOut(t *testing.T) {
	sandbox(t)
	// Hermes by its config.yaml alone (the reporter's 37 bytes): its
	// folder is kept by nothing here — no hermes on the sandbox's PATH,
	// no hermes-agent checkout beside the config. Where Hermes reads it:
	// ~/.hermes, but %LOCALAPPDATA%\hermes on Windows, where a fixture
	// in ~/.hermes left hermes undetected and Takes refused nothing
	// (#2254, ggbdpq; #2275)
	write(t, filepath.Join(appdir.HermesHome(), "config.yaml"), "custom_providers: []\nmcp_servers: {}\n")
	if a, err := agent.Find("hermes"); err != nil || !a.Detected() {
		t.Fatalf("hermes isn't there by its config.yaml: %v", err)
	}
	if got := ids(Targets()); slices.Contains(got, "hermes") {
		t.Fatalf("hermes is a target with its CLI gone: %v", got)
	}
	// named for one (uni-switch library mcp add … agents=hermes), it is
	// refused, not recorded and then given nothing
	if _, err := Takes("hermes", "mcp"); err == nil || !strings.Contains(err.Error(), "isn't installed") {
		t.Fatalf("Takes(hermes, mcp) with its CLI gone: %v", err)
	}
}

// An agent that goes keeps what uni-switch and the user wrote into its folder:
// a later change writes nothing there and takes nothing away, and its
// instructions file is still its own, never a file a set is kept in. Pi's,
// which goes on every system: on Windows a ~\.codex whose CLI isn't
// found is never taken for a leftover, since other programs there read it
// too (#1482), so this test with codex failed there (#2254, ggbdpq).
func TestGoneAgentKeepsWhatItHas(t *testing.T) {
	h := sandbox(t)
	shared := "Be brief."
	ok(t)(SaveInstructions(InstructionsChange{Shared: &shared, Agents: []string{"pi"}}))
	file := filepath.Join(h, ".pi", "agent", "AGENTS.md")
	write(t, file, "my own notes\n"+read(t, file))
	before := read(t, file)
	if !strings.Contains(before, "Be brief.") {
		t.Fatalf("pi wasn't given the instructions:\n%s", before)
	}
	for _, f := range []string{"pi", "pi.exe"} {
		if err := os.Remove(filepath.Join(h, "bin", f)); err != nil {
			t.Fatal(err)
		}
	}
	if slices.Contains(ids(Targets()), "pi") {
		t.Fatal("pi is a target with its CLI gone")
	}
	other := "Be thorough."
	ok(t)(SaveInstructions(InstructionsChange{Shared: &other}))
	if got := read(t, file); got != before {
		t.Errorf("a change after pi went rewrote its instructions:\n%s\nwas:\n%s", got, before)
	}
	// with no part of uni-switch's in it, only its being pi's own refuses it
	write(t, file, "my own notes\n")
	if _, err := SaveInstructions(InstructionsChange{Create: &InstrSet{ID: "b", Name: "B", File: file}}); err == nil {
		t.Error("a set was kept in a gone agent's own instructions file")
	}
}

// rtk is a Library page's too (#2254): a gone agent isn't offered it, and
// one left with rtk's hook still shows, for the hook to be taken out.
func TestRTKLeavesGoneAgentsOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake rtk is a shell script")
	}
	h := sandbox(t)
	bin := filepath.Join(h, "bin")
	testenv.Program(t, filepath.Join(bin, "rtk"), fakeRTK)
	for _, f := range []string{"codex", "codex.exe"} {
		if err := os.Remove(filepath.Join(bin, f)); err != nil {
			t.Fatal(err)
		}
	}
	listed := func() bool {
		return slices.ContainsFunc(ReadRTK().Agents, func(a RTKAgent) bool { return a.ID == "codex" })
	}
	if listed() {
		t.Fatal("codex is offered rtk with its CLI gone")
	}
	if _, err := SetRTK("codex", true); err == nil {
		t.Fatal("rtk was given to codex with its CLI gone")
	}
	hooks := filepath.Join(h, ".codex", "hooks.json")
	write(t, hooks, `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"rtk hook codex"}]}]}}`)
	if !listed() {
		t.Fatal("codex, left with rtk's hook, isn't shown to take it out")
	}
	ok := func(v *RTKView, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	ok(SetRTK("codex", false))
	if strings.Contains(read(t, hooks), "rtk hook codex") {
		t.Fatalf("rtk's hook is still in codex:\n%s", read(t, hooks))
	}
}
