package sessions

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// The messages each way, the tools called and the skills called up are
// counted from Claude Code's and Codex's files, read fast and read whole
// alike, and the overview sums them up: the shape of the sessions, the
// tools by category and week, and the skills.
func TestStatsToolsAndSkills(t *testing.T) {
	inZone(t, 0)
	claude, codex := setup(t)
	id := "44444444-2222-3333-4444-555555555555"
	where := `,"cwd":"/work/tools","sessionId":"` + id + `"}` + "\n"
	user := func(at, content string) string {
		return `{"parentUuid":null,"isSidechain":false,"type":"user","message":{"role":"user","content":` + content + `},"timestamp":"` + at + `"` + where
	}
	reply := func(msg, at, content string) string {
		return `{"parentUuid":null,"isSidechain":false,"message":{"model":"claude-opus-5-5","id":"` + msg + `","type":"message","role":"assistant","content":` + content +
			`,"usage":{"input_tokens":10,"output_tokens":5}},"type":"assistant","timestamp":"` + at + `"` + where
	}
	lines := user("2026-09-27T10:00:00.000Z", `"fix the build"`) +
		reply("m1", "2026-09-27T10:00:05.000Z", `[{"type":"text","text":"on it"}]`) +
		// the same message's next blocks: two tool calls, one reply
		reply("m1", "2026-09-27T10:00:06.000Z", `[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"go build"}}]`) +
		reply("m1", "2026-09-27T10:00:07.000Z", `[{"type":"tool_use","id":"toolu_2","name":"Skill","input":{"skill":"artifact-design"},"caller":{"type":"direct"}}]`) +
		user("2026-09-27T10:00:08.000Z", `[{"tool_use_id":"toolu_1","type":"tool_result","content":"ok"}]`) +
		// laid out otherwise: read whole
		`{"type":"assistant","message":{"role":"assistant","id":"m2","model":"claude-opus-5-5","content":[{"type":"tool_use","name":"mcp__slack__send","id":"toolu_3","input":{}},{"type":"tool_use","name":"Edit","id":"toolu_4","input":{}}],"usage":{"input_tokens":1,"output_tokens":1}},"timestamp":"2026-09-27T10:00:09.000Z"` + where +
		user("2026-09-27T10:01:00.000Z", `[{"type":"text","text":"<command-name>/clear</command-name>"}]`) +
		user("2026-09-27T10:02:00.000Z", `"[Request interrupted by user]"`)
	path := filepath.Join(claude, "projects", "-work-tools", id+".jsonl")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(lines), 0o644)
	// a subagent's tool calls count, its messages not
	sub := filepath.Join(claude, "projects", "-work-tools", id, "subagents", "agent-a1.jsonl")
	os.MkdirAll(filepath.Dir(sub), 0o755)
	os.WriteFile(sub, []byte(user("2026-09-27T10:00:10.000Z", `"look around"`)+
		reply("s1", "2026-09-27T10:00:11.000Z", `[{"type":"tool_use","id":"toolu_5","name":"Grep","input":{}}]`)), 0o644)

	cx := filepath.Join(codex, "sessions", "2026", "09", "27", "rollout-2026-09-27T10-00-00-55555555-2222-3333-4444-555555555555.jsonl")
	os.MkdirAll(filepath.Dir(cx), 0o755)
	os.WriteFile(cx, []byte(
		`{"timestamp":"2026-09-27T10:00:00.000Z","type":"session_meta","payload":{"id":"55555555-2222-3333-4444-555555555555","cwd":"/work/cx"}}`+"\n"+
			`{"timestamp":"2026-09-27T10:00:01.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>x</environment_context>"}]}}`+"\n"+
			`{"timestamp":"2026-09-27T10:00:02.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"add a test"}]}}`+"\n"+
			`{"timestamp":"2026-09-27T10:00:03.000Z","type":"response_item","payload":{"type":"function_call","id":"a","name":"exec_command","arguments":"{}","call_id":"c1"}}`+"\n"+
			`{"timestamp":"2026-09-27T10:00:04.000Z","type":"response_item","payload":{"type":"custom_tool_call","status":"completed","call_id":"c2","name":"apply_patch","input":"x"}}`+"\n"+
			`{"timestamp":"2026-09-27T10:00:05.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}}`+"\n"+
			`{"timestamp":"2026-09-27T10:00:06.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"thanks"}]}}`+"\n"), 0o644)

	s := statsAt(0, statsNow)
	var cc, cxs Summary
	for _, x := range s.Sessions {
		switch x.Cwd {
		case "/work/tools":
			cc = x
		case "/work/cx":
			cxs = x
		}
	}
	// the prompt and /clear typed; m1 and m2 back; five calls
	if cc.Prompts != 2 || cc.Replies != 2 || cc.ToolCalls != 5 {
		t.Fatalf("claude: %d prompts, %d replies, %d calls (%v)", cc.Prompts, cc.Replies, cc.ToolCalls, cc.tools)
	}
	if cc.tools.of("Bash") != 1 || cc.tools.of("Grep") != 1 || cc.tools.of("mcp__slack__send") != 1 || cc.skills.of("artifact-design") != 1 {
		t.Fatalf("claude tools %v skills %v", cc.tools, cc.skills)
	}
	if cxs.Prompts != 2 || cxs.Replies != 1 || cxs.ToolCalls != 2 || cxs.tools.of("exec_command") != 1 || cxs.tools.of("apply_patch") != 1 {
		t.Fatalf("codex: %+v %v", cxs, cxs.tools)
	}

	o := s.Overview("", "", "/work/tools", 10)
	if o.Tools.Calls != 5 || o.Tools.Sessions != 1 || len(o.Tools.Top) != 5 {
		t.Fatalf("tools %+v", o.Tools)
	}
	cats := map[string]int{}
	for _, c := range o.Tools.Categories {
		cats[c.Name] = c.Calls
	}
	if cats["Bash"] != 1 || cats["Edit"] != 1 || cats["Grep"] != 1 || cats["Tool"] != 1 || cats["Other"] != 1 {
		t.Fatalf("categories %v", cats)
	}
	if len(o.Tools.Weeks) != 1 || o.Tools.Weeks[0].Start != "2026-09-21" || o.Tools.Weeks[0].Calls["Bash"] != 1 {
		t.Fatalf("weeks %+v", o.Tools.Weeks)
	}
	if o.Skills.Calls != 1 || o.Skills.Count != 1 || o.Skills.Top[0].Name != "artifact-design" || o.Skills.Top[0].Last != "2026-09-27" ||
		o.Skills.Top[0].Agents["claude"] != 1 || o.Skills.Top[0].Projects[0].Name != "/work/tools" {
		t.Fatalf("skills %+v", o.Skills)
	}
	// 4 messages, 2 minutes' work, 5 calls in 2 prompts
	if sh := o.Shape["messages"]; sh.Total != 1 || sh.Counts[0] != 1 {
		t.Fatalf("shape by messages %+v", sh)
	}
	if sh := o.Shape["autonomy"]; sh.Counts[1] != 1 {
		t.Fatalf("shape by autonomy %+v", sh)
	}
	day := -1
	for i := range o.Days {
		if o.Messages[i] > 0 {
			day = i
		}
	}
	if day < 0 || o.Messages[day] != 4 || o.Output[day] != 5+1+5 { // the subagent's reply too
		t.Fatalf("messages %v output %v", o.Messages, o.Output)
	}
}

