package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

func clientCacheRequest(t *testing.T, fields, messages string) *Request {
	t.Helper()
	r, err := parseAnthropicRequest([]byte(`{"model":"claude-sonnet-5","max_tokens":100,`+fields+`"messages":`+messages+`}`), true)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestClaudeClientCachePrompt(t *testing.T) {
	for _, ttl := range []string{"", "5m", "1h"} {
		t.Run("system_"+ttl, func(t *testing.T) {
			cc := `{"type":"ephemeral"}`
			want := ttl
			if ttl != "" {
				cc = `{"type":"ephemeral","ttl":"` + ttl + `"}`
			} else {
				want = "5m"
			}
			r := clientCacheRequest(t, `"system":[{"type":"text","text":"shared","cache_control":`+cc+`},{"type":"text","text":"local"}],`, `[{"role":"user","content":"hello"}]`)
			blocks, _ := renderClaudePrompt(r)
			marked := 0
			for _, b := range blocks {
				if cc, ok := b["cache_control"].(map[string]string); ok {
					marked++
					if cc["ttl"] != want || !strings.HasSuffix(b["text"].(string), "shared") || strings.Contains(b["text"].(string), "local") {
						t.Fatalf("system boundary/TTL: %v", b)
					}
				}
			}
			if marked != 1 {
				t.Fatalf("system marks = %d, want 1", marked)
			}
		})
	}
	for _, block := range []struct{ name, raw, end string }{
		{"text", `{"type":"text","text":"pinned","cache_control":{"type":"ephemeral","ttl":"1h"}}`, "pinned"},
		{"image", `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="},"cache_control":{"type":"ephemeral","ttl":"1h"}}`, ""},
		{"tool_result", `{"type":"tool_result","tool_use_id":"call_1","content":"tool output","cache_control":{"type":"ephemeral","ttl":"1h"}}`, "tool output"},
	} {
		t.Run("message_"+block.name, func(t *testing.T) {
			r := clientCacheRequest(t, "", `[{"role":"user","content":[`+block.raw+`,{"type":"text","text":"after"}]},{"role":"assistant","content":"ok"},{"role":"user","content":"next"}]`)
			blocks, _ := renderClaudePrompt(r)
			marked := 0
			var words strings.Builder
			for _, b := range blocks {
				text, _ := b["text"].(string)
				words.WriteString(text)
				if cc, ok := b["cache_control"].(map[string]string); ok {
					marked++
					if cc["ttl"] != "1h" {
						t.Fatalf("message TTL: %v", b)
					}
					if block.end != "" && !strings.HasSuffix(text, block.end) {
						t.Fatalf("message mark moved: %v", b)
					}
					if block.name == "image" && b["type"] != "image" {
						t.Fatalf("image mark moved: %v", b)
					}
				}
			}
			if marked != 1 {
				t.Fatalf("message marks = %d, want 1", marked)
			}
			if block.name == "tool_result" && !strings.Contains(words.String(), "[end of tool result id=call_1]\n\n[user message]\nafter") {
				t.Fatal("mark lost tool-result boundary")
			}
			rec := httptest.NewRecorder()
			cacheDropped(rec, r)
			if got := rec.Header().Get("X-Uni-Switch-Cache-Marks-Dropped"); got != "" {
				t.Fatalf("fresh mark dropped: %s", got)
			}
		})
	}
	for _, top := range []bool{false, true} {
		t.Run(fmt.Sprintf("tail_top_%t", top), func(t *testing.T) {
			fields, mark := "", `,"cache_control":{"type":"ephemeral"}`
			if top {
				fields = `"cache_control":{"type":"ephemeral","ttl":"1h"},`
				mark = ""
			}
			r := clientCacheRequest(t, fields, `[{"role":"user","content":[{"type":"text","text":"tail"`+mark+`}]}]`)
			blocks, _ := renderClaudePrompt(r)
			want := "5m"
			if top {
				want = "1h"
			}
			var body struct {
				CacheControl map[string]string `json:"cache_control"`
			}
			_ = json.Unmarshal([]byte(claudeExtraBody(nil, r.cacheTail())), &body)
			if body.CacheControl["ttl"] != want {
				t.Fatalf("request tail: %+v", body)
			}
			if n := promptCachePrefix(blocks).count; n != 0 {
				t.Fatalf("tail duplicated on first message: %d marks", n)
			}
		})
	}
}

func TestClaudeClientCacheHistorySlot(t *testing.T) {
	var msgs []string
	for i := 0; i < 18; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs = append(msgs, fmt.Sprintf(`{"role":%q,"content":"message %d"}`, role, i))
	}
	msgs = append(msgs, `{"role":"user","content":"next"}`)
	for _, count := range []int{1, 4} {
		var sys []string
		for i := 0; i < count; i++ {
			sys = append(sys, fmt.Sprintf(`{"type":"text","text":"instruction %d","cache_control":{"type":"ephemeral"}}`, i))
		}
		r := clientCacheRequest(t, `"system":[`+strings.Join(sys, ",")+`],`, `[`+strings.Join(msgs, ",")+`]`)
		blocks, _ := renderClaudePrompt(r)
		marks, history := 0, 0
		for _, b := range blocks {
			if cc, ok := b["cache_control"].(map[string]string); ok {
				marks++
				if cc["ttl"] != "5m" {
					t.Fatalf("history follows 5m with %v", cc)
				}
				if strings.Contains(b["text"].(string), "message 15") {
					history++
				}
			}
		}
		want, wantHistory := count, 0
		if count < 4 {
			want++
			wantHistory = 1
		}
		if marks != want || history != wantHistory {
			t.Fatalf("%d client marks: %d marks, %d history; want %d, %d", count, marks, history, want, wantHistory)
		}
	}
}

