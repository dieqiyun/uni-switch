package gateway

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// parseMemoBody is a Claude Code conversation in a folder, long enough to
// be held, with a tool result in it.
func parseMemoBody(tag string) []byte {
	return []byte(`{"model":"` + tag + `","system":" - Primary working directory: /work/` + tag + `\n","messages":[` +
		`{"role":"user","content":"fix it"},` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"path":"a"}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"` + strings.Repeat("line of a file\\n", 4000) + `"}]}]}`)
}

// The turn count (turnOf) and the folder (dirOf, tellWorkPlace) of a
// request being served are read from one parse of its body, not one each:
// each decoded every message and tool output of a long conversation again
// (yg6888 on X: ~1 GB). What they say is what a parse of their own says.
func TestReadersOfAHeldBodyParseItOnce(t *testing.T) {
	ctx, done := context.WithCancel(context.Background())
	defer done()
	body := parseMemoBody(t.Name())
	holdBody(ctx, body)

	want, err := parse(provider.Anthropic, bytes.Clone(body))
	if err != nil {
		t.Fatal(err)
	}
	wantDir := "/work/" + t.Name()
	if workDir(want) != wantDir {
		t.Fatalf("the fixture's folder reads %q", workDir(want))
	}
	wantTurn, wantWithin := turnIn(want)
	var turn int
	var within bool
	var dir string
	h := http.Header{}
	readers := allocated(func() {
		turn, within = turnOf(provider.Anthropic, body)
		dir = dirOf(provider.Anthropic, body)()
		tellWorkPlace(h, provider.Anthropic, body)
	})
	if turn != wantTurn || within != wantWithin || turn != 1 || !within {
		t.Fatalf("turnOf = %d %v, a parse of its own says %d %v", turn, within, wantTurn, wantWithin)
	}
	if dir != wantDir {
		t.Fatalf("dirOf = %q, want %q", dir, wantDir)
	}
	if got, _ := url.PathUnescape(h.Get(provider.WorkDirHeader)); got != wantDir {
		t.Fatalf("tellWorkPlace said %q, want %q", got, wantDir)
	}
	whole := allocated(func() { parse(provider.Anthropic, body) })
	// Under -race, sync.Pool drops a quarter of what is put in it, and
	// workDirIn's regexps take their matchers (~37 KB for this body) from
	// one, so the readers' bytes swing by whole matchers: 1.3× to 2.1×
	// a parse over five tries, against a parse's steady 135 KB.
	if !raceOn && readers > whole*3/2 {
		t.Fatalf("turnOf, dirOf and tellWorkPlace allocate %d bytes on a held body, a parse %d: they parse it each", readers, whole)
	}
	// and again, nothing: what was read is kept while the body is held
	if again := allocated(func() { turnOf(provider.Anthropic, body); dirOf(provider.Anthropic, body)() }); again > whole/8 {
		t.Fatalf("a held body's second readers allocate %d bytes, a parse %d", again, whole)
	}

	// another protocol's reading of the same bytes is its own
	if got, want := factsOf(provider.Chat, body), readFacts(provider.Chat, body); got != want {
		t.Fatalf("a held body read as Chat says %+v, a parse of its own %+v", got, want)
	}
	if got := factsOf(provider.Anthropic, body); got.dir != wantDir || got.turn != 1 {
		t.Fatalf("read as Messages again after Chat: %+v", got)
	}
	// a body not held, of the same bytes, is parsed afresh: nothing says
	// its memory isn't written over
	clone := bytes.Clone(body)
	var fresh atomic.Bool
	seen := func(b []byte) {
		if unsafe.SliceData(b) == unsafe.SliceData(clone) {
			fresh.Store(true)
		}
	}
	factsParsed.Store(&seen)
	defer factsParsed.Store(nil)
	if factsOf(provider.Anthropic, clone); !fresh.Load() {
		t.Fatal("a body not held wasn't read: it got a held body's facts")
	}
}

// What is kept goes when the request ends, so the body's memory, given to
// another body, never reads as this one, and a body no longer held is not
// listed again.
func TestHeldFactsGoWithTheirBody(t *testing.T) {
	ctx, done := context.WithCancel(context.Background())
	body := parseMemoBody(t.Name())
	holdBody(ctx, body)
	factsOf(provider.Anthropic, body)
	key := unsafe.SliceData(body)
	listed := func() bool {
		heldBodies.Lock()
		defer heldBodies.Unlock()
		_, ok := heldBodies.facts[key]
		return ok
	}
	if !listed() {
		t.Fatal("a held body's facts were not kept for its next reader")
	}
	done()
	for deadline := time.Now().Add(5 * time.Second); listed(); {
		if time.Now().After(deadline) {
			t.Fatal("a body's facts are still listed after its request ended")
		}
		time.Sleep(time.Millisecond)
	}
	if f := factsOf(provider.Anthropic, body); f.dir != "/work/"+t.Name() {
		t.Fatalf("a released body reads %+v", f)
	}
	if listed() {
		t.Fatal("a body no longer held got its facts listed")
	}
	// a part of a held body is not the body
	ctx2, done2 := context.WithCancel(context.Background())
	defer done2()
	holdBody(ctx2, body)
	factsOf(provider.Anthropic, body[:len(body)-1])
	if listed() {
		t.Fatal("a held body's prefix got facts listed under the body")
	}
}

// The facts of a Responses body read without the assistant's words and the
// tools' outputs (readResponses for facts) are those its whole parse says:
// real Codex requests, and the items that move a turn or a folder.
func TestResponsesFactsAsTheWholeParse(t *testing.T) {
	bodies := map[string][]byte{
		"large": largeConversation(provider.Responses, "gpt-5.5", 20_000),
		"reminder after a reply": []byte(`{"model":"m","input":[{"role":"user","content":"fix it"},{"type":"function_call","call_id":"c1","name":"sh","arguments":"{}"},` +
			`{"type":"function_call_output","call_id":"c1","output":"Current working directory: /not/this"},{"role":"developer","content":"<cwd>/work/b</cwd>"},{"role":"user","content":"and now"}]}`),
		"standalone output": []byte(`{"model":"m","input":[{"role":"user","content":"go"},{"type":"function_call_output","output":[{"type":"input_text","text":"done"}]}]}`),
		"sealed reasoning": []byte(`{"model":"m","input":[{"role":"user","content":"go"},{"type":"reasoning","summary":[],"encrypted_content":"x"},` +
			`{"type":"reasoning","summary":[]},{"role":"assistant","content":[{"type":"output_text","text":"<cwd>/said/by/model</cwd>"}]},{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,AA=="}]}]}`),
		"required, no tools": []byte(`{"model":"m","tool_choice":"required","input":[{"role":"user","content":"<cwd>/w</cwd> go"}]}`),
		"bad allowed_tools":  []byte(`{"model":"m","tool_choice":{"type":"allowed_tools","mode":"none","tools":[]},"input":[{"role":"user","content":"go"}]}`),
		"string input":       []byte(`{"model":"m","instructions":"Current working directory: /w/s","input":"hello"}`),
	}
	for _, name := range []string{"workdir/codex-0.162.0.json", "workdir/codex-0.162.1-tool-search.json", "context/codex.json", "codex-26.1007-compaction.json", "upstream_bodies/responses.json"} {
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		bodies[name] = b
	}
	factsAsTheWholeParse(t, provider.Responses, bodies)
}

// factsAsTheWholeParse fails where readFacts of a body, in proto, isn't
// what the whole parse of it gives.
func factsAsTheWholeParse(t *testing.T, proto provider.Protocol, bodies map[string][]byte) {
	t.Helper()
	for name, body := range bodies {
		var want bodyFacts
		if req, err := parse(proto, body); err == nil {
			want.turn, want.within = turnIn(req)
			want.dir, want.git = workPlace(req)
		}
		if got := readFacts(proto, body); got != want {
			t.Errorf("%s: facts %+v, the whole parse's %+v", name, got, want)
		}
	}
}

// Chat and Messages are read for facts as Responses is (#3472): what the
// assistant said, and the tools' outputs, never decide them.
func TestChatAndMessagesFactsAsTheWholeParse(t *testing.T) {
	files := func(names ...string) map[string][]byte {
		bodies := map[string][]byte{}
		for _, name := range names {
			b, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			bodies[name] = b
		}
		return bodies
	}
	chat := files("workdir/pi-1.1.0.json", "upstream_bodies/chat.json", "upstream_bodies/chat-group.json",
		"upstream_bodies/anthropic-to-chat.json", "upstream_bodies/responses-to-chat.json")
	chat["large"] = largeConversation(provider.Chat, "m", 20_000)
	chat["said by the model and a tool"] = []byte(`{"model":"m","messages":[{"role":"user","content":"fix it"},` +
		`{"role":"assistant","content":"<cwd>/said/by/model</cwd>","tool_calls":[{"id":"c1","type":"function","function":{"name":"sh","arguments":"{}"}}]},` +
		`{"role":"tool","tool_call_id":"c1","content":"Current working directory: /not/this"},{"role":"user","content":[{"type":"text","text":"<cwd>/work/b</cwd> now"}]}]}`)
	chat["answered, then a tool"] = []byte(`{"model":"m","messages":[{"role":"system","content":"Current working directory: /w/s"},{"role":"user","content":"go"},` +
		`{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"sh","arguments":"{}"}}]},{"role":"tool","tool_call_id":"c1","content":"ok"}]}`)
	chat["answered, then nothing said"] = []byte(`{"model":"m","messages":[{"role":"user","content":"go"},{"role":"assistant","content":"done"},{"role":"user","content":[]}]}`)
	factsAsTheWholeParse(t, provider.Chat, chat)

	messages := files("workdir/claude-code-2.1.296.json", "context/claude-code.json", "upstream_bodies/anthropic.json",
		"upstream_bodies/anthropic-group.json", "upstream_bodies/anthropic-image.json", "upstream_bodies/anthropic-thinking.json",
		"upstream_bodies/chat-to-anthropic.json", "upstream_bodies/responses-to-anthropic.json")
	messages["large"] = largeConversation(provider.Anthropic, "claude-opus-5-5", 20_000)
	messages["said by the model and a tool"] = []byte(`{"model":"m","max_tokens":1,"messages":[{"role":"user","content":"fix it"},` +
		`{"role":"assistant","content":[{"type":"thinking","thinking":"<cwd>/thought</cwd>","signature":"s"},{"type":"text","text":"<cwd>/said/by/model</cwd>"},{"type":"tool_use","id":"t1","name":"sh","input":{}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"Current working directory: /not/this"}]},{"role":"user","content":[{"type":"text","text":"<cwd>/work/b</cwd>"}]}]}`)
	messages["a tool's result alone"] = []byte(`{"model":"m","max_tokens":1,"system":"Current working directory: /w/s","messages":[{"role":"user","content":"go"},` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"sh","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"ok"}]}]}]}`)
	factsAsTheWholeParse(t, provider.Anthropic, messages)
}

// The facts a long Chat or Messages conversation is read for leave its
// history alone: they allocate a small part of what the whole parse does.
// Each is the least of three runs, as other tests' goroutines allocate
// meanwhile and only ever add to it.
func TestChatAndMessagesFactsLeaveTheHistory(t *testing.T) {
	allocated := func(f func()) uint64 {
		least := uint64(0)
		for i := range 3 {
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			before := ms.TotalAlloc
			f()
			runtime.ReadMemStats(&ms)
			if n := ms.TotalAlloc - before; i == 0 || n < least {
				least = n
			}
		}
		return least
	}
	for _, proto := range []provider.Protocol{provider.Chat, provider.Anthropic} {
		body := largeConversation(proto, "m", 200_000)
		facts := allocated(func() { readFacts(proto, body) })
		parsed := allocated(func() { _, _ = parse(proto, body) })
		t.Logf("%s: %d-byte body, facts allocated %d bytes, the whole parse %d", proto, len(body), facts, parsed)
		// Under -race, sync.Pool drops a quarter of what is put in it, and
		// userSaid's regexps take their backtrackers from one, so a long
		// conversation's facts make ~2.7 MB more of them (#3956).
		if !raceOn && facts*3 > parsed {
			t.Errorf("%s: facts allocated %d bytes, a third or more of the whole parse's %d", proto, facts, parsed)
		}
	}
}
