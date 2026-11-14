package sessions

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// #1607 (infinitr0us): Kimi Code's own calls reach the usage ledger. The
// sessions in testdata/kimi-code are the reporter's real wire files from
// two machines (redacted by them: text replaced, every id, model and number
// kept), cut down to the lines a call is read from (metadata, llm.request,
// step.end, usage.record, turn.cancel), each line as it was:
//
//   - wd_v14-june-subagent: the June 1.4 build, no llm.request and no
//     messageId; main and one subagent (agent-0), 25 + 7 calls
//   - wd_v14-july-subagents: a July 1.4 subagent's file (agent-0), 10 calls
//   - wd_v14-cancel: a turn cancelled mid-request, whose last llm.request
//     has no usage.record, 6 calls
//   - wd_v15-early-no-agentid: 1.5 records written before agentId, 10 calls
//   - wd_v15-uni-switch-failed: a failed call through uni-switch, no usage.record
//   - wd_v14-no-turn, wd_v14-reopened-ses, wd_v10-migrated: no calls
//
// The totals for the whole files (32, 151, 6 and 10 calls) are the
// reporter's README's, and the reader gives the same on them.
type kimiTotal struct{ n, input, cacheRead, cacheWrite, output int }

var kimiWant = map[string]kimiTotal{
	"session_a0a7c8d3-2a5a-480d-9d42-740257598abd": {32, 132475 + 68345, 2399488 + 241664, 0, 12283},
	"session_1e5ea5b2-382a-4788-aa81-dc0e5b99f818": {10, 48900, 312832, 0, 9249},
	"session_169766c9-86f5-4577-97a3-a51d402db5b9": {6, 15726, 132096, 0, 2009},
	"session_fc16731e-2bfe-4873-a6cb-4b9e73280360": {10, 20888, 317440, 0, 2176},
}

func kimiCalls() []Call {
	var out []Call
	for _, c := range Calls(time.Time{}) {
		if c.Agent == "kimi" {
			out = append(out, c)
		}
	}
	return out
}

func kimiTotals(cs []Call) map[string]kimiTotal {
	out := map[string]kimiTotal{}
	for _, c := range cs {
		t := out[c.Session]
		t.n++
		t.input += c.Input
		t.cacheRead += c.CacheRead
		t.cacheWrite += c.CacheWrite
		t.output += c.Output
		out[c.Session] = t
	}
	return out
}

// kimiHome is a sandbox's Kimi Code folder holding the reporter's sessions.
func kimiHome(t *testing.T) string {
	claude, _ := setup(t)
	dir := filepath.Join(filepath.Dir(claude), "home", ".kimi-code")
	copyTree(t, "testdata/kimi-code", dir)
	return dir
}

func TestKimiCodeCalls(t *testing.T) {
	for _, viaEnv := range []bool{false, true} {
		name := "~/.kimi-code"
		if viaEnv {
			name = "KIMI_CODE_HOME"
		}
		t.Run(name, func(t *testing.T) {
			claude, _ := setup(t)
			dir := filepath.Join(filepath.Dir(claude), "home", ".kimi-code")
			if viaEnv {
				dir = filepath.Join(filepath.Dir(claude), "elsewhere")
				t.Setenv("KIMI_CODE_HOME", dir)
			}
			copyTree(t, "testdata/kimi-code", dir)
			got := kimiCalls()
			if tot := kimiTotals(got); !equalTotals(tot, kimiWant) {
				t.Fatalf("totals by session\n got %+v\nwant %+v", tot, kimiWant)
			}
			var first *Call
			for i, c := range got {
				if c.Msg == "" {
					t.Errorf("call %+v has no key to count it once by", c)
				}
				if c.Time.Equal(time.UnixMilli(1781503063433)) {
					first = &got[i]
				}
			}
			if first == nil {
				t.Fatal("the June subagent's first call is missing")
			}
			if first.Model != "kimi-code/kimi-for-coding" || first.Session != "session_a0a7c8d3-2a5a-480d-9d42-740257598abd" ||
				first.Tokens != (Tokens{Input: 2823, Output: 253, CacheRead: 6400}) {
				t.Errorf("first call %+v", *first)
			}
		})
	}
}

func equalTotals(a, b map[string]kimiTotal) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// kimiRecordLine is a 1.5 usage.record with agentId, as the reporter quoted
// it from their own file on the issue.
const kimiRecordLine = `{"type":"usage.record","agentId":"main","model":"kimi-code/k3","usage":{"inputOther":20257,"output":230,"inputCacheRead":0,"inputCacheCreation":0},"usageScope":"turn","time":1791696952640}`

