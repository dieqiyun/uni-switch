package gateway

import (
	"strings"
	"testing"
)

// Each agent's compaction request is told from its prompt; an ordinary
// request, or one only quoting a prompt earlier on, isn't.
func TestCompacting(t *testing.T) {
	user := func(text string) Message { return Message{Role: "user", Parts: []Part{{Kind: Text, Text: text}}} }
	said := Message{Role: "assistant", Parts: []Part{{Kind: Text, Text: "done"}}}
	for name, req := range map[string]*Request{
		"claude code": {System: "You are Claude Code, Anthropic's official CLI for Claude.\nYou are a helpful AI assistant tasked with summarizing conversations.",
			Messages: []Message{user("fix it"), said, user("Your task is to create a detailed summary of the conversation so far, paying close attention to the user's explicit requests")}},
		"codex":      {Messages: []Message{user("fix it"), said, user(codexCompactPrompt)}},
		"opencode":   {System: "You are a context summarization agent. You are given a conversation between a user and an agent.", Messages: []Message{user("fix it")}},
		"pi":         {System: "You are a context summarization assistant. Your task is to read a conversation", Messages: []Message{user("<conversation>…</conversation>")}},
		"gemini cli": {System: "You are a specialized system component responsible for distilling chat history into a structured XML <state_snapshot>.", Messages: []Message{user("fix it")}},
		"qwen code":  {System: "You are the component that summarizes a conversation when its context window is about to overflow. The summary", Messages: []Message{user("fix it")}},
		"kimi":       {Messages: []Message{user("fix it"), said, user("---\n\nThe above is a list of messages in an agent conversation. You are now given a task to compact this conversation context according to specific priorities and rules.")}},
	} {
		if !compacting(req) {
			t.Errorf("%s: not taken for compacting", name)
		}
	}
	for name, req := range map[string]*Request{
		"plain":  {System: "You are a coding agent.", Messages: []Message{user("fix it")}},
		"quoted": {Messages: []Message{user(codexCompactPrompt), said, user("thanks, go on")}},
		"nil":    nil,
	} {
		if compacting(req) {
			t.Errorf("%s: taken for compacting", name)
		}
	}
}

// Minis's prompts as Minis sends them (OpenMinis b4c0661: Android's
// ChatViewModel.compactSummarySystemPrompt and its user message, iOS's
// AIChatViewModel+Compaction.swift, whose line continuations render the
// first paragraph to the same bytes) (#1635, jueda12).
const minisCompactSystem = `You are a context compaction engine. Your summary will REPLACE the original messages in the conversation context window. The agent will read your summary as past context, then proceed based on the user's NEXT message — your summary is background, not a standing work order. Write the summary in the same language the user used in the conversation.

MUST PRESERVE (never omit or shorten):
- All file paths, directory names, URLs, UUIDs, and identifiers — copy verbatim
- Commands executed and their outcomes (success/failure/output)
- What was requested and what was done (record as past events, not as ongoing goals)
- Key decisions made and their rationale
- Errors encountered and how they were resolved
- Important constraints, rules, or user preferences mentioned
- Any tool calls and their results that affect current state

STRUCTURE:
1. Start with a one-line description of what the conversation was about (use past tense — "User asked X, agent did Y", NOT "Goal: X").
2. Then a concise narrative of what happened, preserving technical details.
3. End with a "What had been done so far" section listing completed work — NOT a "todo" or "pending" list. Do not invent ongoing objectives or carry-over tasks from old turns; if the user wants to continue, they will say so in their next message.

PRIORITIZE recent context over older history — recent decisions and recent file/path references are most useful for continuity.

Do NOT translate or alter code snippets, file paths, identifiers, or error messages. Be concise but never lose information the agent needs.`

func minisCompactAsk(conversation string) string {
	return "Compact this conversation into a context summary:\n\n" + conversation +
		"\n\n---\nEND OF CONVERSATION TO COMPACT.\n\n" +
		"Now generate a structured context summary following the system prompt instructions. Do NOT continue the conversation above — summarize it. " +
		"Write everything in past tense, framed as \"what was discussed / what was done\", NOT as an ongoing goal or todo list."
}

// Minis compacts with one request, no tools: its own system prompt and the
// conversation wrapped in one user message. Either one tells it; an
// ordinary Minis turn, asking to compact something else or after a
// compaction, isn't one.
func TestCompactingMinis(t *testing.T) {
	user := func(text string) Message { return Message{Role: "user", Parts: []Part{{Kind: Text, Text: text}}} }
	said := Message{Role: "assistant", Parts: []Part{{Kind: Text, Text: "done"}}}
	conversation := "[user]: 幫我整理桌面的檔案\n[assistant]: 已整理完成"
	for name, req := range map[string]*Request{
		"minis":          {System: minisCompactSystem, Messages: []Message{user(minisCompactAsk(conversation))}},
		"system only":    {System: minisCompactSystem, Messages: []Message{user(conversation)}},
		"user only":      {Messages: []Message{user(minisCompactAsk(conversation))}},
		"system message": {System: "You are an on-device AI assistant.\n\n" + minisCompactSystem, Messages: []Message{user(minisCompactAsk(conversation))}},
	} {
		if !compacting(req) {
			t.Errorf("%s: not taken for compacting", name)
		}
	}
	for name, req := range map[string]*Request{
		"turn":  {System: "You are an on-device AI assistant running on iOS.", Messages: []Message{user("Compact this conversation into a short note for my boss")}},
		"after": {System: "You are an on-device AI assistant running on iOS.", Messages: []Message{user(minisCompactAsk(conversation)), said, user("繼續")}},
	} {
		if compacting(req) {
			t.Errorf("%s: taken for compacting", name)
		}
	}
}

