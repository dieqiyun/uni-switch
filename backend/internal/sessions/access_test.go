package sessions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// consentHome is setupCalls with settings of its own, and the gate as a
// user's uni-switch has it: nothing settled reads as unsettled, not all-on.
func consentHome(t *testing.T) callDirs {
	t.Helper()
	d := setupCalls(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(filepath.Dir(d.claude), "config"))
	readsAllInTests = false
	forgetSettled()
	t.Cleanup(func() { readsAllInTests = true; forgetSettled(); Reset() })
	return d
}

// writeTwoAgents gives the home a Claude Code session and a Codex one,
// each with a call, and returns the Claude session's file.
func writeTwoAgents(t *testing.T, d callDirs) string {
	t.Helper()
	claude := filepath.Join(d.claude, "projects", "-work-app", "sess1.jsonl")
	writeLines(t, claude,
		`{"type":"user","timestamp":"`+stamp(0)+`","message":{"role":"user","content":"my secret plan"},"cwd":"/work/app","sessionId":"sess1"}`,
		claudeMsg("m1", "claude-opus-5-5", 100, 1, 0, 0, 1))
	name := "rollout-2026-09-20T10-00-00-0190aaaa-1111-7222-8333-444455556666.jsonl"
	lines := []string{`{"timestamp":"` + stamp(0) + `","type":"session_meta","payload":{"id":"thread-id","session_id":"sess-x","cwd":"/work/start"}}`}
	lines = append(lines, cxTurnLines(1, "t1", "gpt-6-astra", "high")...)
	lines = append(lines, tokenCountLine(2, cxUse(1000, 600, 0, 50, 20), cxUse(1000, 600, 0, 50, 20)), cxDoneLine(5, "t1", 5000, 700))
	writeLines(t, filepath.Join(d.codex, "sessions", "2026", "09", "20", name), lines...)
	return claude
}

func agentsOf[T any](xs []T, agent func(T) string) map[string]int {
	out := map[string]int{}
	for _, x := range xs {
		out[agent(x)]++
	}
	return out
}

// RustCats on X: a fresh install read every agent's history before anyone
// agreed. Until the user allows an agent, no route reads its files: the
// Sessions list, the per-agent lists, calls, stats, traces, transcripts
// and search. Allowed one by one, only that agent is read.
func TestFreshInstallReadsNoAgentUntilAllowed(t *testing.T) {
	d := consentHome(t)
	claudeFile := writeTwoAgents(t, d)

	r := SettleReads()
	if r.Asked || r.All || len(r.Agents) > 0 {
		t.Fatalf("a fresh install settled to %+v, want nothing allowed", r)
	}
	if got := settings.Load().SessionReads; got == nil || got.Asked {
		t.Fatalf("the decision was not saved as not asked: %+v", got)
	}
	found := strings.Join(Found(), ",")
	if !strings.Contains(found, "claude") || !strings.Contains(found, "codex") {
		t.Errorf("Found() = %q, want claude and codex (found by their folders alone)", found)
	}

	none := func(when string) {
		t.Helper()
		if l := List(0); len(l) != 0 {
			t.Errorf("%s: List read %d sessions: %+v", when, len(l), l)
		}
		if a := Agents(); len(a) != 0 {
			t.Errorf("%s: Agents() = %+v", when, a)
		}
		if l := ListAgent("claude"); len(l) != 0 {
			t.Errorf("%s: ListAgent(claude) = %d", when, len(l))
		}
		if c := Calls(time.Time{}); len(c) != 0 {
			t.Errorf("%s: Calls read %d calls", when, len(c))
		}
		if c := AgentCalls(time.Time{}, "codex"); len(c) != 0 {
			t.Errorf("%s: AgentCalls(codex) read %d calls", when, len(c))
		}
		if s := CallSources(); len(s) != 0 {
			t.Errorf("%s: CallSources = %+v", when, s)
		}
		if st := StatsAt(0, callT0.Add(time.Hour)); len(st.Keys) != 0 {
			t.Errorf("%s: stats count %v", when, st.Keys)
		}
		if _, err := TranscriptOf(Session{Agent: "claude", ID: "sess1", Path: claudeFile}); !errors.Is(err, ErrNotRead) {
			t.Errorf("%s: TranscriptOf a session listed before read it: %v", when, err)
		}
		if _, _, err := SearchSession(t.Context(), Session{Agent: "claude", ID: "sess1", Path: claudeFile}, "secret"); !errors.Is(err, ErrNotRead) {
			t.Errorf("%s: SearchSession read it: %v", when, err)
		}
	}
	none("not asked")

	// allow selected: Claude Code only
	if err := SetReads(new(bool), map[string]bool{"claude": true, "codex": false}); err != nil {
		t.Fatal(err)
	}
	if got := agentsOf(List(0), func(s Session) string { return s.Agent }); got["claude"] != 1 || got["codex"] != 0 {
		t.Errorf("claude allowed: List agents = %v", got)
	}
	if got := agentsOf(Calls(time.Time{}), func(c Call) string { return c.Agent }); got["claude"] == 0 || got["codex"] != 0 {
		t.Errorf("claude allowed: Calls agents = %v", got)
	}
	if _, err := TranscriptOf(Session{Agent: "claude", ID: "sess1", Path: claudeFile}); errors.Is(err, ErrNotRead) {
		t.Errorf("claude allowed: TranscriptOf still refused")
	}

	// turned off again in Settings: gone at once, though it was read
	if err := SetReads(nil, map[string]bool{"claude": false}); err != nil {
		t.Fatal(err)
	}
	none("claude turned off")

	// allow all
	on := true
	if err := SetReads(&on, map[string]bool{"claude": true, "codex": true}); err != nil {
		t.Fatal(err)
	}
	if got := agentsOf(List(0), func(s Session) string { return s.Agent }); got["claude"] != 1 || got["codex"] != 1 {
		t.Errorf("all allowed: List agents = %v", got)
	}
	if !Reads("gemini") {
		t.Error("allow all leaves out an agent not named (one found later)")
	}
}

