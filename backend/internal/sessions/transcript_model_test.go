package sessions

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// models are a transcript's parts as role/kind=model, so a test reads which
// model each one is said with.
func models(ps []Part) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Role + "/" + p.Kind + "=" + p.Model
	}
	return out
}

func wantModels(t *testing.T, tr Transcript, want []string) {
	t.Helper()
	if got := models(tr.Parts); !reflect.DeepEqual(got, want) {
		t.Fatalf("models\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Each assistant reply in a Claude Code session is said with the model its
// own line names, the one that answered it (#2253, cnlove7777777-art); a
// line Claude Code wrote itself ("<synthetic>") and what the user and the
// tools said name none.
func TestTranscriptModelClaude(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sess1.jsonl")
	writeLines(t, file,
		claudeUser(1, `"Fix the login bug"`),
		claudeBlocks("m1", 2, `{"type":"thinking","thinking":"Read it."}`),
		claudeBlocks("m1", 3, `{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/a"}}`),
		claudeUser(4, `[{"type":"tool_result","tool_use_id":"t1","content":"package a"}]`),
		strings.Replace(claudeBlocks("m2", 5, `{"type":"text","text":"Fixed."}`), `"claude-opus-5-5"`, `"glm-5.2"`, 1),
		claudeUser(6, `"Again"`),
		strings.Replace(claudeBlocks("m3", 7, `{"type":"text","text":"API Error: 529"}`), `"claude-opus-5-5"`, `"<synthetic>"`, 1),
	)
	tr, err := TranscriptOf(Session{Agent: "claude", ID: "sess1", Path: file})
	if err != nil {
		t.Fatal(err)
	}
	wantModels(t, tr, []string{
		"user/text=",
		"assistant/thinking=claude-opus-5-5",
		"assistant/tool_use=claude-opus-5-5",
		"tool/tool_result=",
		"assistant/text=glm-5.2",
		"user/text=",
		"assistant/text=",
	})
}

// A Codex rollout names the model of each turn in its turn_context line:
// a turn's replies are said with it, and a /model between turns moves the
// next turn's replies onto the new one.
func TestTranscriptModelCodex(t *testing.T) {
	file := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeLines(t, file,
		`{"type":"session_meta","payload":{"id":"x"}}`,
		`{"type":"turn_context","payload":{"turn_id":"a","model":"gpt-6-astra","effort":"high"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"List the files"}]}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{}","call_id":"c1"}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"c1","output":"a.go"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"One file."}]}}`,
		`{"type":"turn_context","payload":{"turn_id":"b","model":"gpt-6-luna","effort":"low"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Thanks"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Welcome."}]}}`,
	)
	tr, err := TranscriptOf(Session{Agent: "codex", ID: "x", Path: file})
	if err != nil {
		t.Fatal(err)
	}
	wantModels(t, tr, []string{
		"user/text=",
		"assistant/tool_use=gpt-6-astra",
		"tool/tool_result=",
		"assistant/text=gpt-6-astra",
		"user/text=",
		"assistant/text=gpt-6-luna",
	})
}

// A Pi reply names the model that answered it in its own message.
func TestTranscriptModelPi(t *testing.T) {
	file := filepath.Join(t.TempDir(), "pi.jsonl")
	writeLines(t, file,
		`{"type":"session","version":3,"id":"s","cwd":"/w"}`,
		`{"type":"message","id":"e1","parentId":null,"message":{"role":"user","content":"Rename foo"}}`,
		`{"type":"message","id":"e2","parentId":"e1","message":{"role":"assistant","provider":"relay","model":"deepseek-v4","content":[{"type":"toolCall","id":"t1","name":"bash","arguments":{}}]}}`,
		`{"type":"message","id":"e3","parentId":"e2","message":{"role":"toolResult","toolCallId":"t1","toolName":"bash","content":[{"type":"text","text":"ok"}]}}`,
		`{"type":"message","id":"e4","parentId":"e3","message":{"role":"assistant","provider":"anthropic","model":"claude-opus-5-5","content":[{"type":"text","text":"Renamed."}]}}`,
	)
	tr, err := TranscriptOf(Session{Agent: "pi", ID: "s", Path: file})
	if err != nil {
		t.Fatal(err)
	}
	wantModels(t, tr, []string{
		"user/text=",
		"assistant/tool_use=deepseek-v4",
		"tool/tool_result=",
		"assistant/text=claude-opus-5-5",
	})
}

// An OpenCode 2 reply is said with its own model.id.
func TestTranscriptModelOpenCodeV2(t *testing.T) {
	data, _ := setupMore(t)
	db := ocMakeV2(t, filepath.Join(data, "opencode", "opencode.db"))
	defer db.Close()
	tr, err := TranscriptOf(find(t, List(All), "opencode", ocMain))
	if err != nil {
		t.Fatal(err)
	}
	wantModels(t, tr, []string{
		"user/text=",
		"assistant/thinking=gpt-6-astra",
		"assistant/text=gpt-6-astra",
		"assistant/text=codex/gpt-6-luna",
	})
}

// An OpenCode reply kept in its files is said with its message's modelID.
func TestTranscriptModelOpenCodeFiles(t *testing.T) {
	data, _ := setupMore(t)
	dir := filepath.Join(data, "opencode", "storage", "part", "msg_0002")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prt_0003.json"), []byte(`{"id":"prt_0003","sessionID":"`+ocMain+`","messageID":"msg_0002","type":"text","text":"Let me look."}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tr, err := TranscriptOf(find(t, List(All), "opencode", ocMain))
	if err != nil {
		t.Fatal(err)
	}
	wantModels(t, tr, []string{
		"user/text=",
		"assistant/text=gpt-6-astra",
	})
}
