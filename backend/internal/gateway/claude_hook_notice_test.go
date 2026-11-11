package gateway

import (
	"strings"
	"testing"
)

// What Claude Code 2.1.296 sends after a Bash call when a PreToolUse and a
// PostToolUse hook each give additionalContext, as it sent it to a local
// stand-in for Anthropic (#2060): one {role: "system"} message after the
// results, with the hooks' notices and its <total_tokens> count. The call's
// id is the one continuing waits on.
const (
	hooksAfter = `PreToolUse:Bash hook additional context: <context_guidance>prefer ctx_execute</context_guidance>\n\n` +
		`PostToolUse:Bash hook additional context: post context here\n\n<total_tokens>14999985 tokens left</total_tokens>`
	// and with a message the user sent while the call ran, read in with
	// --input-format stream-json
	hooksAndUserAfter = `PreToolUse:Bash hook additional context: <context_guidance>prefer ctx_execute</context_guidance>\n\n` +
		`PostToolUse:Bash hook additional context: post context here\n\n` +
		`The user sent a new message while you were working:\nalso tell me the date\n\n` +
		`This is how Claude Code surfaces messages the user sends mid-turn — within the running turn, often alongside the next tool result, rather than as a separate conversation turn. Address the message above as you continue this turn.\n\n` +
		`<total_tokens>14999985 tokens left</total_tokens>`
	hookCallStart = `{"model":"claude-sonnet-5","max_tokens":64,"messages":[` +
		`{"role":"user","content":"run echo hi"},` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"call-1","name":"Bash","input":{"command":"echo hi","description":"say hi"}}]},` +
		`{"role":"user","content":[{"tool_use_id":"call-1","type":"tool_result","content":"hi","is_error":false}]},`
	cc296Hooks        = hookCallStart + `{"role":"system","content":[{"type":"text","text":"` + hooksAfter + `","cache_control":{"type":"ephemeral"}}]}]}`
	cc296HooksAndUser = hookCallStart + `{"role":"system","content":[{"type":"text","text":"` + hooksAndUserAfter + `","cache_control":{"type":"ephemeral"}}]}]}`
	// the shape #2060 reports: the notices as text blocks of the results'
	// message, in reminders, one with nothing in it
	reportedHooks = `{"model":"claude-sonnet-5","max_tokens":64,"messages":[` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"call-1","name":"Read","input":{"file_path":"a.go"}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":"package a"},` +
		`{"type":"text","text":"<system-reminder>\nPreToolUse:Read hook additional context: <context_guidance>…</context_guidance>\n</system-reminder>"},` +
		`{"type":"text","text":"<total_tokens>14956781 tokens left</total_tokens>"},` +
		`{"type":"text","text":"<system-reminder>\n</system-reminder>"}]}]}`
)

// A hook's notices and the token count after a tool's results are Claude
// Code's, not a message of the user's: the results go on in their run with
// nothing written to its Claude Code as the user's, which told its model
// "The user sent a new message while you were working:" after nearly every
// call, of a hook's output or of nothing (#2060).
func TestClaudeHookNoticesAreNotTheUsersMessage(t *testing.T) {
	for name, body := range map[string]string{"2.1.296": cc296Hooks, "reported": reportedHooks} {
		t.Run(name, func(t *testing.T) {
			got, said, _ := continuing(t, body)
			if text := resultText(got); text != "hi" && text != "package a" {
				t.Fatalf("call-1 was answered %q", text)
			}
			if len(said) != 0 {
				t.Fatalf("Claude Code was written %q as the user's", said)
			}
		})
	}
}

// A message the user sent mid-turn among the notices is theirs, its words
// alone: the run's Claude Code tells them its own way.
func TestClaudeUsersMessageAmongHookNoticesGoesOnAlone(t *testing.T) {
	_, said, _ := continuing(t, cc296HooksAndUser)
	if len(said) != 1 || said[0] != "also tell me the date" {
		t.Fatalf("Claude Code was written %q, want the user's words alone", said)
	}
}

// A run started anew, or a turn resumed, is told a notice after a result
// with the result closed, not marked as the user's.
func TestClaudePromptToldHookNoticesNotAsTheUsers(t *testing.T) {
	req := parsed(t, reportedHooks)
	var turn strings.Builder
	for _, b := range renderClaudeTurn(req.Messages, nil) {
		if s, ok := b["text"].(string); ok {
			turn.WriteString(s)
		}
	}
	if strings.Contains(turn.String(), "[user message]") {
		t.Fatalf("a hook's notice was told as the user's:\n%s", turn.String())
	}
	if !strings.Contains(turn.String(), "[end of tool result id=call-1]") {
		t.Fatalf("the result isn't closed before the notice:\n%s", turn.String())
	}
}

// What a user may write is still theirs: words about a hook, a reminder
// with words in it.
func TestAgentNoticeLeavesTheUsersWords(t *testing.T) {
	for _, s := range []string{
		"Stop hook is broken: fix it first",
		"<system-reminder>also answer the message I sent while the tool was running</system-reminder>",
		summaryAsk,
	} {
		if agentNotice(s) {
			t.Errorf("%q was taken for the agent's notice", s)
		}
	}
	for _, s := range []string{
		"", " \n", "<system-reminder>\n</system-reminder>", "<total_tokens>12 tokens left</total_tokens>",
		"Token usage: 1200/200000; 198800 remaining",
		"<system-reminder>\nSessionStart:startup hook success: loaded\n</system-reminder>",
		"Stop hook blocking error from command: \"x\": no",
		"<system-reminder>\nThe TodoWrite tool hasn't been used recently.\n</system-reminder>",
	} {
		if !agentNotice(s) {
			t.Errorf("%q was taken for the user's words", s)
		}
	}
}
