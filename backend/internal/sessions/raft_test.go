package sessions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Raft's agents keep their Pi sessions in their own folders under
// ~/.slock/agents/<agent id>/: .pi-sessions/ for one run on Pi,
// .builtin-sessions/ for one on Raft's built-in runtime, each Pi's SDK
// file, <time>_<id>.jsonl, flat (raft-computer 1.0.14's PiDriver,
// buildPiSessionDir, and BuiltInDriver). No Raft agent here ran on Pi, so
// the files are Pi's own (testdata/pi-calls, Pi 1.1.0's, and testdata/pi),
// moved into Raft's folders with their cwd the agent's folder, as Raft
// starts Pi there. runtime-sessions/ holds Raft's handoff notes, no
// session.
const (
	raftAgentA = "5b1f0c2e-8d3a-4f6b-9c7e-1a2b3c4d5e6f"
	raftAgentB = "7c2e1d3f-9e4b-4a7c-8d6f-2b3c4d5e6f70"
	raftParent = "01a1343b-2288-73d4-b1b4-6fb13abf0069"
	raftFork   = "01a1343b-9a0f-7603-b371-4124126b8b05"
	raftViaUs  = "01a1343b-e8a8-704d-b6cd-046b86c5bf00" // its later calls through uni-switch
)

// raftSetup puts the sessions in slock's folder, the home's .slock unless
// slock is given, and gives back the two agents' folders.
func raftSetup(t *testing.T, slock string) (a, b string) {
	t.Helper()
	claude, _ := manageSetup(t)
	if slock == "" {
		slock = filepath.Join(filepath.Dir(claude), "home", ".slock")
	}
	a, b = filepath.Join(slock, "agents", raftAgentA), filepath.Join(slock, "agents", raftAgentB)
	put := func(from, dir string) {
		t.Helper()
		data, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		ws := filepath.Dir(dir)
		data = []byte(strings.ReplaceAll(string(data), `"cwd":"/work/relay"`, `"cwd":`+jsonString(ws)))
		data = []byte(strings.ReplaceAll(string(data), `"cwd":"/work/pi"`, `"cwd":`+jsonString(ws)))
		os.MkdirAll(dir, 0o755)
		p := filepath.Join(dir, filepath.Base(from))
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-24 * time.Hour)
		os.Chtimes(p, old, old)
	}
	calls, _ := filepath.Glob(filepath.Join("testdata", "pi-calls", "sessions", "*", "*.jsonl"))
	if len(calls) != 3 {
		t.Fatalf("pi-calls: %v", calls)
	}
	for _, f := range calls {
		put(f, filepath.Join(a, ".pi-sessions"))
	}
	put(filepath.Join("testdata", "pi", "sessions", "--work-pi--", "2026-09-24T08-00-00-000Z_"+piFirst+".jsonl"), filepath.Join(b, ".builtin-sessions"))
	// what Raft hands a runtime on, beside the agents: not a session
	os.MkdirAll(filepath.Join(slock, "runtime-sessions"), 0o755)
	os.WriteFile(filepath.Join(slock, "runtime-sessions", "pi-"+raftAgentA+".jsonl"), []byte(`{"type":"handoff","text":"carry on"}`+"\n"), 0o600)
	Reset()
	return a, b
}

func TestRaftSessions(t *testing.T) {
	a, b := raftSetup(t, "")
	ss := List(0)
	for _, s := range ss {
		if s.Agent == "pi" {
			t.Fatalf("a Raft session listed as Pi's: %+v", s)
		}
	}
	p := find(t, ss, "raft", raftParent)
	if p.Cwd != a || p.Title != "ping" || p.Resume != "" {
		t.Fatalf("raft: %+v", p)
	}
	// a fork as Pi's: titled as its parent, its own call alone counted
	if f := find(t, ss, "raft", raftFork); f.Title != "ping" || f.Tokens != (Tokens{Input: 203, Output: 30, CacheRead: 1000}) {
		t.Fatalf("the fork: %+v", f)
	}
	// one on Raft's built-in runtime
	if bi := find(t, ss, "raft", piFirst); bi.Cwd != b || bi.Title != "Parser refactor" {
		t.Fatalf("built-in runtime: %+v", bi)
	}
	n := 0
	for _, s := range ss {
		if s.Agent == "raft" {
			n++
		}
	}
	if n != 4 {
		t.Fatalf("%d Raft sessions, want 4 (the handoff note none)", n)
	}

	// its conversation, the prompts the user typed in
	if !HasTranscript("raft") {
		t.Fatal("no transcript for Raft")
	}
	tr, err := TranscriptOf(p)
	if err != nil {
		t.Fatal(err)
	}
	var said []string
	for _, x := range tr.Parts {
		if x.Role == "user" && x.Kind == "text" {
			said = append(said, x.Text)
		}
	}
	if strings.Join(said, "|") != "ping|again" {
		t.Fatalf("prompts %q in %+v", said, tr.Parts)
	}

	// its calls are Raft's on the Usage page, those through uni-switch the
	// gateway's
	by, relay := map[string]int{}, 0
	for _, c := range Calls(time.Time{}) {
		by[c.Agent]++
		if c.Agent == "raft" && (c.Upstream == "uni-switch" || c.Upstream == "dead") {
			t.Errorf("a call through uni-switch or one that failed: %+v", c)
		}
		if c.Agent == "raft" && (c.Session == raftParent || c.Session == raftFork) && c.Upstream == "relay" && c.Cwd == a {
			relay++
		}
	}
	if relay != 3 || by["pi"] != 0 {
		t.Fatalf("Raft's relay calls %d, want 3 (as TestPiCalls); calls by agent %v", relay, by)
	}

	found := false
	for _, d := range Dirs() {
		found = found || d == filepath.Dir(filepath.Dir(a))
	}
	if !found {
		t.Fatalf("Raft's folder not among %v", Dirs())
	}
}

