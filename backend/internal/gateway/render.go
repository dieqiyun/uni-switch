package gateway

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func stopFrom(p Protocol, s string) string {
	switch s {
	case "max_tokens", "length", "max_output_tokens":
		return "length"
	case "tool_use", "tool_calls":
		return "tool"
	case "content_filter", "refusal":
		return "filter"
	}
	return "stop"
}
func stopTo(p Protocol, s string) string {
	if p == Messages {
		switch s {
		case "length":
			return "max_tokens"
		case "tool":
			return "tool_use"
		case "filter":
			return "refusal"
		}
		return "end_turn"
	}
	if s == "tool" {
		return "tool_calls"
	}
	if s == "filter" {
		return "content_filter"
	}
	return s
}
func usageFrom(p Protocol, u Object) Object {
	if u == nil {
		return Object{}
	}
	if p == Messages {
		return Object{"input_tokens": integer(u["input_tokens"]) + integer(u["cache_read_input_tokens"]) + integer(u["cache_creation_input_tokens"]), "output_tokens": integer(u["output_tokens"]), "cached_tokens": integer(u["cache_read_input_tokens"]), "cache_write_tokens": integer(u["cache_creation_input_tokens"])}
	}
	if p == Chat {
		return Object{"input_tokens": integer(u["prompt_tokens"]), "output_tokens": integer(u["completion_tokens"]), "cached_tokens": integer(field(u, "prompt_tokens_details", "cached_tokens")), "reasoning_tokens": integer(field(u, "completion_tokens_details", "reasoning_tokens"))}
	}
	return Object{"input_tokens": integer(u["input_tokens"]), "output_tokens": integer(u["output_tokens"]), "cached_tokens": integer(field(u, "input_tokens_details", "cached_tokens")), "reasoning_tokens": integer(field(u, "output_tokens_details", "reasoning_tokens"))}
}
func usageTo(p Protocol, u Object) Object {
	in, out, cache, write, reason := integer(u["input_tokens"]), integer(u["output_tokens"]), integer(u["cached_tokens"]), integer(u["cache_write_tokens"]), integer(u["reasoning_tokens"])
	if p == Messages {
		return Object{"input_tokens": max(0, in-cache-write), "output_tokens": out, "cache_read_input_tokens": cache, "cache_creation_input_tokens": write}
	}
	if p == Chat {
		return Object{"prompt_tokens": in, "completion_tokens": out, "total_tokens": in + out, "prompt_tokens_details": Object{"cached_tokens": cache}, "completion_tokens_details": Object{"reasoning_tokens": reason}}
	}
	return Object{"input_tokens": in, "output_tokens": out, "total_tokens": in + out, "input_tokens_details": Object{"cached_tokens": cache}, "output_tokens_details": Object{"reasoning_tokens": reason}}
}
func DecodeResult(p Protocol, v Object) (Result, error) {
	if p == Gemini {
		return decodeGemini(v)
	}
	r := Result{ID: str(v["id"]), Model: str(v["model"]), Usage: usageFrom(p, obj(v["usage"])), Stop: "stop"}
	if v["error"] != nil {
		return r, errors.New("上游返回推理错误")
	}
	if p == Messages {
		var e error
		r.Parts, e = content(v["content"], p)
		r.Stop = stopFrom(p, str(v["stop_reason"]))
		return r, e
	}
	if p == Chat {
		cs := list(v["choices"])
		if len(cs) == 0 {
			return r, errors.New("Chat Completions 回复缺少 choices")
		}
		c := obj(cs[0])
		r.Stop = stopFrom(p, str(c["finish_reason"]))
		m := obj(c["message"])
		ps, e := content(m["content"], Chat)
		if e != nil {
			return r, e
		}
		r.Parts = ps
		if t := str(m["reasoning_content"]); t != "" {
			r.Parts = append([]Part{{Kind: "thinking", Text: t}}, r.Parts...)
		}
		for _, v := range list(m["tool_calls"]) {
			t := obj(v)
			name := str(field(t, "function", "name"))
			if name == "" {
				continue
			}
			var args any
			raw := str(field(t, "function", "arguments"))
			if decode([]byte(raw), &args) != nil {
				return r, errors.New("上游工具参数不是有效 JSON")
			}
			id := str(t["id"])
			if id == "" {
				id = uid("call_")
			}
			r.Parts = append(r.Parts, Part{Kind: "call", ID: id, Name: name, Args: json.RawMessage(raw)})
		}
		if r.Stop == "tool" {
			named := false
			for _, part := range r.Parts {
				named = named || part.Kind == "call"
			}
			if !named {
				r.Stop = "stop"
			}
		}
		return r, nil
	}
	if v["status"] == "failed" {
		return r, errors.New("Responses 推理失败")
	}
	if v["status"] == "incomplete" {
		r.Stop = stopFrom(p, str(field(v, "incomplete_details", "reason")))
	}
	if _, ok := v["output"].([]any); !ok {
		return r, errors.New("Responses 回复缺少 output")
	}
	for _, v := range list(v["output"]) {
		it := obj(v)
		switch str(it["type"]) {
		case "message":
			ps, e := content(it["content"], Responses)
			if e != nil {
				return r, e
			}
			r.Parts = append(r.Parts, ps...)
		case "reasoning":
			ps, _ := content(it["content"], Responses)
			if allText(ps) == "" {
				ps, _ = content(it["summary"], Responses)
			}
			r.Parts = append(r.Parts, Part{Kind: "thinking", Text: allText(ps), Raw: it, SealedBy: "responses"})
		case "function_call":
			var args any
			if decode([]byte(str(it["arguments"])), &args) != nil {
				return r, errors.New("上游工具参数不是有效 JSON")
			}
			r.Parts = append(r.Parts, Part{Kind: "call", ID: str(it["call_id"]), Name: str(it["name"]), Args: args})
			r.Stop = "tool"
		case "custom_tool_call":
			r.Parts = append(r.Parts, Part{Kind: "call", ID: str(it["call_id"]), Name: str(it["name"]), Args: Object{"input": it["input"]}})
			r.Stop = "tool"
		}
	}
	return r, nil
}
func partOutput(p Part, proto Protocol, tools []Tool) Object {
	if proto == Messages {
		switch p.Kind {
		case "text":
			return Object{"type": "text", "text": p.Text}
		case "thinking":
			if p.SealedBy == "messages" && p.Raw["type"] == "redacted_thinking" {
				return p.Raw
			}
			if p.Text == "" && p.Signature == "" {
				return nil
			}
			sig := p.Signature
			if p.SealedBy != "messages" {
				sig = ""
			}
			return Object{"type": "thinking", "thinking": p.Text, "signature": sig}
		case "call":
			return Object{"type": "tool_use", "id": messageCallID(p), "name": p.Name, "input": p.Args}
		}
		return nil
	}
	if p.Kind == "text" {
		return Object{"id": uid("msg_"), "type": "message", "role": "assistant", "status": "completed", "content": []any{Object{"type": "output_text", "text": p.Text, "annotations": []any{}}}}
	}
	if p.Kind == "thinking" {
		if p.SealedBy == "responses" && str(p.Raw["encrypted_content"]) != "" {
			return clone(p.Raw)
		}
		summary := []any{}
		if p.Text != "" {
			summary = append(summary, Object{"type": "summary_text", "text": p.Text})
		}
		it := Object{"id": uid("rs_"), "type": "reasoning", "status": "completed", "summary": summary}
		if p.SealedBy == "messages" && (p.Signature != "" || p.Raw["type"] == "redacted_thinking") {
			raw := Object{"type": "thinking", "thinking": p.Text, "signature": p.Signature}
			if p.Raw["type"] == "redacted_thinking" {
				raw = p.Raw
			}
			it["encrypted_content"] = seal(claudeSeal, raw)
		}
		return it
	}
	if p.Kind == "call" {
		it := Object{"id": uid("fc_"), "type": "function_call", "status": "completed", "call_id": p.ID, "name": p.Name, "arguments": rawArguments(p.Args)}
		for _, t := range tools {
			if t.Name == p.Name {
				it["name"] = t.Original
				if t.Namespace != "" {
					it["namespace"] = t.Namespace
				}
				if t.Custom {
					it["type"] = "custom_tool_call"
					it["input"] = customInput(p.Args)
					delete(it, "arguments")
				}
				break
			}
		}
		return it
	}
	return nil
}
func Render(r Result, p Protocol, model string, tools []Tool) Object {
	if p == Gemini {
		return renderGemini(r, model)
	}
	if r.ID == "" {
		r.ID = uid("resp_")
	}
	if model == "" {
		model = r.Model
	}
	u := usageTo(p, r.Usage)
	items := []any{}
	for _, part := range r.Parts {
		if it := partOutput(part, p, tools); it != nil {
			items = append(items, it)
		}
	}
	if p == Messages {
		id := r.ID
		if !strings.HasPrefix(id, "msg_") {
			id = "msg_" + id
		}
		return Object{"id": id, "type": "message", "role": "assistant", "model": model, "content": items, "stop_reason": stopTo(p, r.Stop), "stop_sequence": nil, "usage": u}
	}
	if p == Chat {
		text, reason := "", ""
		calls := []any{}
		for _, part := range r.Parts {
			switch part.Kind {
			case "text":
				text += part.Text
			case "thinking":
				reason += part.Text
			case "call":
				calls = append(calls, Object{"id": part.ID, "type": "function", "function": Object{"name": part.Name, "arguments": rawArguments(part.Args)}})
			}
		}
		msg := Object{"role": "assistant", "content": text}
		if reason != "" {
			msg["reasoning_content"] = reason
		}
		if len(calls) > 0 {
			msg["tool_calls"] = calls
		}
		return Object{"id": r.ID, "object": "chat.completion", "model": model, "created": time.Now().Unix(), "choices": []any{Object{"index": 0, "message": msg, "finish_reason": stopTo(p, r.Stop)}}, "usage": u}
	}
	status := "completed"
	var details any
	if r.Stop == "length" || r.Stop == "filter" {
		status = "incomplete"
		why := "max_output_tokens"
		if r.Stop == "filter" {
			why = "content_filter"
		}
		details = Object{"reason": why}
	}
	return Object{"id": r.ID, "object": "response", "created_at": time.Now().Unix(), "model": model, "status": status, "output": items, "usage": u, "incomplete_details": details, "error": nil, "parallel_tool_calls": true, "tool_choice": "auto", "tools": []any{}}
}