func TestClaudeClientCacheValidation(t *testing.T) {
	mark := `"cache_control":{"type":"ephemeral"}`
	long := `"cache_control":{"type":"ephemeral","ttl":"1h"}`
	for _, tc := range []struct{ name, fields, messages, contains string }{
		{"five", `"system":[{"type":"text","text":"1",` + mark + `},{"type":"text","text":"2",` + mark + `},{"type":"text","text":"3",` + mark + `},{"type":"text","text":"4",` + mark + `}],`, `[{"role":"user","content":[{"type":"text","text":"5",` + mark + `}]}]`, "maximum of 4"},
		{"six_including_top", mark + `,"system":[{"type":"text","text":"1",` + mark + `},{"type":"text","text":"2",` + mark + `},{"type":"text","text":"3",` + mark + `},{"type":"text","text":"4",` + mark + `}],`, `[{"role":"user","content":[{"type":"text","text":"5",` + mark + `}]}]`, "A maximum of 4 blocks with cache_control may be provided. Found 6."},
		{"tool_system_order", `"tools":[{"name":"read","input_schema":{"type":"object"},` + mark + `}],"system":[{"type":"text","text":"system",` + long + `}],`, `[{"role":"user","content":"hi"}]`, "1h"},
		{"system_message_order", `"system":[{"type":"text","text":"system",` + mark + `}],`, `[{"role":"user","content":[{"type":"text","text":"hi",` + long + `}]}]`, "1h"},
		{"message_top_order", long + `,`, `[{"role":"user","content":[{"type":"text","text":"pin",` + mark + `},{"type":"text","text":"tail"}]}]`, "1h"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"model":"claude-sonnet-5","max_tokens":10,` + tc.fields + `"messages":` + tc.messages + `}`
			s := New()
			t.Cleanup(s.subscription.abortAll)
			r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
			r.Header.Set("X-Uni-Switch-Cache-Marks", "client")
			w := httptest.NewRecorder()
			code, _ := s.serveSubscription(w, r, provider.Anthropic, "Claude Code", "claude-sonnet-5", "test", []byte(body), &Usage{},
				func(context.Context, *Request) (*subscriptionRun, <-chan Event, error) {
					return nil, nil, fmt.Errorf("invalid request reached process startup")
				})
			var got struct {
				Type  string
				Error struct {
					Type    string
					Message string
				}
			}
			_ = json.Unmarshal(w.Body.Bytes(), &got)
			if code != 400 || w.Code != 400 || got.Type != "error" || got.Error.Type != "invalid_request_error" || !strings.Contains(got.Error.Message, tc.contains) {
				t.Fatalf("validation: %d %s", code, w.Body)
			}
		})
	}
}

func cacheAsk(t *testing.T, h *resumeHarness, fields, messages, mode string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"model":"claude-sonnet-5","max_tokens":100,` + fields + `"messages":` + messages + `}`
	r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	if h.from != "" {
		r.RemoteAddr = h.from
	}
	if mode != "" {
		r.Header.Set("X-Uni-Switch-Cache-Marks", mode)
	}
	w := httptest.NewRecorder()
	if code, msg := h.s.serveClaudeSubscription(w, r, provider.Anthropic, h.p, "claude-sonnet-5", []byte(body), &Usage{}); code != 200 {
		t.Fatalf("%d %s", code, msg)
	}
	return w
}

func TestClaudeClientCacheRunningTailAndDrops(t *testing.T) {
	h := newScriptHarness(t, "ok", `echo "cache-disabled ${DISABLE_PROMPT_CACHING-unset}" >> $log`)
	system := `"system":[{"type":"text","text":"shared","cache_control":{"type":"ephemeral","ttl":"1h"}}],`
	tool := `"tools":[{"name":"read","input_schema":{"type":"object"},"cache_control":{"type":"ephemeral","ttl":"1h"}}],`
	tail := func(ttl string) string { return `"cache_control":{"type":"ephemeral","ttl":"` + ttl + `"},` }
	first := `{"role":"user","content":"hello"}`
	w := cacheAsk(t, h, system+tool+tail("5m"), `[`+first+`]`, "client")
	if got := w.Header().Get("X-Uni-Switch-Cache-Marks-Dropped"); got != "tools:1" {
		t.Fatalf("tool drop: %q", got)
	}
	marked := `{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral"}}]}`
	rest := `,{"role":"assistant","content":"ok"},{"role":"user","content":"next"}`
	w = cacheAsk(t, h, system+tool+tail("5m"), `[`+marked+rest+`]`, "client")
	if got := w.Header().Get("X-Uni-Switch-Cache-Marks-Dropped"); got != "tools:1,messages:1" {
		t.Fatalf("running drops: %q", got)
	}
	third := first + rest + `,{"role":"assistant","content":"ok"},{"role":"user","content":"again"}`
	// The last-block form must update the same request-level tail.
	thirdMarked := strings.Replace(third, `"content":"again"`, `"content":[{"type":"text","text":"again","cache_control":{"type":"ephemeral","ttl":"1h"}}]`, 1)
	cacheAsk(t, h, system+tool, `[`+thirdMarked+`]`, "client")
	fourth := third + `,{"role":"assistant","content":"ok"},{"role":"user","content":"without tail"}`
	cacheAsk(t, h, system+tool, `[`+fourth+`]`, "client")
	logs := h.runs()
	if len(logs) != 1 {
		t.Fatalf("TTL changes restarted run: %d", len(logs))
	}
	for _, log := range logs {
		if !strings.Contains(log, "cache-disabled 1") {
			t.Fatal("run did not disable its own caching")
		}
		var tails []string
		for _, line := range strings.Split(log, "\n") {
			if !strings.HasPrefix(line, "control ") {
				continue
			}
			var v struct {
				Request struct {
					Settings struct{ Env map[string]string }
				}
			}
			_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "control ")), &v)
			if raw, ok := v.Request.Settings.Env["CLAUDE_CODE_EXTRA_BODY"]; ok {
				var body struct {
					CacheControl map[string]string `json:"cache_control"`
				}
				_ = json.Unmarshal([]byte(raw), &body)
				tails = append(tails, body.CacheControl["ttl"])
			}
		}
		if fmt.Sprint(tails) != "[5m 1h ]" {
			t.Fatalf("tail updates = %#v, want 5m, 1h, cleared", tails)
		}
	}
	// A later ordinary request may follow identical words, but cannot use
	// the process whose automatic caching was disabled.
	fifth := fourth + `,{"role":"assistant","content":"ok"},{"role":"user","content":"ordinary"}`
	w = cacheAsk(t, h, system+tool, `[`+fifth+`]`, "")
	if len(h.runs()) != 2 {
		t.Fatal("opted-out turn reused client-cache run")
	}
	if w.Header().Get("X-Uni-Switch-Cache-Marks-Dropped") != "" {
		t.Fatal("ordinary reply reports cache drops")
	}
}

