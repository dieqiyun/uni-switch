package sessions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// kindsOf counts a transcript's parts by role and kind.
func kindsOf(tr Transcript) map[string]int {
	n := map[string]int{}
	for _, p := range tr.Parts {
		n[p.Role+"/"+p.Kind]++
	}
	return n
}

// #2056 (infinitr0us): a Kimi Code session opens as a transcript, read from
// the #1607 reporter's whole wire files (text redacted by them, every
// record's shape as Kimi Code wrote it). The counts are the files' own: the
// prompts and loop events of 1.4 and 1.5, the untimed messages of 1.0, and
// nothing from the timed context.append_message or agent.message.appended
// records, which say again what those said.
func TestKimiCodeTranscript(t *testing.T) {
	inZone(t, 0)
	claude, _ := setup(t)
	kimiSessionsIn(t, filepath.Join(filepath.Dir(claude), "home", ".kimi-code"))
	ss := List(0)
	for _, c := range []struct {
		id    string
		first string
		want  map[string]int
	}{
		// 1.4, three prompts typed and a background task's steer
		{kimiJune, "user/text", map[string]int{"user/text": 3, "user/context": 1, "assistant/thinking": 25, "assistant/text": 6, "assistant/tool_use": 35, "tool/tool_result": 35}},
		// 1.4, cut off mid-request
		{kimiCancel, "user/text", map[string]int{"user/text": 1, "assistant/thinking": 6, "assistant/text": 1, "assistant/tool_use": 9, "tool/tool_result": 9}},
		// 1.5 with no agent.message.appended, started by a skill
		{kimiEarly, "user/context", map[string]int{"user/context": 1, "assistant/thinking": 8, "assistant/text": 5, "assistant/tool_use": 10, "tool/tool_result": 10}},
		// 1.5 whose one call failed: its prompt, once
		{"session_0f2d1067-903e-4a6a-9f1f-df1f0b0e606c", "user/text", map[string]int{"user/text": 1}},
		// imported from kimi-cli: context.append_message alone
		{kimiOld, "user/text", map[string]int{"user/text": 1, "assistant/thinking": 1, "assistant/text": 1}},
	} {
		s := find(t, ss, "kimi", c.id)
		if !s.Transcript {
			t.Fatalf("%s: listed as having no transcript", c.id)
		}
		tr, err := TranscriptOf(s)
		if err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		got := kindsOf(tr)
		if len(got) != len(c.want) {
			t.Fatalf("%s: parts %v, want %v", c.id, got, c.want)
		}
		for k, n := range c.want {
			if got[k] != n {
				t.Fatalf("%s: parts %v, want %v", c.id, got, c.want)
			}
		}
		if p := tr.Parts[0]; p.Role+"/"+p.Kind != c.first {
			t.Fatalf("%s: starts with %+v", c.id, p)
		}
	}
}

// A ses_* session Kimi Code opened again: kimi-cli's messages (an
// assistant's tool calls as Kimi Code imports them, and OpenAI's shape),
// then Kimi Code's own turn, each record in the shape the reporter's files
// have. Each part comes once, in order, a result named for its call.
func TestKimiCodeTranscriptOrder(t *testing.T) {
	dir := t.TempDir()
	wire := filepath.Join(dir, "wire.jsonl")
	lines := []string{
		`{"type":"metadata","protocol_version":"1.4","created_at":1772085117694}`,
		`{"type":"context.append_message","message":{"role":"user","content":[{"type":"text","text":"old ask"}],"toolCalls":[]}}`,
		`{"type":"context.append_message","message":{"role":"assistant","content":[{"type":"think","think":"old think"},{"type":"text","text":"old reply"}],"toolCalls":[{"type":"function","id":"tool_A","name":"Shell","arguments":"{\"command\":\"ls\"}"},{"type":"function","id":"tool_B","function":{"name":"ReadFile","arguments":"{\"path\":\"a.go\"}"}}]}}`,
		`{"type":"context.append_message","message":{"role":"tool","content":[{"type":"text","text":"a.go"},{"type":"text","text":"b.go"}],"toolCalls":[],"toolCallId":"tool_A"}}`,
		`{"type":"context.append_message","message":{"role":"tool","content":[{"type":"text","text":"package a"}],"toolCalls":[],"toolCallId":"tool_B"}}`,
		`{"type":"config.update","modelAlias":"kimi-code/k3","thinkingEffort":"max","time":1784436245384}`,
		`{"type":"turn.prompt","input":[{"type":"text","text":"new ask"}],"origin":{"kind":"user"},"time":1784436249636}`,
		`{"type":"context.append_message","agentId":"main","message":{"role":"user","content":[{"type":"text","text":"new ask"}],"toolCalls":[],"origin":{"kind":"user"}},"time":1784436249636}`,
		`{"type":"context.append_message","agentId":"main","message":{"role":"user","content":[{"type":"text","text":"<system-reminder>date</system-reminder>"}],"toolCalls":[],"origin":{"kind":"injection"}},"time":1784436249637}`,
		`{"type":"context.append_loop_event","event":{"type":"step.begin","uuid":"u1","turnId":"0","step":1},"time":1784436249640}`,
		`{"type":"context.append_loop_event","event":{"type":"content.part","uuid":"u2","turnId":"0","step":1,"part":{"type":"think","think":"new think"}},"time":1784436254379}`,
		`{"type":"context.append_loop_event","event":{"type":"tool.call","uuid":"tool_C","turnId":"0","step":1,"toolCallId":"tool_C","name":"Grep","args":{"pattern":"needle"},"description":"x"},"time":1784436254398}`,
		`{"type":"context.append_loop_event","event":{"type":"tool.result","parentUuid":"p","toolCallId":"tool_C","result":{"output":"found the haystack","isError":true}},"time":1784436254401}`,
		`{"type":"context.append_loop_event","event":{"type":"content.part","uuid":"u3","turnId":"0","step":2,"part":{"type":"text","text":"new reply"}},"time":1784436255379}`,
		`{"message":{"message":{"role":"assistant","content":[{"type":"text","text":"new reply"}],"toolCalls":[{"id":"tool_C","name":"Grep","arguments":"{}"}]}},"type":"agent.message.appended","time":1784436255400,"kind":"event"}`,
		`{"type":"context.append_loop_event","event":{"type":"step.end","uuid":"u1","turnId":"0","step":2,"finishReason":"end_turn"},"time":1784436255406}`,
	}
	if err := os.WriteFile(wire, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Session{Agent: "kimi", ID: "ses_x", Path: wire}
	tr, err := TranscriptOf(s)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range tr.Parts {
		got = append(got, p.Role+"/"+p.Kind+"/"+p.Name+"/"+strings.Join(strings.Fields(p.Text), " "))
	}
	want := []string{
		"user/text//old ask",
		"assistant/thinking//old think",
		"assistant/text//old reply",
		`assistant/tool_use/Shell/{ "command": "ls" }`,
		`assistant/tool_use/ReadFile/{ "path": "a.go" }`,
		"tool/tool_result/Shell/a.go b.go",
		"tool/tool_result/ReadFile/package a",
		"user/text//new ask",
		"assistant/thinking//new think",
		`assistant/tool_use/Grep/{ "pattern": "needle" }`,
		"tool/tool_result/Grep/found the haystack",
		"assistant/text//new reply",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("parts:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// search reads the same parts
	h, found, err := SearchSession(context.Background(), s, NeedleOf("haystack"))
	if err != nil || !found || h.Kind != "tool_result" || !strings.Contains(strings.ToLower(h.Match), "haystack") {
		t.Fatalf("search: %+v %v %v", h, found, err)
	}
}