// An install that read sessions before the choice existed keeps reading
// them all; a fresh one stays unasked once decided, whatever is cached
// after; and the user's own choice is never replaced.
func TestSettleReadsKeepsWhatWasThere(t *testing.T) {
	t.Run("existing", func(t *testing.T) {
		consentHome(t)
		if err := os.MkdirAll(filepath.Dir(CachePath()), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(CachePath(), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if r := SettleReads(); !r.Asked || !r.All {
			t.Errorf("an install with a sessions cache settled to %+v, want all read", r)
		}
	})
	t.Run("existing call cache", func(t *testing.T) {
		consentHome(t)
		if err := os.MkdirAll(filepath.Join(filepath.Dir(CachePath()), "calls-v14"), 0o755); err != nil {
			t.Fatal(err)
		}
		if r := SettleReads(); !r.Asked || !r.All {
			t.Errorf("an install with a call cache settled to %+v, want all read", r)
		}
	})
	t.Run("fresh, then a cache", func(t *testing.T) {
		consentHome(t)
		if r := SettleReads(); r.Asked {
			t.Fatalf("fresh settled to %+v", r)
		}
		if err := os.MkdirAll(filepath.Dir(CachePath()), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(CachePath(), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		forgetSettled() // a new run
		if r := SettleReads(); r.Asked || Reads("claude") {
			t.Errorf("a fresh install became %+v once a cache was written", r)
		}
	})
	t.Run("user's choice", func(t *testing.T) {
		consentHome(t)
		if err := os.MkdirAll(filepath.Dir(CachePath()), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(CachePath(), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		old := settings.Load() // a snapshot from before the choice
		if err := SetReads(nil, map[string]bool{"codex": true}); err != nil {
			t.Fatal(err)
		}
		forgetSettled()
		if r := SettleReads(); r.All || !r.Reads("codex") || r.Reads("claude") {
			t.Errorf("the user's choice became %+v", r)
		}
		// a save from the old snapshot (another page, sync) keeps it
		if err := settings.Save(old); err != nil {
			t.Fatal(err)
		}
		if r := settings.Load().SessionReads; r == nil || !r.Reads("codex") || r.Reads("claude") {
			t.Errorf("a save from an older snapshot changed the choice to %+v", r)
		}
	})
}