func TestClaudeClientCacheAbsentUnchanged(t *testing.T) {
	h := newScriptHarness(t, "ok", `echo "cache-disabled ${DISABLE_PROMPT_CACHING-unset}" >> $log`)
	t.Setenv("DISABLE_PROMPT_CACHING", "0")
	h.from = "127.0.0.1:51000" // a caller elsewhere is told where its tools run
	fields := `"system":[{"type":"text","text":"shared","cache_control":{"type":"ephemeral","ttl":"5m"}}],"cache_control":{"type":"ephemeral","ttl":"1h"},`
	w := cacheAsk(t, h, fields, `[{"role":"user","content":"hello"}]`, "")
	if w.Header().Get("X-Uni-Switch-Cache-Marks-Dropped") != "" {
		t.Fatal("ordinary header changed")
	}
	for _, log := range h.runs() {
		if !strings.Contains(log, "cache-disabled 0") || strings.Contains(log, "apply_flag_settings") {
			t.Fatalf("ordinary environment/settings changed: %s", log)
		}
		if !strings.Contains(log, "\"cache_control\":{\"ttl\":\"1h\",\"type\":\"ephemeral\"},\"text\":\"\\u003cexternal_system_instructions\\u003e\nshared\n\\u003c/external_system_instructions\\u003e\n\n\"") {
			t.Fatalf("ordinary prompt changed: %s", log)
		}
	}
}

