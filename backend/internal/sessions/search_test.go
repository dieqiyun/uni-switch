package sessions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// claudeFile writes a Claude Code session of the given lines and returns it
// as a session to read.
func claudeFile(t *testing.T, lines ...string) Session {
	t.Helper()
	path := filepath.Join(t.TempDir(), "11111111-2222-3333-4444-555555555555.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return Session{Agent: "claude", ID: "11111111-2222-3333-4444-555555555555", Path: path}
}

const (
	claudeAsked = `{"type":"user","message":{"role":"user","content":"Fix the   login\nbug in Auth.go please"},"uuid":"u1"}`
	claudeTool  = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"grep -r rainbow ."}}]},"uuid":"a1"}`
	claudeSaid  = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"The rainbow table lookup in auth.go skips the salt."}]},"uuid":"a2"}`
)

func TestSearchSessionFindsWhatWasSaid(t *testing.T) {
	s := claudeFile(t, claudeAsked, claudeTool, claudeSaid)
	ctx := context.Background()

	// case and the spaces of a line break don't matter
	h, ok, err := SearchSession(ctx, s, NeedleOf("LOGIN  bug"))
	if err != nil || !ok {
		t.Fatalf("not found: %v %v", ok, err)
	}
	if h.Role != "user" || h.Before != "Fix the " || h.Match != "login bug" || h.After != " in Auth.go please" {
		t.Fatalf("hit: %+v", h)
	}

	// a word in a tool's call and in what the model said: the said one is shown
	h, ok, err = SearchSession(ctx, s, NeedleOf("rainbow"))
	if err != nil || !ok || h.Role != "assistant" || h.Kind != "text" || h.Match != "rainbow" {
		t.Fatalf("said hit: %+v %v %v", h, ok, err)
	}

	if _, ok, err := SearchSession(ctx, s, NeedleOf("kubernetes")); ok || err != nil {
		t.Fatalf("found what wasn't said: %v %v", ok, err)
	}
}

func TestSearchSessionCutsALongPart(t *testing.T) {
	long := strings.Repeat("alpha ", 40) + "needle" + strings.Repeat(" omega", 60)
	s := claudeFile(t, `{"type":"user","message":{"role":"user","content":"`+long+`"},"uuid":"u1"}`)
	h, ok, err := SearchSession(context.Background(), s, NeedleOf("needle"))
	if err != nil || !ok {
		t.Fatalf("not found: %v %v", ok, err)
	}
	if !strings.HasPrefix(h.Before, "…") || !strings.HasSuffix(h.After, "…") {
		t.Fatalf("a long part's snippet should show it was cut: %+v", h)
	}
	if n := len([]rune(h.Before)); n > hitBefore+1 {
		t.Fatalf("before is %d runes", n)
	}
	if n := len([]rune(h.After)); n > hitAfter+1 {
		t.Fatalf("after is %d runes", n)
	}
}

func TestSearchSessionWithoutATranscript(t *testing.T) {
	if _, _, err := SearchSession(context.Background(), Session{Agent: "gemini", Path: "x"}, "a"); err != ErrNoTranscript {
		t.Fatalf("err = %v", err)
	}
}

func TestMayHoldSkipsAFileWithoutTheWords(t *testing.T) {
	ctx := context.Background()
	s := claudeFile(t, claudeAsked, claudeSaid)
	if !mayHold(ctx, s.Path, rawNeedle(NeedleOf("AUTH.GO"))) {
		t.Fatal("a file holding the words, in another case, said no")
	}
	if mayHold(ctx, s.Path, rawNeedle(NeedleOf("kubernetes"))) {
		t.Fatal("a file without the words said yes")
	}
	// a needle the file may hold escaped reads it
	for _, q := range []string{`say "hi"`, "café"} {
		if rawNeedle(NeedleOf(q)) != "" {
			t.Fatalf("%q is matched as raw bytes", q)
		}
	}
	// words a line break may part in the file are looked for by the longest
	if got := rawNeedle(NeedleOf("the Login bug")); got != "login" {
		t.Fatalf("raw of words = %q", got)
	}
	if !mayHold(ctx, s.Path, rawNeedle(NeedleOf("login bug"))) {
		t.Fatal("words parted by a line break in the file said no")
	}
	if rawNeedle(NeedleOf("登录")) == "" {
		t.Fatal("Han is written as itself")
	}
	// a match across the edge of two chunks is still seen
	path := filepath.Join(t.TempDir(), "big.jsonl")
	body := make([]byte, 1<<20+10)
	for i := range body {
		body[i] = 'x'
	}
	copy(body[1<<20-3:], "NEEDLE")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if !mayHold(ctx, path, "needle") {
		t.Fatal("a match across two chunks was missed")
	}
}

func TestSearchSessionStopsWhenCancelled(t *testing.T) {
	s := claudeFile(t, claudeAsked, claudeSaid)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok, err := SearchSession(ctx, s, NeedleOf("login")); ok || err == nil {
		t.Fatalf("a cancelled search went on: %v %v", ok, err)
	}
}

