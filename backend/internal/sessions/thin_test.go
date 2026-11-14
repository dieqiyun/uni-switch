package sessions

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

// a Claude Code tool result laid out as Claude Code writes it: the image
// in the message and again in toolUseResult, the timestamp between them,
// the folder and session id at the end
func toolResultLine(size int) string {
	img := strings.Repeat("iVBORw0KGgoAAAANSUhEUgAA", size/24+1)[:size]
	text := strings.Repeat(`中文 \"quoted\" \\ é 😀 é`, size/60+1)
	return `{"parentUuid":"p-1","isSidechain":false,"message":{"role":"user","content":[{"tool_use_id":"toolu_1","type":"tool_result","content":[{"type":"text","text":"` + text + `"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + img + `"}}]}]},"type":"user","uuid":"u-1","timestamp":"2026-10-20T01:02:03.456Z","toolUseResult":[{"type":"image","source":{"data":"` + img + `"}}],"userType":"external","cwd":"/Users/someone/proj","sessionId":"sess-1","version":"2.0.1"}`
}

func TestThinnerKeepsJSON(t *testing.T) {
	line := toolResultLine(64 << 10)
	var want any
	if err := json.Unmarshal([]byte(line), &want); err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(1))
	for round := 0; round < 200; round++ {
		var th thinner
		var out []byte
		for rest := []byte(line); len(rest) > 0; {
			n := min(len(rest), 1+rng.Intn(9000))
			if round%2 == 0 {
				n = min(len(rest), 1+rng.Intn(7))
			}
			out = th.add(out, rest[:n])
			rest = rest[n:]
		}
		if !utf8.Valid(out) {
			t.Fatalf("round %d: a rune was cut", round)
		}
		var got any
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("round %d: thinned line is not JSON: %v", round, err)
		}
		prefixes(t, want, got)
		if len(out) > 40<<10 {
			t.Fatalf("round %d: thinned to %d bytes", round, len(out))
		}
	}
}

// prefixes checks that got is want with each string cut to a prefix of
// itself, at most thinKeep bytes of the line long
func prefixes(t *testing.T, want, got any) {
	t.Helper()
	switch w := want.(type) {
	case map[string]any:
		g := got.(map[string]any)
		if len(g) != len(w) {
			t.Fatalf("keys: %d, want %d", len(g), len(w))
		}
		for k, v := range w {
			prefixes(t, v, g[k])
		}
	case []any:
		g := got.([]any)
		if len(g) != len(w) {
			t.Fatalf("items: %d, want %d", len(g), len(w))
		}
		for i := range w {
			prefixes(t, w[i], g[i])
		}
	case string:
		g := got.(string)
		// a surrogate pair cut in two decodes as U+FFFD
		g = strings.TrimSuffix(g, "�")
		if !strings.HasPrefix(w, g) || len(w) > thinKeep && len(g) < thinKeep/4 {
			t.Fatalf("string %.40q… is not cut from %.40q…", g, w)
		}
	default:
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("%v, want %v", got, want)
		}
	}
}

