package gateway

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

func content(v any, proto Protocol) ([]Part, error) {
	if s, ok := v.(string); ok {
		if s == "" {
			return nil, nil
		}
		return []Part{{Kind: "text", Text: s}}, nil
	}
	if v == nil {
		return nil, nil
	}
	a, ok := v.([]any)
	if !ok {
		return nil, invalid("content 必须为文本或数组")
	}
	var out []Part
	for _, v := range a {
		m := obj(v)
		p := Part{Raw: m}
		switch str(m["type"]) {
		case "text", "input_text", "output_text", "summary_text", "reasoning_text":
			p.Kind = "text"
			p.Text = str(m["text"])
		case "image":
			p.Kind = "image"
			s := obj(m["source"])
			p.Image = Object{"url": s["url"], "media_type": s["media_type"], "data": s["data"]}
		case "input_image", "image_url":
			p.Kind = "image"
			u := str(m["image_url"])
			if u == "" {
				u = str(field(m, "image_url", "url"))
			}
			p.Image = Object{"url": u, "detail": m["detail"]}
		case "tool_use":
			p.Kind = "call"
			p.ID = rawID(str(m["id"]))
			p.Name = str(m["name"])
			p.Args = m["input"]
		case "tool_result":
			p.Kind = "result"
			p.ID = rawID(str(m["tool_use_id"]))
			p.IsError = m["is_error"] == true
			var e error
			p.Content, e = content(m["content"], proto)
			if e != nil {
				return nil, e
			}
			p.Text = allText(p.Content)
		case "thinking":
			p.Kind = "thinking"
			p.Text = str(m["thinking"])
			p.Signature = str(m["signature"])
			p.SealedBy = "messages"
		case "redacted_thinking":
			p.Kind = "thinking"
			p.SealedBy = "messages"
		case "tool_reference":
			p.Kind = "text"
			p.Text = "Available tool: " + str(m["tool_name"])
		case "input_file", "file", "document":
			return nil, invalid("此附件格式尚未实现，不能丢弃附件后继续推理")
		default:
			return nil, invalid("不支持的消息内容类型")
		}
		out = append(out, p)
	}
	return out, nil
}

func readTools(r *Request, tools []any, namespace string) error {
	for _, v := range tools {
		t := obj(v)
		kind := str(t["type"])
		if kind == "web_search" || kind == "web_search_preview" || strings.HasPrefix(kind, "web_search_") {
			r.WebSearch = true
			continue
		}
		if kind == "namespace" {
			if e := readTools(r, list(t["tools"]), str(t["name"])); e != nil {
				return e
			}
			continue
		}
		if r.Source == Chat {
			t = obj(t["function"])
		}
		name := str(t["name"])
		if name == "" {
			return invalid("工具缺少名称")
		}
		spec := Tool{Original: name, Name: name, Namespace: namespace, Description: str(t["description"]), Schema: t["parameters"], Custom: kind == "custom", Strict: t["strict"] == true}
		if r.Source == Messages {
			spec.Schema = t["input_schema"]
			spec.Custom = false
		}
		if namespace != "" {
			spec.Name = namespace + "__" + name
		}
		if spec.Custom {
			spec.Schema = Object{"type": "object", "properties": Object{"input": Object{"type": "string"}}, "required": []string{"input"}, "additionalProperties": false}
			if t["format"] != nil {
				spec.Description += "\nRaw tool input format: " + encode(t["format"])
			}
		}
		if spec.Schema == nil {
			spec.Schema = Object{"type": "object", "properties": Object{}}
		}
		if !safeChars(spec.Name) || len(spec.Name) > 64 {
			h := sha256.Sum256([]byte(namespace + "\x00" + name))
			spec.Name = fmt.Sprintf("uni_tool_%x", h[:12])
		}
		for _, old := range r.Tools {
			if old.Name == spec.Name {
				return invalid("工具名称冲突")
			}
		}
		r.Tools = append(r.Tools, spec)
	}
	return nil
}
func (r *Request) toolName(name, namespace string) string {
	for _, t := range r.Tools {
		if t.Original == name && t.Namespace == namespace {
			return t.Name
		}
	}
	if namespace != "" {
		return namespace + "__" + name
	}
	return name
}