func TestClaudeClientCacheIdentity(t *testing.T) {
	plain, _ := parseAnthropic([]byte(`{"model":"m","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"ok"}]}`))
	cached := *plain
	cached.clientCache = &clientCache{}
	if turnKey("owner", plain, plain.Messages) == turnKey("owner", &cached, cached.Messages) {
		t.Fatal("exact identity shares cache modes")
	}
	if newLooseTurn("owner", plain, plain.Messages, true).key == newLooseTurn("owner", &cached, cached.Messages, true).key {
		t.Fatal("rewritten identity shares cache modes")
	}
	// The identity step must leave ordinary key bytes untouched, whatever
	// the rest of the conversation hash covers.
	h := sha256.New()
	h.Write([]byte(turnKey("owner", plain, plain.Messages)))
	withoutCacheIdentity := fmt.Sprintf("%x", h.Sum(nil))
	cacheIdentity(h, plain)
	if got := fmt.Sprintf("%x", h.Sum(nil)); got != withoutCacheIdentity {
		t.Fatalf("ordinary cache identity wrote key bytes: %s, want %s", got, withoutCacheIdentity)
	}
}

func TestClaudeClientCachePrefixRestart(t *testing.T) {
	for _, shelved := range []bool{false, true} {
		t.Run(fmt.Sprintf("shelved_%t", shelved), func(t *testing.T) {
			h := newResumeHarness(t)
			fields := `"tools":[{"name":"read","input_schema":{"type":"object"}}],`
			first := `[{"role":"user","content":[{"type":"text","text":"pinned","cache_control":{"type":"ephemeral"}},{"type":"text","text":" end"}]}]`
			cacheAsk(t, h, fields, first, "client")
			if shelved {
				b := h.s.subscription
				b.mu.Lock()
				var run *subscriptionRun
				for _, r := range b.idle {
					run = r
					b.shelve(r)
					delete(b.idle, r.idleKey)
					r.idleKey = ""
					break
				}
				b.mu.Unlock()
				if run == nil {
					t.Fatal("no idle run to shelve")
				}
				run.abort()
			}
			// The request's old pin is gone. A retained 5m mark cannot precede
			// its new 1h tail, so render the history in a new session.
			second := `[{"role":"user","content":[{"type":"text","text":"pinned"},{"type":"text","text":" end"}]},{"role":"assistant","content":"ok"},{"role":"user","content":"next"}]`
			cacheAsk(t, h, fields+`"cache_control":{"type":"ephemeral","ttl":"1h"},`, second, "client")
			if len(h.runs()) != 2 {
				t.Fatal("retained 5m prefix accepted a 1h tail")
			}
			for _, log := range h.runs() {
				if strings.Contains(log, "--resume ") {
					t.Fatal("incompatible saved prefix was resumed")
				}
			}
		})
	}
}