// A system prompt that only quotes a compaction prompt, behind the agent's
// own, isn't a compaction: Minis puts its memory (GLOBAL.md, the daily log)
// after its own prompt in every request, so one memory holding Minis's
// compaction line took every turn of that conversation for one (#1635,
// jueda12: 28 requests in a row, one with 4 tokens of chat, went to the
// compaction rule's model). The prompts that lead the system prompt, alone
// or behind a short lead, still are.
func TestCompactingIgnoresQuotedPromptInSystem(t *testing.T) {
	user := func(text string) Message { return Message{Role: "user", Parts: []Part{{Kind: Text, Text: text}}} }
	// Minis's own prompt runs to thousands of tokens (19k-23k with its
	// tools, in the report) before the memory it adds
	own := "You are an on-device AI assistant running on iOS.\n\n" +
		strings.Repeat("- Use the tools to read and change the user's files, notes and reminders; ask before deleting anything.\n", 60)
	notes := "以下是先前的筆記：\n\n" +
		"「You are a context compaction engine. Your summary will REPLACE the original messages in the conversation context window.」\n\n" +
		"其餘是一般對話內容……"
	for name, req := range map[string]*Request{
		"memory":      {System: own + "\n\n" + notes, Messages: []Message{user("hello there")}},
		"claude code": {System: own + "\n\nYou are a helpful AI assistant tasked with summarizing conversations.", Messages: []Message{user("hello there")}},
	} {
		if compacting(req) {
			t.Errorf("%s: a system prompt that merely quotes a compaction prompt was taken for a compaction", name)
		}
	}
	conversation := "[user]: 幫我整理桌面的檔案\n[assistant]: 已整理完成"
	for name, req := range map[string]*Request{
		"alone":       {System: minisCompactSystem, Messages: []Message{user(conversation)}},
		"short lead":  {System: "You are an on-device AI assistant.\n\n" + minisCompactSystem, Messages: []Message{user(conversation)}},
		"claude code": {System: "x-anthropic-billing-header: cc_version=2.1.296; cc_entrypoint=cli;You are Claude Code, Anthropic's official CLI for Claude.You are a helpful AI assistant tasked with summarizing conversations.", Messages: []Message{user("fix it")}},
		"kimi (1.52)": {System: "You are a helpful assistant that compacts conversation context.", Messages: []Message{user("## Message 1\nRole: user\nContent:\nfix it")}},
		"gemini cli":  {System: "You are a specialized system component responsible for distilling chat history into a structured XML <state_snapshot>.", Messages: []Message{user("fix it")}},
	} {
		if !compacting(req) {
			t.Errorf("%s: not taken for compacting", name)
		}
	}
}

// The last user message counts as a compaction's ask only where the agent
// puts its ask: at the start of a text part (Claude Code 2.1.296's behind
// its TEXT ONLY preamble; Kimi's in a part of its own after the
// conversation), or, for Minis, anywhere in a part that opens with Minis's
// own first line. A user writing about one of them, as on #1635, isn't
// compacting.
func TestCompactingAsksWhereTheAgentPutsThem(t *testing.T) {
	user := func(parts ...string) Message {
		m := Message{Role: "user"}
		for _, p := range parts {
			m.Parts = append(m.Parts, Part{Kind: Text, Text: p})
		}
		return m
	}
	said := Message{Role: "assistant", Parts: []Part{{Kind: Text, Text: "done"}}}
	// Claude Code 2.1.296's compaction ask: its preamble (hnn), then the
	// instructions
	ccAsk := "CRITICAL: Respond with TEXT ONLY. Do NOT call any tools.\n\n" +
		"- Do NOT use Read, Bash, Grep, Glob, Edit, Write, or ANY other tool.\n" +
		"- You already have all the context you need in the conversation above.\n" +
		"- Tool calls will be REJECTED and will waste your only turn — you will fail the task.\n" +
		"- Your entire response must be plain text: an <analysis> block followed by a <summary> block.\n\n" +
		"Your task is to create a detailed summary of the conversation so far, paying close attention to the user's explicit requests and your previous actions."
	history := strings.Repeat("[user]: 幫我整理桌面的檔案\n[assistant]: 已整理完成\n", 200) // well past compactLead
	for name, req := range map[string]*Request{
		"claude code": {Messages: []Message{user("fix it"), said, user(ccAsk)}},
		"kimi": {Messages: []Message{user("## Message 1\nRole: user\nContent:\n", history,
			"\n---\n\nThe above is a list of messages in an agent conversation. You are now given a task to compact this conversation context according to specific priorities and rules.")}},
		"minis": {Messages: []Message{user(minisCompactAsk(history))}},
	} {
		if !compacting(req) {
			t.Errorf("%s: not taken for compacting", name)
		}
	}
	for name, req := range map[string]*Request{
		"minis marker quoted": {Messages: []Message{user("Minis 壓縮時，會把整段對話包成一則以 \"END OF CONVERSATION TO COMPACT.\" 結尾的 user 訊息，這樣對嗎？")}},
		"ask deep in a paste": {Messages: []Message{user(history + "\nYour task is to create a detailed summary of the conversation so far — what does this line in my log mean?")}},
	} {
		if compacting(req) {
			t.Errorf("%s: taken for compacting", name)
		}
	}
}

// Alma's compaction ask, as the report on #2833 quotes it: the first bytes
// of the last user message, the conversation behind it.
const almaCompactAsk = "Merge the previous summary with the new conversation. Produce the <analysis>, then the merged structured summary exactly as instructed. The conversation continues after your summary — do not conclude it."