func Parse(source Protocol, body Object) (*Request, error) {
	if source == Gemini {
		return parseGemini(body)
	}
	r := &Request{Source: source, Model: str(body["model"]), Stream: body["stream"] == true, Original: body, Temp: body["temperature"], TopP: body["top_p"]}
	if source == Chat {
		body = clone(body)
		repairChatHistory(body)
	}
	if r.Model == "" {
		return nil, invalid("缺少 model")
	}
	if e := readTools(r, list(body["tools"]), ""); e != nil {
		return nil, e
	}
	if b, ok := body["parallel_tool_calls"].(bool); ok {
		r.Parallel = &b
	}
	var ts []any
	if source == Responses {
		r.Max = integer(body["max_output_tokens"])
		r.System = str(body["instructions"])
		r.Effort = effort(str(field(body, "reasoning", "effort")))
		r.Off = str(field(body, "reasoning", "effort")) == "none"
		r.Think = obj(body["reasoning"]) != nil
		r.Format = obj(field(body, "text", "format"))
		if body["previous_response_id"] != nil {
			return nil, invalid("转换需要完整 input 历史，不支持 previous_response_id")
		}
		if s, ok := body["input"].(string); ok {
			ts = []any{Object{"role": "user", "content": s}}
		} else {
			var ok bool
			ts, ok = body["input"].([]any)
			if !ok {
				return nil, invalid("input 必须为文本或数组")
			}
		}
	} else {
		var ok bool
		ts, ok = body["messages"].([]any)
		if !ok {
			return nil, invalid("缺少 messages")
		}
		r.Max = integer(body["max_tokens"])
		if n := integer(body["max_completion_tokens"]); n > 0 {
			r.Max = n
		}
		if source == Messages {
			ps, e := content(body["system"], source)
			if e != nil {
				return nil, e
			}
			for _, p := range ps {
				if p.Kind != "text" {
					return nil, invalid("system 只支持文本")
				}
			}
			r.System = allText(ps)
			r.Stop = list(body["stop_sequences"])
			th := str(field(body, "thinking", "type"))
			r.Off = th == "disabled" || th == "between_tools"
			r.Think = th == "enabled" || th == "adaptive"
			if r.Think {
				r.Effort = effortBudget(integer(field(body, "thinking", "budget_tokens")))
				if e := str(field(body, "output_config", "effort")); e != "" {
					r.Effort = effort(e)
				}
			}
			r.Format = obj(field(body, "output_config", "format"))
		} else {
			r.Effort = effort(str(body["reasoning_effort"]))
			r.Off = str(body["reasoning_effort"]) == "none"
			r.Think = r.Effort != ""
			r.Format = obj(body["response_format"])
			r.Stop = list(body["stop"])
			if s, ok := body["stop"].(string); ok {
				r.Stop = []any{s}
			}
		}
	}
	for _, v := range ts {
		m := obj(v)
		kind := str(m["type"])
		role := str(m["role"])
		switch kind {
		case "function_call", "custom_tool_call":
			name := r.toolName(str(m["name"]), str(m["namespace"]))
			args := m["arguments"]
			if s, ok := args.(string); ok {
				if decode([]byte(s), &args) != nil {
					return nil, invalid("工具参数不是 JSON")
				}
				args = json.RawMessage(s)
			}
			if kind == "custom_tool_call" {
				args = Object{"input": m["input"]}
			}
			push(&r.Turns, "assistant", Part{Kind: "call", ID: str(m["call_id"]), Name: name, Args: args})
		case "function_call_output", "custom_tool_call_output":
			ps, e := content(m["output"], source)
			if e != nil {
				ps = []Part{{Kind: "text", Text: encode(m["output"])}}
			}
			push(&r.Turns, "user", Part{Kind: "result", ID: str(m["call_id"]), Content: ps, Text: allText(ps)})
		case "reasoning":
			if encrypted := str(m["encrypted_content"]); strings.HasPrefix(encrypted, "uni-anthropic-continuation:1") || strings.HasPrefix(encrypted, "uni-openai-reasoning:") {
				return nil, invalid("此旧版续接封装已移除，请从完整历史重新开始对话")
			}
			p := Part{Kind: "thinking", Raw: m, SealedBy: "responses"}
			ps, _ := content(m["content"], source)
			if allText(ps) == "" {
				ps, _ = content(m["summary"], source)
			}
			p.Text = allText(ps)
			if b, ours := openSeal(str(m["encrypted_content"]), claudeSeal); ours {
				if b == nil {
					return nil, invalid("思考续接记录损坏")
				}
				p.Raw = b
				p.Text = str(b["thinking"])
				p.Signature = str(b["signature"])
				p.SealedBy = "messages"
			} else if b, ours := openSeal(str(m["encrypted_content"]), "uni-anthropic-thinking:"); ours {
				p.Raw = b
				p.Text = str(b["thinking"])
				p.Signature = str(b["signature"])
				p.SealedBy = "messages"
			}
			push(&r.Turns, "assistant", p)
		case "compaction":
			cp, ours := openSeal(str(m["encrypted_content"]), compactSeal)
			if ours && cp != nil {
				push(&r.Turns, "user", Part{Kind: "text", Text: str(cp["summary"])})
				continue
			}
			if s, ok := strings.CutPrefix(str(m["encrypted_content"]), "uni-anthropic-compaction:"); ok {
				b, e := base64.StdEncoding.DecodeString(s)
				if e != nil {
					return nil, invalid("压缩记录损坏")
				}
				push(&r.Turns, "user", Part{Kind: "text", Text: string(b)})
				continue
			}
			return nil, invalid("此压缩记录需要原来的上游协议")
		case "configuration_update":
			if e := str(field(m, "reasoning", "effort")); e != "" && r.Think {
				r.Effort = effort(e)
				r.Off = e == "none"
			}
		case "compaction_trigger":
			// The trigger becomes a summary ask after protocol parsing.
		case "message", "", "agent_message":
			if kind == "agent_message" {
				role = "user"
			}
			if role != "user" && role != "assistant" && role != "system" && role != "developer" && role != "tool" {
				return nil, invalid("消息角色不受支持")
			}
			ps, e := content(m["content"], source)
			if e != nil {
				return nil, e
			}
			if source == Chat && role == "tool" {
				role = "user"
				ps = []Part{{Kind: "result", ID: str(m["tool_call_id"]), Text: allText(ps), Content: ps}}
			}
			for _, v := range list(m["tool_calls"]) {
				t := obj(v)
				a := field(t, "function", "arguments")
				var args any
				if decode([]byte(str(a)), &args) != nil {
					return nil, invalid("工具参数不是 JSON")
				}
				ps = append(ps, Part{Kind: "call", ID: str(t["id"]), Name: str(field(t, "function", "name")), Args: json.RawMessage(str(a))})
			}
			if s := str(m["reasoning_content"]); s != "" {
				ps = append([]Part{{Kind: "thinking", Text: s}}, ps...)
			}
			push(&r.Turns, role, ps...)
		default:
			return nil, invalid("不支持的历史条目类型")
		}
	}
	choice := body["tool_choice"]
	if source == Messages {
		switch str(field(choice, "type")) {
		case "auto", "none":
			r.Choice = str(field(choice, "type"))
		case "any":
			r.Choice = "required"
		case "tool":
			r.Choice = "name:" + str(field(choice, "name"))
		}
		if field(choice, "disable_parallel_tool_use") == true {
			b := false
			r.Parallel = &b
		}
	} else if s, ok := choice.(string); ok {
		r.Choice = s
	} else if m := obj(choice); m != nil {
		name := str(m["name"])
		if source == Chat {
			name = str(field(m, "function", "name"))
		}
		if name != "" {
			r.Choice = "name:" + r.toolName(name, str(m["namespace"]))
		} else if str(m["type"]) == "allowed_tools" {
			r.Choice = str(m["mode"])
			allowed := map[string]bool{}
			for _, v := range list(m["tools"]) {
				t := obj(v)
				allowed[r.toolName(str(t["name"]), str(t["namespace"]))] = true
			}
			var kept []Tool
			for _, t := range r.Tools {
				if allowed[t.Name] {
					kept = append(kept, t)
				}
			}
			r.Tools = kept
		}
	}
	if len(r.Turns) == 0 {
		trigger := false
		for _, it := range list(body["input"]) {
			if field(it, "type") == "compaction_trigger" {
				trigger = true
			}
		}
		if !trigger {
			return nil, invalid("没有可发送的对话")
		}
	}
	return r, nil
}

func customInput(v any) string {
	if m := obj(v); m != nil {
		if s, ok := m["input"].(string); ok {
			return s
		}
	}
	if s, ok := v.(string); ok {
		return s
	}
	return encode(v)
}
func rawArguments(v any) string {
	if v == nil {
		return "{}"
	}
	if r, ok := v.(json.RawMessage); ok {
		return string(r)
	}
	return encode(v)
}