func TestClaudeClientCacheUsage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for Claude Code")
	}
	for _, tc := range []struct {
		name        string
		write, hour int
		split       string
	}{
		{"mixed", 40, 29, `,"cache_creation":{"ephemeral_5m_input_tokens":11,"ephemeral_1h_input_tokens":29}`},
		// The pure-5m counts come from the live omitted-TTL probe.
		{"pure5m", 1894, 0, `,"cache_creation":{"ephemeral_5m_input_tokens":1894,"ephemeral_1h_input_tokens":0}`},
		{"reportedZero", 0, 0, `,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0}`},
		{"unreported", 40, 0, ""},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream_%t", tc.name, stream), func(t *testing.T) {
				dir := t.TempDir()
				u := fmt.Sprintf(`{"input_tokens":12,"cache_read_input_tokens":300,"cache_creation_input_tokens":%d%s,"output_tokens":1}`, tc.write, tc.split)
				testenv.Program(t, filepath.Join(dir, "claude"), `#!/bin/sh
while read -r line; do
 case $line in *'"type":"control_request"'*)
  id=$(printf '%s' "$line" | sed 's/.*"request_id":"\([^"]*\)".*/\1/')
  echo '{"type":"control_response","response":{"subtype":"success","request_id":"'$id'"}}'
  continue ;;
 esac
 echo '{"type":"stream_event","event":{"type":"message_start","message":{"id":"m","model":"claude-sonnet-5","usage":`+u+`}}}'
 echo '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}}'
 echo '{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}}'
 echo '{"type":"stream_event","event":{"type":"message_stop"}}'
 echo '{"type":"result","subtype":"success","is_error":false,"result":""}'
done
`)
				t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
				runsEndWithTest(t, dir)
				s := New()
				t.Cleanup(s.subscription.abortAll)
				p := provider.Provider{ID: "claude", Account: &provider.Account{Agent: "claude", User: "u"}}
				body := fmt.Sprintf(`{"model":"claude-sonnet-5","max_tokens":100,"stream":%t,"cache_control":{"type":"ephemeral"},"messages":[{"role":"user","content":"hi"}]}`, stream)
				r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
				r.Header.Set("X-Uni-Switch-Cache-Marks", "client")
				w := httptest.NewRecorder()
				var usage Usage
				if code, msg := s.serveClaudeSubscription(w, r, provider.Anthropic, p, "claude-sonnet-5", []byte(body), &usage); code != 200 {
					t.Fatalf("%d %s", code, msg)
				}
				if usage.CacheWrite != tc.write || usage.CacheWrite1h != tc.hour || usage.CacheRead != 300 {
					t.Fatalf("cache usage changed: %+v", usage)
				}
				if tc.split == "" {
					if strings.Contains(w.Body.String(), `"cache_creation":`) {
						t.Fatalf("invented TTL split: %s", w.Body)
					}
				} else {
					want := strings.TrimPrefix(tc.split, ",")
					if !strings.Contains(w.Body.String(), want) {
						t.Fatalf("missing %s from %s", want, w.Body)
					}
					if stream && strings.Count(w.Body.String(), want) != 2 {
						t.Fatalf("TTL split must survive to message_delta: %s", w.Body)
					}
				}
			})
		}
	}
}

func TestClaudeClientCacheStreamingHeaders(t *testing.T) {
	for _, mode := range []string{"selected", "timeout", "ordinary"} {
		t.Run(mode, func(t *testing.T) {
			w := httptest.NewRecorder()
			hw := newHoldWriter(w, true)
			hw.alive = &keptAlive{proto: provider.Anthropic, at: time.Now().Add(-time.Hour)}
			if mode != "ordinary" {
				hw.alive.cacheReportUntil = time.Now().Add(time.Minute)
			}
			r := clientCacheRequest(t, `"tools":[{"name":"read","input_schema":{"type":"object"},"cache_control":{"type":"ephemeral"}}],`, `[{"role":"user","content":"hi"}]`)
			if mode != "ordinary" {
				cacheDropped(hw, r)
				hw.keepAlive()
				if w.Flushed {
					t.Fatal("headers sent before run selection")
				}
				if mode == "selected" {
					cacheReportReady(hw, r)
				} else {
					hw.alive.cacheReportUntil = time.Now().Add(-time.Second)
				}
			}
			hw.keepAlive()
			if !w.Flushed || w.Body.String() != ": keepalive\n" {
				t.Fatalf("keepalive blocked: %q", w.Body.String())
			}
			want := ""
			if mode == "selected" {
				want = "tools:1"
			}
			if got := w.Result().Header.Get("X-Uni-Switch-Cache-Marks-Dropped"); got != want {
				t.Fatalf("header %q, want %q", got, want)
			}
		})
	}
}