func writeKimiWire(t *testing.T, home, session, agent string, lines ...string) {
	t.Helper()
	p := filepath.Join(home, "sessions", "wd_test_000000000009", session, "agents", agent, "wire.jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A call Kimi made through uni-switch's own entry (a model table keyed
// uni-switch/<provider>/<model>, as the reporter's failed call's modelAlias
// is) is the gateway's to count. The record is the quoted one with that
// model: no successful call through uni-switch was in the files shared.
func TestKimiCodeSkipsCallsThroughUniSwitch(t *testing.T) {
	home := kimiHome(t)
	viaUniSwitch := strings.Replace(kimiRecordLine, `"kimi-code/k3"`, `"uni-switch/mistral/zai-glm-5-3"`, 1)
	viaUniSwitch = strings.Replace(viaUniSwitch, `1791696952640`, `1791696960000`, 1)
	writeKimiWire(t, home, "session_through-uni-switch", "main", kimiRecordLine, viaUniSwitch, "")
	var got []Call
	for _, c := range kimiCalls() {
		if c.Session == "session_through-uni-switch" {
			got = append(got, c)
		}
	}
	if len(got) != 1 || got[0].Model != "kimi-code/k3" {
		t.Fatalf("want only the call on Kimi's own sign-in, got %+v", got)
	}
	if want := (Tokens{Input: 20257, Output: 230}); got[0].Tokens != want || !got[0].Time.Equal(time.UnixMilli(1791696952640)) {
		t.Errorf("call %+v", got[0])
	}
}

// kimi fork copies its parent's records into the new session unchanged:
// each call counts once, in the session that made it.
func TestKimiCodeForkCountsOnce(t *testing.T) {
	home := kimiHome(t)
	parent := filepath.Join(home, "sessions", "wd_v14-cancel_000000000003", "session_169766c9-86f5-4577-97a3-a51d402db5b9", "agents", "main", "wire.jsonl")
	b, err := os.ReadFile(parent)
	if err != nil {
		t.Fatal(err)
	}
	// a fork of a 1.4 session, then a 1.5 call of its own
	own := strings.Replace(kimiRecordLine, `1791696952640`, `1791696970000`, 1)
	writeKimiWire(t, home, "session_fork", "main", strings.TrimRight(string(b), "\n"), `{"type":"forked","time":1791696950000}`, own, "")
	later := time.Now().Add(time.Hour)
	fork := filepath.Join(home, "sessions", "wd_test_000000000009", "session_fork", "agents", "main", "wire.jsonl")
	if err := os.Chtimes(fork, later, later); err != nil {
		t.Fatal(err)
	}
	// and two subagents of one session whose 1.5 records name themselves:
	// the same call in two agents' files is two calls
	sub := strings.Replace(kimiRecordLine, `"agentId":"main"`, `"agentId":"agent-0"`, 1)
	writeKimiWire(t, home, "session_agents", "agent-0", sub, "")
	writeKimiWire(t, home, "session_agents", "main", kimiRecordLine, "")

	tot := kimiTotals(kimiCalls())
	if got := tot["session_169766c9-86f5-4577-97a3-a51d402db5b9"]; got != kimiWant["session_169766c9-86f5-4577-97a3-a51d402db5b9"] {
		t.Errorf("the parent's calls %+v", got)
	}
	if got := tot["session_fork"]; got.n != 1 || got.input != 20257 {
		t.Errorf("the fork's own calls %+v, want its one call after the fork", got)
	}
	if got := tot["session_agents"]; got.n != 2 {
		t.Errorf("two agents' calls at one time %+v, want 2", got)
	}
}

// A file cut short, empty or holding lines that aren't records reads as
// what it holds: no crash, and no call made up.
func TestKimiCodeBrokenFiles(t *testing.T) {
	home := kimiHome(t)
	src := filepath.Join(home, "sessions", "wd_v14-cancel_000000000003", "session_169766c9-86f5-4577-97a3-a51d402db5b9", "agents", "main", "wire.jsonl")
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	// the copy below would count once with its original: take that away
	if err := os.RemoveAll(filepath.Dir(filepath.Dir(filepath.Dir(src)))); err != nil {
		t.Fatal(err)
	}
	// cut in the middle of the last usage.record: 5 whole calls are left
	last := bytes.LastIndex(b, []byte(`{"type":"usage.record"`))
	if last < 0 {
		t.Fatal("no usage.record in the fixture")
	}
	writeKimiWire(t, home, "session_cut", "main", string(b[:last+60]))
	writeKimiWire(t, home, "session_empty", "main")
	writeKimiWire(t, home, "session_junk", "main",
		"\x00\xff not json",
		`{"type":"usage.record"`,
		`{"type":"usage.record","model":"kimi-code/k3","usage":null,"time":1791696952640}`,
		`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":-5,"output":3},"time":1791696952641}`,
		`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":5,"output":3}}`,
		`{"type":"usage.record","model":"kimi-code/k3","usage":{"inputOther":0,"output":0},"time":1791696952642}`,
		`{"type":"context.append_loop_event","event":{"type":"step.end","usage":{"inputOther":7,"output":1},"messageId":"usage.record"},"time":1791696952643}`,
		`["usage.record"]`,
		strings.Replace(kimiRecordLine, `"agentId":"main"`, `"agentId":"main","usage":"x"`, 1),
	)
	tot := kimiTotals(kimiCalls())
	if got := tot["session_cut"]; got.n != 5 || got.input+got.cacheRead != 147822-(468+25856) {
		// the last record of the cancel session: inputOther 468, cache 25856
		t.Errorf("cut file %+v, want its 5 whole calls", got)
	}
	for _, s := range []string{"session_empty", "session_junk"} {
		if got, ok := tot[s]; ok {
			t.Errorf("%s: %+v, want no calls", s, got)
		}
	}
}

// A Kimi Code wire file that grew is read on from where the last read of
// its calls left off, not whole again; a half-written last line waits for
// the rest; a file cut shorter or written over is read whole again. Before
// this, every poll after an append decoded the whole file again.
func TestKimiCodeCallsReadOnlyWhatWasAppended(t *testing.T) {
	home := kimiHome(t)
	rec := func(i int) string {
		r := strings.Replace(kimiRecordLine, `1791696952640`, strconv.FormatInt(1791696900000+int64(i)*1000, 10), 1)
		return strings.Replace(r, `"inputOther":20257`, `"inputOther":`+strconv.Itoa(20000+i), 1)
	}
	writeKimiWire(t, home, "session_grow", "main", rec(1), rec(2), rec(3), "")
	path := filepath.Join(home, "sessions", "wd_test_000000000009", "session_grow", "agents", "main", "wire.jsonl")
	// the listing folds state.json's size into the main wire file's
	if err := os.WriteFile(filepath.Join(home, "sessions", "wd_test_000000000009", "session_grow", "state.json"), []byte(`{"title":"Grow","cwd":"/work/grow"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var starts []int64
	scanHook = func(p string, off int64) {
		if p == path {
			mu.Lock()
			starts = append(starts, off)
			mu.Unlock()
		}
	}
	t.Cleanup(func() { scanHook = nil })
	read := func(what string, from int64, want ...int) {
		t.Helper()
		mu.Lock()
		starts = nil
		mu.Unlock()
		var calls []Call
		for _, s := range CallSources() {
			if s.Path == path {
				calls = ReadCallSource(s)
			}
		}
		mu.Lock()
		got := append([]int64(nil), starts...)
		mu.Unlock()
		if len(got) == 0 {
			t.Fatalf("%s: not read", what)
		}
		for _, off := range got {
			if off != from {
				t.Fatalf("%s: read from %d, want %d (%v)", what, off, from, got)
			}
		}
		var in []int
		for _, c := range calls {
			in = append(in, c.Input-20000)
		}
		if !slices.Equal(in, want) {
			t.Fatalf("%s: calls %v, want %v", what, in, want)
		}
	}
	size := func() int64 {
		t.Helper()
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return st.Size()
	}
	read("first read", 0, 1, 2, 3)

	end := size()
	appendText(t, path, rec(4)+"\n"+rec(5)[:40])
	read("appended, half a line after", end, 1, 2, 3, 4)

	appendText(t, path, rec(5)[40:]+"\n")
	read("the half line finished", end+int64(len(rec(4))+1), 1, 2, 3, 4, 5)

	// cut shorter: read whole again
	writeKimiWire(t, home, "session_grow", "main", rec(7), "")
	read("cut shorter", 0, 7)

	// written over, longer than it was: read whole again
	writeKimiWire(t, home, "session_grow", "main", rec(8), rec(9), "")
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	read("written over", 0, 8, 9)
}
