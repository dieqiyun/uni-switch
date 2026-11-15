package sessions

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// These fixtures contain only invented identities, usage and tool names.
func claudeUsageLine(at time.Time, id, request, model string, tokens Tokens, content string, fast bool) []byte {
	if content == "" {
		content = `[]`
	}
	message := fmt.Sprintf(`"model":%q,"id":%q`, model, id)
	if !fast {
		message = fmt.Sprintf(`"id":%q,"model":%q`, id, model)
	}
	return []byte(fmt.Sprintf(`{"type":"assistant","timestamp":%q,"requestId":%q,"message":{%s,"role":"assistant","content":%s,"usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d}}}`,
		at.Format(time.RFC3339Nano), request, message, content, tokens.Input, tokens.Output, tokens.CacheRead, tokens.CacheWrite))
}

func claudeUsageReplies(s *state) int {
	n := 0
	for _, d := range s.Days.all() {
		n += d.Replies
	}
	return n
}

func TestClaudeUsageNonAdjacentReplay(t *testing.T) {
	for _, fast := range []bool{false, true} {
		t.Run(fmt.Sprint("fast=", fast), func(t *testing.T) {
			s := &state{}
			at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
			a, b := Tokens{10, 2, 30, 4, 0}, Tokens{20, 4, 60, 8, 0}
			claudeLine(s, claudeUsageLine(at, "a", "req-a", "m", a, "", fast), true)
			claudeLine(s, claudeUsageLine(at.Add(time.Second), "b", "req-b", "m", b, "", fast), true)
			claudeLine(s, claudeUsageLine(at.Add(48*time.Hour), "a", "req-a", "m", a, "", fast), true)
			want := a
			want.add(b)
			if s.Models.of("m") != want || claudeUsageReplies(s) != 2 {
				t.Fatalf("tokens=%+v replies=%d, want %+v / 2", s.Models, claudeUsageReplies(s), want)
			}
			if s.Days.of(dateOf(at)).Models.of("m") != want {
				t.Fatal("replay moved the original contribution to its observation date")
			}
		})
	}
}

func TestClaudeUsageRevisionAndTools(t *testing.T) {
	for _, fast := range []bool{false, true} {
		t.Run(fmt.Sprint("fast=", fast), func(t *testing.T) {
			s := &state{}
			at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
			old, updated := Tokens{10, 1, 30, 4, 0}, Tokens{10, 7, 30, 4, 0}
			tool := `[{"type":"tool_use","id":"tool-1","name":"Read","input":{"requestId":"not-the-response"}}]`
			newTool := `[{"type":"tool_use","id":"tool-2","name":"Skill","input":{"skill":"review"}}]`
			line := func(d time.Duration, model string, tok Tokens, tools string) []byte {
				return claudeUsageLine(at.Add(d), "a", "req-a", model, tok, tools, fast)
			}
			claudeLine(s, line(0, "m", old, tool), true)
			claudeLine(s, line(time.Second, "m", updated, newTool), true)
			claudeLine(s, line(2*time.Second, "m", old, tool), true)
			claudeLine(s, line(time.Millisecond, "m", Tokens{10, 99, 30, 4, 0}, tool), true)
			if s.Models.of("m") != updated || claudeUsageReplies(s) != 1 {
				t.Fatalf("old replay undid newer usage: %+v", s)
			}
			d := s.Days.of(dateOf(at))
			if d.Tools.of("Read") != 1 || d.Tools.of("Skill") != 1 || d.Skills.of("review") != 1 {
				t.Fatalf("tools=%v skills=%v", d.Tools, d.Skills)
			}
			// A genuinely newer correction can reduce tokens and change model.
			correction := Tokens{8, 5, 20, 0, 0}
			claudeLine(s, line(48*time.Hour, "m2", correction, ""), true)
			if !s.Models.of("m").zero() || s.Models.of("m2") != correction || d.Replies != 1 {
				t.Fatalf("correction failed: %+v", s)
			}
			if !d.Models.of("m").zero() || s.Days.of(dateOf(at.Add(48*time.Hour))).Models.of("m2") != correction {
				t.Fatal("correction did not replace both date/model buckets")
			}
			claudeLine(s, line(49*time.Hour, "m", updated, newTool), true)
			if s.Models.of("m2") != correction || !s.Models.of("m").zero() {
				t.Fatal("known old snapshot with new observation time replaced correction")
			}
		})
	}
}