// TestToolUseMergesSpellings: the callers spell a tool's name either way
// ("bash" and "Bash", "read" and "Read"), so the card's detail list counts
// one tool apiece whatever the case: its spellings are one row, shown as its
// summary spells it, a session that called both ways counted once; tools
// that only share a kind stay apart, spelled as called.
func TestToolUseMergesSpellings(t *testing.T) {
	bash := func(n int) []Summary {
		in := []Summary{}
		for i := 0; i < n; i++ {
			in = append(in, Summary{ToolCalls: 70, tools: countsOf(map[string]int{"bash": 70}, nil)})
		}
		return in
	}
	tests := []struct {
		name  string
		in    []Summary
		top   []ToolStat // the card's rows, in its order
		cats  []ToolStat // the summary's rows, when said
		calls int
		sess  int
	}{
		{
			// the issue's shape: 1.4K "bash" calls beside 779 "Bash" ones,
			// read as the summary reads them, "Bash 2.2K"
			name:  "one tool spelled two ways is one row, spelled as its summary does",
			in:    append(bash(20), Summary{ToolCalls: 779, tools: countsOf(map[string]int{"Bash": 779}, nil)}),
			top:   []ToolStat{{Name: "Bash", Category: "Bash", Calls: 2179, Sessions: 21}},
			cats:  []ToolStat{{Name: "Bash", Calls: 2179, Sessions: 21}},
			calls: 2179,
			sess:  21,
		},
		{
			name: "a session that called both ways counts once",
			in: []Summary{
				{ToolCalls: 12, tools: countsOf(map[string]int{"read": 12}, nil)},
				{ToolCalls: 123, tools: countsOf(map[string]int{"read": 12, "Read": 111}, nil)},
			},
			top:   []ToolStat{{Name: "Read", Category: "Read", Calls: 135, Sessions: 2}},
			calls: 135,
			sess:  2,
		},
		{
			// the legend counts each spelling under its own kind, as the
			// weeks do, whichever spelling came first: "task" is Other,
			// "Task" is Task
			name: "the legend keeps each spelling's own kind",
			in: []Summary{
				{ToolCalls: 3, tools: countsOf(map[string]int{"task": 3}, nil)},
				{ToolCalls: 5, tools: countsOf(map[string]int{"Task": 5}, nil)},
			},
			top:   []ToolStat{{Name: "Task", Category: "Task", Calls: 8, Sessions: 2}},
			cats:  []ToolStat{{Name: "Task", Calls: 5, Sessions: 1}, {Name: "Other", Calls: 3, Sessions: 1}},
			calls: 8,
			sess:  2,
		},
		{
			name: "tools that only share a kind stay apart",
			in:   []Summary{{ToolCalls: 12, tools: countsOf(map[string]int{"mcp__github__create_issue": 5, "mcp__github__list_issues": 7}, nil)}},
			top: []ToolStat{
				{Name: "mcp__github__list_issues", Category: "Tool", Calls: 7, Sessions: 1},
				{Name: "mcp__github__create_issue", Category: "Tool", Calls: 5, Sessions: 1},
			},
			calls: 12,
			sess:  1,
		},
	}
	for _, test := range tests {
		ins := make([]*Summary, len(test.in))
		for i := range test.in {
			ins[i] = &test.in[i]
		}
		tu := toolUse(ins, time.Time{}, 10)
		if tu.Calls != test.calls || tu.Sessions != test.sess {
			t.Errorf("%s: %d calls, %d sessions, want %d and %d", test.name, tu.Calls, tu.Sessions, test.calls, test.sess)
		}
		if !reflect.DeepEqual(tu.Top, test.top) {
			t.Errorf("%s: top %+v, want %+v", test.name, tu.Top, test.top)
		}
		if test.cats != nil && !reflect.DeepEqual(tu.Categories, test.cats) {
			t.Errorf("%s: categories %+v, want %+v", test.name, tu.Categories, test.cats)
		}
	}
}

// TestShapesMinutesWithoutMessages: a session whose agent tells no messages
// (pi, grok…) still counts by its minutes at work, and only there.
func TestShapesMinutesWithoutMessages(t *testing.T) {
	sh := shapes([]*Summary{
		{Agent: "claude", Prompts: 2, Replies: 3, ToolCalls: 4, Active: 600},
		{Agent: "pi", Active: 1200},
		{Agent: "grok"},
	})
	if m := sh["minutes"]; m.Total != 2 || m.Counts[1] != 1 || m.Counts[2] != 1 {
		t.Fatalf("minutes %+v", m)
	}
	if m := sh["messages"]; m.Total != 1 || m.Counts[0] != 1 {
		t.Fatalf("messages %+v", m)
	}
	if a := sh["autonomy"]; a.Total != 1 || a.Counts[1] != 1 {
		t.Fatalf("autonomy %+v", a)
	}
}
