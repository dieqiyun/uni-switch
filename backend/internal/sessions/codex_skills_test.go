package sessions

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Codex has no tool for skills: the model calls one up by reading its
// SKILL.md through a shell command. The two calls here are real lines from
// Codex rollouts, byte for byte: one through code mode's exec script
// (tools.exec_command), the tool every call goes through in Codex's current
// sessions (MaxMooreMy on X: a day of Codex use, "exec" 488 calls, no
// skill), and one through exec_command itself. Each calls up imagegen; a
// patch writing a SKILL.md, a redirect into one and a find for them call
// up none.
func TestCodexSkillReads(t *testing.T) {
	inZone(t, 0)
	_, codex := setup(t)
	id := "019fdaf9-90aa-78b1-8db0-2ff9edf69052"
	cx := filepath.Join(codex, "sessions", "2026", "08", "07", "rollout-2026-08-07T14-46-56-"+id+".jsonl")
	os.MkdirAll(filepath.Dir(cx), 0o755)
	os.WriteFile(cx, []byte(
		`{"timestamp":"2026-08-07T06:46:56.000Z","type":"session_meta","payload":{"id":"`+id+`","cwd":"/work/skills"}}`+"\n"+
			`{"timestamp":"2026-08-07T06:46:57.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"draw a cat"}]}}`+"\n"+
			// real: code mode
			`{"timestamp":"2026-08-07T06:47:06.196Z","ordinal":14,"type":"response_item","payload":{"type":"custom_tool_call","id":"ctc_02280df79713d205016a757f695740819bbb199e7e518fc531","status":"completed","call_id":"call_meBGaRczwfCsNl9FQOEhhCMu","name":"exec","input":"const r = await tools.exec_command({cmd:\"cat '/Users/yetone/.codex/skills/.system/imagegen/SKILL.md'\",\"yield_time_ms\":10000,\"max_output_tokens\":12000}); text(r.output)\n","internal_chat_message_metadata_passthrough":{"turn_id":"019fdaf9-9764-74e0-b3f7-99cabf4e015c"}}}`+"\n"+
			// real: exec_command (2026-07-26, its timestamp moved to this session's day)
			`{"timestamp":"2026-08-07T06:47:35.723Z","ordinal":14,"type":"response_item","payload":{"type":"function_call","id":"fc_02a31ee780cd8312016a6634fe3e9c819a99ec3bbf765da4e9","name":"exec_command","arguments":"{\"cmd\":\"cat '/Users/yetone/.codex/skills/.system/imagegen/SKILL.md'\",\"workdir\":\"/Users/yetone/Library/Application Support/Codex Image Session Helper/inputs/2026-07-26T16-21-48-987Z\",\"yield_time_ms\":10000,\"max_output_tokens\":20000}","call_id":"call_1YSUTjeI9CNi9YVoHIYhODVH","internal_chat_message_metadata_passthrough":{"turn_id":"019f9f3e-e6e9-7b01-ad7e-858c2bfeaaf7"}}}`+"\n"+
			// writes and searches call up nothing
			`{"timestamp":"2026-08-07T06:48:00.000Z","type":"response_item","payload":{"type":"custom_tool_call","status":"completed","call_id":"c3","name":"apply_patch","input":"*** Begin Patch\n*** Add File: /Users/u/.codex/skills/mine/SKILL.md\n+---\n*** End Patch\n"}}`+"\n"+
			`{"timestamp":"2026-08-07T06:48:01.000Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"cat > /Users/u/.codex/skills/mine/SKILL.md <<'EOF'\\nx\\nEOF\"}","call_id":"c4"}}`+"\n"+
			`{"timestamp":"2026-08-07T06:48:02.000Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"find ~/.codex/skills -name SKILL.md\"}","call_id":"c5"}}`+"\n"), 0o644)

	s := statsAt(0, statsNow)
	var got Summary
	for _, x := range s.Sessions {
		if x.Cwd == "/work/skills" {
			got = x
		}
	}
	if got.ToolCalls != 5 || got.tools.of("exec") != 1 || got.tools.of("exec_command") != 3 {
		t.Fatalf("tools %d %v", got.ToolCalls, got.tools)
	}
	if got.skills.of("imagegen") != 2 || len(got.skills) != 1 {
		t.Fatalf("skills %v", got.skills)
	}
	o := s.Overview("", "", "", 10)
	if o.Skills.Calls != 2 || o.Skills.Top[0].Name != "imagegen" || o.Skills.Top[0].Agents["codex"] != 2 || o.Skills.Top[0].Last != "2026-08-07" {
		t.Fatalf("overview skills %+v", o.Skills)
	}
}

func TestCxSkillReads(t *testing.T) {
	for _, c := range []struct {
		line, name string
		want       []string
	}{
		{`{"payload":{"type":"function_call","name":"shell","arguments":"{\"command\":[\"bash\",\"-lc\",\"sed -n '1,200p' /a/skills/pdf/SKILL.md; cat /a/skills/pdf/SKILL.md /a/skills/docx/SKILL.md\"]}"}}`, "shell", []string{"pdf", "docx"}},
		{`{"payload":{"type":"local_shell_call","action":{"type":"exec","command":["cat","/a/skills/xlsx/SKILL.md"]}}}`, "local_shell_call", []string{"xlsx"}},
		{`{"payload":{"type":"function_call","name":"shell_command","arguments":"{\"command\":\"Get-Content C:\\\\Users\\\\u\\\\.codex\\\\skills\\\\pptx\\\\SKILL.md\"}"}}`, "shell_command", []string{"pptx"}},
		{`{"payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"cat SKILL.md /a/*/SKILL.md /a/x/SKILL.md.bak | tee /a/y/SKILL.md\"}"}}`, "exec_command", nil},
	} {
		if got := cxSkillReads([]byte(c.line), c.name); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.line, got, c.want)
		}
	}
}
