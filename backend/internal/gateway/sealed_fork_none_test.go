package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// forkNoneTask is the first request Codex 0.162 (rust-v0.162.0-alpha.17.2)
// sends for a subagent its lead spawned with fork_turns="none", rebuilt
// from Codex's source, not the reporter's bytes (#1855 has none):
// spawn.rs's fork_mode is None for "none", so no turn of the lead's is
// copied in, and the input is the subagent's own context and its task.
// The task is spawn_agent's message, which the ChatGPT backend returned
// sealed (encrypted_function_args), so agent_message_from_tool makes it
// AgentMessage::Encrypted and to_model_input_item writes it under the
// NEW_TASK header as encrypted_content.
const forkNoneTask = `{"model":"ag/gemini-3.8-flash","stream":true,"store":false,"instructions":"You are Codex.","tools":[],"tool_choice":"auto","parallel_tool_calls":true,"include":["reasoning.encrypted_content"],"input":[` +
	`{"type":"message","role":"developer","content":[{"type":"input_text","text":"<permissions instructions>Filesystem sandboxing: workspace-write.</permissions instructions>"}]},` +
	`{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>\n  <cwd>/Users/u/proj</cwd>\n  <shell>zsh</shell>\n</environment_context>"}]},` +
	`{"type":"agent_message","author":"/root","recipient":"/root/gemini_worker","content":[{"type":"input_text","text":"Message Type: NEW_TASK\nTask name: /root/gemini_worker\nSender: /root\nPayload:\n"},{"type":"encrypted_content","encrypted_content":"gAAAAABo_sealedTaskFromTheLead-Q3x9_aZ=="}]}]}`

// #1855 (hanekoo): Codex Desktop's lead on gpt-6.1-sol, Codex's own
// ChatGPT sign-in, spawns a subagent on ag/gemini-3.8-flash with
// fork_turns="none". The task is still sealed, since it is the task
// itself that is, so the subagent is turned away before ag is asked, and
// told so: fork_turns="none" isn't left looking like it should have helped.
func TestSealedTaskWithForkTurnsNoneSaysTheTaskItselfIsSealed(t *testing.T) {
	fresh(t)
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[]}}`))
	})
	asked := chatOnlyOn(t, "ag")
	s := New()
	if rec := codexSubTurn(s, "gpt-6.1-sol", "lead-1855", "", leadAsk); rec.Code != 200 {
		t.Fatalf("lead on Codex's own sign-in: %d %s", rec.Code, rec.Body.String())
	}
	// chatOnlyOn serves "dfmodel"; the subagent asks for the reporter's model
	body := strings.ReplaceAll(forkNoneTask, "ag/gemini-3.8-flash", "ag/dfmodel")
	req := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer chatgpt-token")
	req.Header.Set("chatgpt-account-id", "acct-1")
	req.Header.Set("User-Agent", "Codex Desktop/0.162.0-alpha.17.2 (Mac OS 15.7.3; arm64) unknown (Codex Desktop; 26.1007.30912)")
	req.Header.Set("session_id", "worker-1855")
	req.Header.Set("x-codex-parent-thread-id", "lead-1855")
	req.Header.Set("x-openai-subagent", "collab_spawn")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	var got struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	msg := got.Error.Message
	if rec.Code != 400 || *asked != 0 || strings.Contains(rec.Body.String(), "gAAAAA") {
		t.Fatalf("subagent: %d %s (ag asked %d times)", rec.Code, rec.Body.String(), *asked)
	}
	if !strings.Contains(msg, "sealed by the ChatGPT backend that answered its lead") ||
		!strings.Contains(msg, "What is sealed is the task itself") ||
		!strings.Contains(msg, "spawn_agent") ||
		!strings.Contains(msg, `fork_turns="none" keeps the lead's earlier turns out of the subagent, but not the seal on its task`) ||
		!strings.Contains(msg, "Give the subagent the lead's model") {
		t.Fatalf("the refusal doesn't say the task itself is sealed whatever fork_turns says: %s", msg)
	}
}

// #2245 (qikairo7): a lead on gpt-6.1-sol, a ChatGPT account, spawns a
// subagent on a group of DeepSeek only. The subagent is turned away as
// before, and the refusal now names the way to subagents on any model:
// Settings' "Codex subagents on any model" (#141), which has the lead
// hand its tasks on as text. With it on, a thread still sealing its tasks
// is told why: begun before it, or Codex's own multi_agent_v2.
func TestSealedTaskNamesSubagentsOnAnyModel(t *testing.T) {
	fresh(t)
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[]}}`))
	})
	asked := chatOnlyOn(t, "ag")
	s := New()
	if rec := codexSubTurn(s, "gpt-6.1-sol", "lead-2245", "", leadAsk); rec.Code != 200 {
		t.Fatalf("lead on a ChatGPT model: %d %s", rec.Code, rec.Body.String())
	}
	refusal := func() string {
		t.Helper()
		body := strings.ReplaceAll(forkNoneTask, "ag/gemini-3.8-flash", "ag/dfmodel")
		req := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer chatgpt-token")
		req.Header.Set("chatgpt-account-id", "acct-1")
		req.Header.Set("User-Agent", "Codex Desktop/0.162.0-alpha.17.2 (Windows 10.0.26300; x86_64) unknown (Codex Desktop; 26.1007.71326)")
		req.Header.Set("session_id", "worker-2245")
		req.Header.Set("x-codex-parent-thread-id", "lead-2245")
		req.Header.Set("x-openai-subagent", "collab_spawn")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		var got struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		json.Unmarshal(rec.Body.Bytes(), &got)
		if rec.Code != 400 || *asked != 0 {
			t.Fatalf("subagent: %d %s (ag asked %d times)", rec.Code, rec.Body.String(), *asked)
		}
		return got.Error.Message
	}
	if msg := refusal(); !strings.Contains(msg, `turn on "Codex subagents on any model" in Codex's settings on uni-switch's Agents page and start a new thread`) {
		t.Fatalf("the refusal doesn't name the setting: %s", msg)
	}
	st := settings.Load()
	st.CodexAgentsV1 = true
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	if msg := refusal(); !strings.Contains(msg, `"Codex subagents on any model" is on in Codex's settings on uni-switch's Agents page`) || !strings.Contains(msg, "features.multi_agent_v2") || strings.Contains(msg, "turn on") {
		t.Fatalf("with the setting on, the refusal doesn't say why the thread is still sealed: %s", msg)
	}
}
