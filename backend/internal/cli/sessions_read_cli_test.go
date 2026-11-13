package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// uni-switch sessions read (RustCats on X): a fresh install reads no agent's
// sessions, so the list is empty and the agents found are off until
// `read on` allows them; `read off all` turns every one off again. A flag
// or an unknown agent is refused, never saved.
func TestSessionsRead(t *testing.T) {
	now := sessionsHome(t)
	// a fresh install as SettleReads leaves it: decided, not asked
	if err := settings.SetSessionReads(func(*settings.SessionReads) *settings.SessionReads {
		return &settings.SessionReads{}
	}); err != nil {
		t.Fatal(err)
	}
	read := func(args ...string) (string, error) {
		var b bytes.Buffer
		err := sessionReadsTo(&b, args)
		return b.String(), err
	}
	out, err := read()
	if err != nil {
		t.Fatal(err)
	}
	wantAll(t, out, "Not asked yet", "off  Claude Code (claude)", "off  Codex (codex)")
	if l := runSessions(t, now); strings.Contains(l, "Fix login bug") || strings.Contains(l, "Add a README") {
		t.Fatalf("not asked, the list read sessions:\n%s", l)
	}

	out, err = read("on", "claude")
	if err != nil {
		t.Fatal(err)
	}
	wantAll(t, out, "Agents found later: not read", "on   Claude Code (claude)", "off  Codex (codex)")
	l := runSessions(t, now)
	if !strings.Contains(l, "Fix login bug") || strings.Contains(l, "Add a README") {
		t.Fatalf("claude allowed, the list:\n%s", l)
	}

	if _, err := read("off", "all"); err != nil {
		t.Fatal(err)
	}
	if l := runSessions(t, now); strings.Contains(l, "Fix login bug") {
		t.Fatalf("off all, the list still read claude:\n%s", l)
	}

	before := settings.Load().SessionReads
	for _, bad := range [][]string{{"--help"}, {"on"}, {"on", "--help"}, {"on", "no-such-agent"}, {"maybe", "claude"}} {
		if _, err := read(bad...); err == nil || !strings.Contains(err.Error(), "usage: uni-switch sessions read") {
			t.Errorf("read %v: %v, want the usage", bad, err)
		}
	}
	if after := settings.Load().SessionReads; after.All != before.All || len(after.Agents) != len(before.Agents) {
		t.Errorf("a refused command changed the choice: %+v to %+v", before, after)
	}
}