// A tool result of megabytes reads the same thinned as whole, for a
// session's summary and for its calls, and is never gathered whole.
func TestThinToolResultReadsTheSame(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	big := toolResultLine(3 << 20)
	data := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"hi"},"uuid":"u-0","timestamp":"2026-10-20T01:00:00.000Z","cwd":"/Users/someone/proj","sessionId":"sess-1"}`,
		big,
		`{"type":"assistant","message":{"model":"claude-x","id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":3,"output_tokens":4}},"requestId":"req_1","uuid":"u-2","timestamp":"2026-10-20T01:05:00.000Z","cwd":"/Users/someone/proj","sessionId":"sess-1"}`,
		big,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if !ccResultHead([]byte(big[:1<<20])) {
		t.Fatal("a tool result's head is not known as one")
	}

	for _, main := range []bool{true, false} {
		read := func(thin func([]byte) (thinner, bool)) (*state, int64) {
			s := &state{}
			off, err := scanThin(path, 0, nil, thin, func(b []byte, _, _ int64) bool {
				claudeLine(s, b, main)
				return true
			})
			if err != nil {
				t.Fatal(err)
			}
			return s, off
		}
		whole, wOff := read(nil)
		thin, tOff := read(thinClaude)
		if wOff != tOff || wOff != int64(len(data)) {
			t.Fatalf("read to %d thinned, %d whole, of %d", tOff, wOff, len(data))
		}
		if !reflect.DeepEqual(whole, thin) {
			t.Fatalf("main=%v: thinned\n%+v\nwhole\n%+v", main, thin, whole)
		}
		if thin.ID != "sess-1" || thin.Last.IsZero() {
			t.Fatalf("main=%v: read %+v", main, thin)
		}
	}

	calls := func(thin func([]byte) (thinner, bool)) *callFile {
		st := &callFile{Path: path, Agent: "claude"}
		if _, err := scanThin(path, 0, nil, thin, func(b []byte, start, end int64) bool {
			st.At, st.End = start, end
			claudeCallLine(st, b)
			return true
		}); err != nil {
			t.Fatal(err)
		}
		return st
	}
	whole, thin := calls(nil), calls(thinClaude)
	if !reflect.DeepEqual(whole, thin) {
		t.Fatalf("calls thinned\n%+v\nwhole\n%+v", thin, whole)
	}
	if len(thin.Calls) != 1 || thin.LastUser.IsZero() {
		t.Fatalf("calls %+v", thin)
	}

	// allocated: a few times the reader's buffer, not the 13MB of the lines
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	calls(thinClaude)
	runtime.ReadMemStats(&after)
	if got := after.TotalAlloc - before.TotalAlloc; got > 3<<20 {
		t.Fatalf("a thinned read allocated %d bytes", got)
	}
}

// A Codex prompt with screenshots reads the same with its images cut: its
// text, however long, is kept whole.
func TestThinCodexKeepsPromptText(t *testing.T) {
	prompt := strings.Repeat("重要执行方式：", 3000)
	img := "data:image/png;base64," + strings.Repeat("iVBORw0KGgo", 300<<10)
	big := `{"timestamp":"2026-10-21T09:24:46.501Z","type":"response_item","payload":{"type":"message","id":"msg_1","role":"user","content":[{"type":"input_text","text":"` + prompt + `"},{"type":"input_image","image_url":"` + img + `"},{"type":"input_text","text":"` + prompt + `"}]}}`
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	data := `{"timestamp":"2026-10-21T09:24:40.000Z","type":"session_meta","payload":{"id":"thread-1","cwd":"/work/x"}}` + "\n" + big + "\n" +
		`{"timestamp":"2026-10-21T09:25:00.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	var th thinner
	th.urls = true
	out := th.add(nil, []byte(big))
	var m struct {
		Payload struct {
			Content []struct {
				Text  string `json:"text"`
				Image string `json:"image_url"`
			} `json:"content"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if c := m.Payload.Content; len(c) != 3 || c[0].Text != prompt || c[2].Text != prompt || len(c[1].Image) > thinKeep || !strings.HasPrefix(c[1].Image, "data:image/png;base64,") {
		t.Fatalf("thinned to %d bytes: %.80q", len(out), out)
	}

	for _, main := range []bool{true, false} {
		read := func(thin func([]byte) (thinner, bool)) *state {
			s := &state{}
			if _, err := scanThin(path, 0, func(b []byte) bool { return codexHead(s, b, main) }, thin, func(b []byte, _, _ int64) bool {
				codexBody(s, b, main)
				return true
			}); err != nil {
				t.Fatal(err)
			}
			return s
		}
		whole, thin := read(nil), read(thinCodex)
		if !reflect.DeepEqual(whole, thin) {
			t.Fatalf("main=%v: thinned\n%+v\nwhole\n%+v", main, thin, whole)
		}
		if thin.Title == "" {
			t.Fatalf("main=%v: no title: %+v", main, thin)
		}
	}
}