func TestGatewayTitleAndSearch(t *testing.T) {
	gatewayHome(t)
	if err := SetGatewayRecording(true, false); err != nil {
		t.Fatal(err)
	}
	if got := GatewayTitle("pi", "ses_1"); got != "" {
		t.Fatalf("a session never recorded has title %q", got)
	}
	at := time.Now().UTC()
	turn := GatewayTurn{Agent: "pi", Session: "ses_1", Time: at,
		Input: []Part{
			{Role: "system", Kind: "context", Text: "You are a helpful assistant. Deploy carefully."},
			{Role: "user", Kind: "text", Text: "<system-reminder>today is Monday</system-reminder>"},
			{Role: "user", Kind: "text", Text: "  Deploy the   staging\nserver tonight  "},
		},
		Output: []Part{{Role: "assistant", Kind: "text", Text: "Deploying staging now."}},
	}
	if err := SaveGatewayTurn(turn); err != nil {
		t.Fatal(err)
	}
	later := GatewayTurn{Agent: "pi", Session: "ses_1", Time: at.Add(time.Second),
		Input: []Part{{Role: "user", Kind: "text", Text: "and what about prod?"}}}
	if err := SaveGatewayTurn(later); err != nil {
		t.Fatal(err)
	}
	if got := GatewayTitle("pi", "ses_1"); got != "Deploy the staging server tonight" {
		t.Fatalf("title = %q", got)
	}
	if got := GatewayTitle("pi", "ses_other"); got != "" {
		t.Fatalf("another session's title = %q", got)
	}

	ctx := context.Background()
	h, ok, err := SearchGateway(ctx, "pi", "ses_1", NeedleOf("staging server"))
	if err != nil || !ok || h.Role != "user" || h.Match != "staging server" {
		t.Fatalf("hit: %+v %v %v", h, ok, err)
	}
	// the system prompt an agent sends every time isn't what was said
	if _, ok, _ := SearchGateway(ctx, "pi", "ses_1", NeedleOf("helpful assistant")); ok {
		t.Fatal("matched the system prompt")
	}
	if _, ok, _ := SearchGateway(ctx, "pi", "ses_1", NeedleOf("prod?")); !ok {
		t.Fatal("a later turn wasn't searched")
	}
}

func TestContainsFold(t *testing.T) {
	for _, c := range []struct {
		b, needle string
		want      bool
	}{
		{"The Redis cache", "redis", true},
		{"THE REDIS CACHE", "redis", true},
		{"rRedis", "redis", true},
		{"rediS", "redis", true},
		{"redi", "redis", false},
		{"Rxdis redix", "redis", false},
		{"登录失败", "登录", true},
		{"登", "登录", false},
		{"", "a", false},
		{"abc", "", true},
	} {
		if got := containsFold([]byte(c.b), []byte(c.needle)); got != c.want {
			t.Errorf("containsFold(%q, %q) = %v", c.b, c.needle, got)
		}
	}
}

// A search decodes only the lines holding the words, in either case, and
// still finds them in a Codex file as in a Claude one.
func TestSearchSessionReadsOnlyLinesWithTheWords(t *testing.T) {
	item := func(payload string) string {
		return `{"timestamp":"2026-09-20T10:00:00.000Z","type":"response_item","payload":` + payload + `}`
	}
	path := filepath.Join(t.TempDir(), "rollout-2026-09-20T10-00-00-0190aaaa-1111-7222-8333-444455556666.jsonl")
	lines := []string{
		item(`{"type":"message","role":"user","content":[{"type":"input_text","text":"List the files"}]}`),
		item(`{"type":"function_call_output","call_id":"c1","output":"` + strings.Repeat("noise ", 2000) + `"}`),
		item(`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"The ZEBRA crossing file is b.go."}]}`),
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Session{Agent: "codex", ID: "0190aaaa-1111-7222-8333-444455556666", Path: path}
	h, ok, err := SearchSession(context.Background(), s, NeedleOf("zebra"))
	if err != nil || !ok || h.Role != "assistant" || h.Match != "ZEBRA" || h.Before != "The " {
		t.Fatalf("codex hit: %+v %v %v", h, ok, err)
	}

	// read whole, every line's part is decoded; with the filter, only the
	// part of the line holding the words
	count := func(keep func([]byte) bool) int {
		n := 0
		if err := readTranscriptOf(s, keep, func(bool, Part) bool { n++; return true }); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(nil); n != 3 {
		t.Fatalf("read whole: %d parts", n)
	}
	if n := count(func(b []byte) bool { return containsFold(b, []byte("zebra")) }); n != 1 {
		t.Fatalf("filtered: %d parts", n)
	}

	// a Claude line whose only match is in capitals is kept
	c := claudeFile(t, claudeTool, `{"type":"user","message":{"role":"user","content":"Use KUBERNETES now"},"uuid":"u2"}`)
	if h, ok, err := SearchSession(context.Background(), c, NeedleOf("kubernetes")); err != nil || !ok || h.Match != "KUBERNETES" {
		t.Fatalf("claude hit: %+v %v %v", h, ok, err)
	}
}