func TestClaudeUsageIdentity(t *testing.T) {
	at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	tok := Tokens{3, 4, 5, 6, 0}
	cases := []struct {
		name       string
		identities [][2]string
		calls      int
	}{
		{"missing request upgrade", [][2]string{{"a", ""}, {"a", "r1"}, {"a", ""}}, 1},
		{"request conflict", [][2]string{{"a", "r1"}, {"a", "r2"}, {"a", "r1"}}, 2},
		{"ambiguous missing request", [][2]string{{"a", "r1"}, {"a", "r2"}, {"a", ""}}, 3},
		{"request only", [][2]string{{"", "r1"}, {"", "r1"}}, 1},
		{"no identity", [][2]string{{"", ""}, {"", ""}}, 2},
		{"different message same request", [][2]string{{"a", "r1"}, {"b", "r1"}}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &state{}
			st := &callFile{Agent: "claude"}
			for i, identity := range tc.identities {
				b := claudeUsageLine(at.Add(time.Duration(i)*time.Second), identity[0], identity[1], "m", tok, "", true)
				claudeLine(s, b, true)
				claudeCallLine(st, b)
			}
			want := Tokens{}
			for range tc.calls {
				want.add(tok)
			}
			if s.Models.of("m") != want || claudeUsageReplies(s) != tc.calls || len(st.Calls) != tc.calls {
				t.Fatalf("tokens=%+v replies=%d calls=%d, want %+v / %d", s.Models, claudeUsageReplies(s), len(st.Calls), want, tc.calls)
			}
			if tc.name == "missing request upgrade" && st.Calls[0].RequestID != "r1" {
				t.Fatal("request upgrade was not retained in call")
			}
		})
	}
}

func TestClaudeUsageMissingToolIdentityAndSubagent(t *testing.T) {
	s := &state{}
	at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	tok := Tokens{3, 4, 5, 6, 0}
	content := `[{"type":"tool_use","name":"Read","input":{}}]`
	for i := range 2 {
		claudeLine(s, claudeUsageLine(at.Add(time.Duration(i)*time.Second), "a", "r", "m", tok, content, true), false)
	}
	if s.Models.of("m") != tok || claudeUsageReplies(s) != 0 || s.Days.of(dateOf(at)).Tools.of("Read") != 2 {
		t.Fatalf("subagent/missing tool ID behavior changed: %+v", s)
	}
}

func TestClaudeToolRevisionRejectsReplayedName(t *testing.T) {
	s := &state{}
	at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	for i, name := range []string{"Read", "Write", "Read"} {
		content := fmt.Sprintf(`[{"type":"tool_use","id":"tool-1","name":%q,"input":{}}]`, name)
		claudeLine(s, claudeUsageLine(at.Add(time.Duration(i)*time.Second), "a", "r", "m", Tokens{Input: 3}, content, true), true)
	}
	d := s.Days.of(dateOf(at))
	if d.Tools.of("Read") != 0 || d.Tools.of("Write") != 1 {
		t.Fatalf("old tool snapshot undid newer name: %v", d.Tools)
	}
}

func TestClaudeCallReplayKeepsCompletionWithoutUUID(t *testing.T) {
	st := &callFile{Agent: "claude"}
	at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	tok := Tokens{3, 4, 5, 6, 0}
	content := `[{"type":"text","text":"original block"}]`
	claudeCallLine(st, claudeUsageLine(at, "a", "ra", "m", tok, content, true))
	claudeCallLine(st, claudeUsageLine(at.Add(time.Second), "b", "rb", "m", tok, "", true))
	claudeCallLine(st, claudeUsageLine(at.Add(2*time.Second), "a", "ra", "m", tok, content, true))
	if len(st.Calls) != 2 || !st.Calls[0].Time.Equal(at) {
		t.Fatalf("replay changed call completion time: %+v", st.Calls)
	}
}