func TestClaudeClientCacheServerValidationBeforeStreaming(t *testing.T) {
	claudeMadeFirst(t, false)
	s := New()
	t.Cleanup(s.subscription.abortAll)
	body := `{"model":"claude/claude-sonnet-5","max_tokens":10,"stream":true,"system":[{"type":"text","text":"short","cache_control":{"type":"ephemeral"}}],"cache_control":{"type":"ephemeral","ttl":"1h"},"messages":[{"role":"user","content":"hi"}]}`
	r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	r.Header.Set("X-Uni-Switch-Cache-Marks", "client")
	r.Header.Set("x-api-key", Token)
	r.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	localGuard(s.Handler()).ServeHTTP(w, r)
	if w.Code != 400 || w.Flushed || !strings.Contains(w.Body.String(), `"invalid_request_error"`) {
		t.Fatalf("validation after streaming: %d %s", w.Code, w.Body)
	}
}

func TestClaudeClientCacheTryKeepalive(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(fmt.Sprintf("fallback_%t", fallback), func(t *testing.T) {
			w := httptest.NewRecorder()
			alive := &keptAlive{proto: provider.Anthropic}
			if fallback {
				prior := newHoldWriter(w, true)
				prior.alive = alive
				req := clientCacheRequest(t, `"tools":[{"name":"read","input_schema":{"type":"object"},"cache_control":{"type":"ephemeral"}}],`, `[{"role":"user","content":"hi"}]`)
				cacheReportTry(prior, req)
				prior.keepAlive()
				if w.Flushed {
					t.Fatal("Claude selection did not hold keepalives")
				}
				// A failed try can leave a note on the shared writer before
				// headers go out. The next candidate must remove it too.
				w.Header().Set("X-Uni-Switch-Cache-Marks-Dropped", "tools:1")
			}
			hw := newHoldWriter(w, true)
			hw.alive = alive
			cacheReportTry(hw, nil)
			hw.keepAlive()
			if !w.Flushed || w.Body.String() != ": keepalive\n" {
				t.Fatalf("API-key keepalive held for Claude selection: %q", w.Body.String())
			}
			if got := w.Result().Header.Get("X-Uni-Switch-Cache-Marks-Dropped"); got != "" {
				t.Fatalf("API-key keepalive leaked a Claude report: %q", got)
			}
		})
	}
}

func TestClaudeClientCacheOtherAPIsIgnoreHeader(t *testing.T) {
	for _, tc := range []struct {
		proto      provider.Protocol
		path, body string
	}{
		{provider.Chat, "/v1/chat/completions", `{"model":"m","messages":[{"role":"user","content":"hello"}]}`},
		{provider.Responses, "/v1/responses", `{"model":"m","input":"hello"}`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			h := newScriptHarness(t, "ok", `echo "cache-disabled ${DISABLE_PROMPT_CACHING-unset}" >> $log`)
			t.Setenv("DISABLE_PROMPT_CACHING", "0")
			r := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
			r.Header.Set("X-Uni-Switch-Cache-Marks", "client")
			w := httptest.NewRecorder()
			if code, msg := h.s.serveClaudeSubscription(w, r, tc.proto, h.p, "claude-sonnet-5", []byte(tc.body), &Usage{}); code != 200 {
				t.Fatalf("%d %s", code, msg)
			}
			for _, log := range h.runs() {
				if !strings.Contains(log, "cache-disabled 0") || strings.Contains(log, "apply_flag_settings") {
					t.Fatalf("non-Messages API enabled cache mode: %s", log)
				}
			}
		})
	}
}
