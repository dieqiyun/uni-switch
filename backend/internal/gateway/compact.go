package gateway

import (
	"encoding/base64"
	"errors"
	"strings"
)

const compactPrompt = "Create a handoff summary of the conversation for another model to resume the task. Preserve the request, constraints, decisions, completed work, tool results, unresolved errors, and clear next steps. Return only a concise factual summary."

// These are client-owned compaction prompt signatures, not broad matching on
// the word "summary" in ordinary conversation. Quoted memories after the lead
// do not turn a normal task into a summary request.
func (r *Request) summarizing() bool {
	leads := func(text, marker string) bool {
		at := strings.Index(text, marker)
		return at >= 0 && at <= 2048
	}
	for _, marker := range []string{
		"You are a helpful AI assistant tasked with summarizing conversations.",
		"You are a context summarization agent.",
		"You are a context summarization assistant.",
		"You are a specialized system component responsible for distilling chat history into a structured XML <state_snapshot>",
		"You are the component that summarizes a conversation when its context window is about to overflow.",
		"You are a context compaction engine. Your summary will REPLACE the original messages in the conversation context window.",
		"You are a helpful assistant that compacts conversation context.",
		"You are compacting a long conversation so it can continue WITHOUT losing context.",
		"You are UPDATING an existing conversation summary with new activity so the conversation can continue WITHOUT losing context.",
	} {
		if leads(r.System, marker) {
			return true
		}
	}
	for i := len(r.Turns) - 1; i >= 0; i-- {
		if r.Turns[i].Role != "user" {
			continue
		}
		for _, p := range r.Turns[i].Parts {
			if p.Kind != "text" {
				continue
			}
			for _, marker := range []string{compactPrompt, "Your task is to create a detailed summary of the conversation so far", "You are performing a CONTEXT CHECKPOINT COMPACTION.", "You are now given a task to compact this conversation context", "Merge the previous summary with the new conversation.", "Here is the full conversation to compact."} {
				if leads(p.Text, marker) {
					return true
				}
			}
			if strings.HasPrefix(strings.TrimSpace(p.Text), "Compact this conversation into a context summary:") && strings.Contains(p.Text, "END OF CONVERSATION TO COMPACT.") {
				return true
			}
		}
		break
	}
	return false
}

func prepareCompaction(r *Request) bool {
	compact := false
	for _, v := range list(r.Original["input"]) {
		if field(v, "type") == "compaction_trigger" {
			compact = true
		}
	}
	if !compact {
		return false
	}
	push(&r.Turns, "user", Part{Kind: "text", Text: compactPrompt})
	r.Tools = nil
	r.HostedTools = nil
	r.WebSearch = false
	r.Choice = "auto"
	b := false
	r.Parallel = &b
	return true
}
func compactReply(result Result, p Protocol, model string, stream bool, sink Sink) error {
	toolAnswer := result.Stop == "tool"
	for _, part := range result.Parts {
		toolAnswer = toolAnswer || part.Kind == "call"
	}
	if toolAnswer || result.Stop == "length" || result.Stop == "filter" {
		return sendJSON(sink, 502, failure(p, 502, "压缩摘要未完整结束"))
	}
	summary := allText(result.Parts)
	if summary == "" {
		return sendJSON(sink, 502, failure(p, 502, "上游没有生成压缩摘要"))
	}
	item := Object{"type": "compaction", "id": uid("cmp_"), "encrypted_content": seal(compactSeal, Object{"summary": summary})}
	if !stream {
		response := Render(Result{Stop: "stop", Usage: result.Usage}, Responses, model, nil)
		response["output"] = []any{item}
		return sendJSON(sink, 200, response)
	}
	if p != Responses {
		return errors.New("压缩触发仅适用于 Responses")
	}
	if e := sink(Packet{Type: "head", Status: 200, Headers: map[string]string{"content-type": "text/event-stream; charset=utf-8", "cache-control": "no-cache"}}); e != nil {
		return e
	}
	id := uid("resp_")
	response := Render(Result{ID: id, Stop: "stop", Usage: result.Usage}, Responses, model, nil)
	response["output"] = []any{item}
	start := clone(response)
	start["status"] = "in_progress"
	start["output"] = []any{}
	start["usage"] = nil
	for i, ev := range []Object{{"type": "response.created", "response": start}, {"type": "response.in_progress", "response": start}, {"type": "response.output_item.added", "output_index": 0, "item": item}, {"type": "response.output_item.done", "output_index": 0, "item": item}, {"type": "response.completed", "response": response}} {
		ev["sequence_number"] = i
		if e := sink(Packet{Type: "data", Data: base64.StdEncoding.EncodeToString(frameBytes(Frame{str(ev["type"]), encode(ev)}))}); e != nil {
			return e
		}
	}
	return sink(Packet{Type: "end"})
}