func TestClaudeRepeatedBlockRejectsZeroedHistoricalSnapshot(t *testing.T) {
	for _, fast := range []bool{false, true} {
		t.Run(fmt.Sprint("fast=", fast), func(t *testing.T) {
			s, calls := &state{}, &callFile{Agent: "claude"}
			at := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)
			tokens := Tokens{69, 31852, 185103, 998, 0}
			// Two blocks report the same whole-message usage. Historical
			// copies later retain both original timestamps but zero usage.
			for i, usage := range []Tokens{tokens, tokens, {}, {}} {
				b := claudeUsageLine(at.Add(time.Duration(i%2)*time.Second), "a", "r", "m", usage, "", fast)
				claudeLine(s, b, true)
				claudeCallLine(calls, b)
			}
			if s.Models.of("m") != tokens || len(calls.Calls) != 1 || calls.Calls[0].Tokens != tokens {
				t.Fatalf("zeroed historical blocks removed original usage: summary=%v calls=%+v", s.Models, calls.Calls)
			}
		})
	}
}

func TestClaudeUsageResumeEquivalence(t *testing.T) {
	at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	old, updated := Tokens{10, 1, 30, 4, 0}, Tokens{10, 7, 30, 4, 0}
	tool := `[{"type":"tool_use","id":"tool-1","name":"Read","input":{}}]`
	lines := [][]byte{
		claudeUsageLine(at, "a", "", "m", old, tool, true),
		claudeUsageLine(at.Add(time.Second), "b", "rb", "m", old, "", false),
		claudeUsageLine(at.Add(2*time.Second), "a", "ra", "m", updated, tool, true),
		claudeUsageLine(at.Add(48*time.Hour), "a", "ra", "m", old, tool, false),
		claudeUsageLine(at.Add(49*time.Hour), "a", "ra-other", "m", old, tool, true),
	}
	parse := func(split int) (*state, *callFile) {
		s, st := &state{}, &callFile{Agent: "claude"}
		for i, b := range lines {
			if i == split {
				s = s.clone()
				st = st.clone()
			}
			claudeLine(s, b, true)
			claudeCallLine(st, b)
		}
		return s, st
	}
	want, wantCalls := parse(-1)
	for split := range len(lines) {
		got, gotCalls := parse(split)
		if !reflect.DeepEqual(got.Models, want.Models) || !reflect.DeepEqual(got.Days, want.Days) || !reflect.DeepEqual(gotCalls.Calls, wantCalls.Calls) {
			t.Fatalf("split=%d differs from full parse", split)
		}
	}
	// Cloning must not mutate identities, revisions or tool maps of the source.
	s := &state{}
	claudeLine(s, lines[0], true)
	before := s.clone()
	c := s.clone()
	for _, b := range lines[1:] {
		claudeLine(c, b, true)
	}
	if !reflect.DeepEqual(before, s) {
		t.Fatal("continuing the clone modified its source state")
	}
}

func TestClaudeFastRequestIdentityMatchesFullReader(t *testing.T) {
	for _, raw := range []string{`"outer"`, `null`, `123`, `{}`, `true`} {
		var expected string
		for _, fast := range []bool{false, true} {
			s := &state{}
			line := claudeUsageLine(time.Now(), "msg", "placeholder", "m", Tokens{Input: 10}, `[{"type":"tool_use","id":"t","name":"Read","input":{"requestId":"nested"}}]`, fast)
			line = []byte(strings.Replace(string(line), `"requestId":"placeholder"`, `"requestId":`+raw, 1))
			claudeLine(s, line, true)
			for _, branches := range s.Claude.Messages {
				for _, m := range branches {
					if !fast {
						expected = m.Request
					} else if m.Request != expected {
						t.Fatalf("identity %s: got %q want %q", raw, m.Request, expected)
					}
				}
			}
		}
	}
}