// Raft's Pi uses the user's own Pi folder, so one sent through uni-switch says
// it is Pi to the gateway under the same id: that is the Raft session, and
// not listed a second time.
func TestRaftIsPiToTheGateway(t *testing.T) {
	raftSetup(t, "")
	ss := List(0)
	merged := MergeExternal(ss, []ExternalSession{
		{Agent: "pi", ID: raftViaUs, Title: "fail"},
		{Agent: "pi", ID: "01a1343b-0000-7000-8000-000000000000", Title: "Pi's own"},
	})
	pis := 0
	for _, s := range merged {
		if s.Agent == "pi" {
			pis++
			if s.ID == raftViaUs {
				t.Fatalf("Raft's session listed again as the gateway's Pi one: %+v", s)
			}
		}
	}
	if pis != 1 {
		t.Fatalf("Pi's own gateway session: %d listed", pis)
	}
	if ids := NativeIDs(map[string]bool{"pi": true})["pi"]; !ids[raftViaUs] || !ids[raftParent] {
		t.Fatalf("Pi's native ids leave Raft's out: %v", ids)
	}
	if ids := NativeIDs(map[string]bool{"raft": true}); !ids["raft"][raftViaUs] || len(ids["pi"]) != 0 {
		t.Fatalf("Raft's native ids: %v", ids)
	}
	_, native := StatsAndNative(0)
	if !native["raft|"+raftViaUs] || !native["pi|"+raftViaUs] {
		t.Fatalf("the gateway's Pi session is counted again: %v", native)
	}
}

// A Raft session goes to the trash as a Pi one does: its one file, and no
// folder beside it (the agent's own workspace is not the session's). Raft
// starts afresh when it finds no file for the id it kept.
func TestRaftDeleteRestore(t *testing.T) {
	a, _ := raftSetup(t, "")
	if !Deletable("raft") {
		t.Fatal("Raft not deletable")
	}
	file := filepath.Join(a, ".pi-sessions", "2026-10-13T07-47-33-129Z_"+raftParent+".jsonl")
	// a folder named as the file would be is not the session's
	beside := strings.TrimSuffix(file, ".jsonl")
	os.MkdirAll(beside, 0o755)
	os.Chtimes(beside, time.Now().Add(-24*time.Hour), time.Now().Add(-24*time.Hour))

	now := time.Now()
	os.Chtimes(file, now, now)
	if _, err := Delete("raft", raftParent); !errors.Is(err, ErrActive) {
		t.Fatalf("a session just written: %v", err)
	}
	old := now.Add(-24 * time.Hour)
	os.Chtimes(file, old, old)

	tr, err := Delete("raft", raftParent)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Items) != 1 || tr.Title != "ping" {
		t.Fatalf("trashed %+v, want the one file", tr)
	}
	if _, err := os.Stat(file); err == nil {
		t.Fatal("the file is still there")
	}
	if _, err := os.Stat(beside); err != nil {
		t.Fatalf("the folder beside it went too: %v", err)
	}
	if _, ok := findSession(List(0), "raft", raftParent); ok {
		t.Fatal("still listed")
	}
	if _, err := Restore(tr.Key); err != nil {
		t.Fatal(err)
	}
	Reset()
	if s, ok := findSession(List(0), "raft", raftParent); !ok || s.Title != "ping" {
		t.Fatalf("the restored session: %+v", s)
	}
}

// $SLOCK_HOME moves Raft's folder, as raft-computer's resolveSlockHome
// reads it.
func TestRaftSlockHome(t *testing.T) {
	other := filepath.Join(t.TempDir(), "slock")
	raftSetup(t, other)
	if _, ok := findSession(List(0), "raft", raftParent); ok {
		t.Fatal("read from ~/.slock with $SLOCK_HOME unset")
	}
	t.Setenv("SLOCK_HOME", other)
	Reset()
	find(t, List(0), "raft", raftParent)
	if SlockDir() != other {
		t.Fatalf("SlockDir %s", SlockDir())
	}
}
