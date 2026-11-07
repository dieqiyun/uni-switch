package gateway

import (
	"encoding/json"
	"strings"
)

const geminiSeal = "uni-gemini1:"

func geminiSchema(v any) any {
	if o := obj(v); o != nil {
		out := Object{}
		for k, x := range o {
			if k == "type" {
				if s, ok := x.(string); ok {
					x = strings.ToLower(s)
				}
			}
			switch k {
			case "properties", "$defs", "definitions", "patternProperties":
				if names := obj(x); names != nil {
					mapped := Object{}
					for name, schema := range names {
						mapped[name] = geminiSchema(schema)
					}
					out[k] = mapped
				} else {
					out[k] = x
				}
			case "items", "prefixItems", "additionalProperties", "anyOf", "oneOf", "allOf", "not", "contains", "if", "then", "else":
				out[k] = geminiSchema(x)
			default:
				out[k] = x
			}
		}
		if out["nullable"] == true {
			delete(out, "nullable")
			switch typ := out["type"].(type) {
			case string:
				out["type"] = []any{typ, "null"}
			case []any:
				out["type"] = append(typ, "null")
			}
		}
		delete(out, "propertyOrdering")
		return out
	}
	if a := list(v); a != nil {
		out := []any{}
		for _, x := range a {
			out = append(out, geminiSchema(x))
		}
		return out
	}
	return v
}

func geminiThinking(sig string) ([]Part, error) {
	v, own := openSeal(sig, geminiSeal)
	if !own {
		return nil, nil
	}
	if v == nil {
		return nil, invalid("Gemini 推理续接数据无效")
	}
	b, _ := json.Marshal(v["parts"])
	var ps []Part
	if json.Unmarshal(b, &ps) != nil {
		return nil, invalid("Gemini 推理续接数据无效")
	}
	for _, p := range ps {
		if p.Kind != "thinking" || (p.SealedBy != "messages" && p.SealedBy != "responses") {
			return nil, invalid("Gemini 推理续接来源无效")
		}
	}
	return ps, nil
}

// Normalize only protocol-owned keys. JSON Schema properties, tool arguments,
// labels and tool results belong to the caller and must retain their spelling.
func geminiNormalize(v any, opaque bool) any {
	if opaque {
		return v
	}
	if a := list(v); a != nil {
		out := []any{}
		for _, x := range a {
			out = append(out, geminiNormalize(x, false))
		}
		return out
	}
	m := obj(v)
	if m == nil {
		return v
	}
	out := Object{}
	for k, x := range m {
		key := k
		if strings.Contains(k, "_") {
			bits := strings.Split(k, "_")
			for i := 1; i < len(bits); i++ {
				if bits[i] != "" {
					bits[i] = strings.ToUpper(bits[i][:1]) + bits[i][1:]
				}
			}
			key = strings.Join(bits, "")
		}
		if key != k && m[key] != nil {
			continue
		}
		own := key == "args" || key == "response" || key == "parameters" || key == "parametersJsonSchema" || key == "responseSchema" || key == "responseJsonSchema" || key == "labels"
		out[key] = geminiNormalize(x, own)
	}
	return out
}
func parseGemini(body Object) (*Request, error) {
	body = obj(geminiNormalize(body, false))
	r := &Request{Source: Gemini, Model: str(body["model"]), Stream: body["stream"] == true, Original: body, GenConfig: Object{}}
	if r.Model == "" {
		return nil, invalid("缺少 model")
	}
	sys := body["systemInstruction"]
	if t, ok := sys.(string); ok {
		r.System = t
	} else {
		for _, v := range list(obj(sys)["parts"]) {
			p := obj(v)
			if t, ok := p["text"].(string); ok {
				if r.System != "" {
					r.System += "\n"
				}
				r.System += t
			} else {
				return nil, invalid("Gemini systemInstruction 只支持文本")
			}
		}
	}
	cfg := obj(body["generationConfig"])
	r.Max = integer(cfg["maxOutputTokens"])
	r.Temp = cfg["temperature"]
	r.TopP = cfg["topP"]
	r.Stop = list(cfg["stopSequences"])
	for k, v := range cfg {
		switch k {
		case "maxOutputTokens", "temperature", "topP", "stopSequences", "responseMimeType", "responseSchema", "responseJsonSchema", "thinkingConfig", "candidateCount":
		default:
			r.GenConfig[k] = v
		}
	}
	if cfg["responseMimeType"] == "application/json" {
		r.Format = Object{"type": "json_object"}
		schema := cfg["responseJsonSchema"]
		if schema == nil {
			schema = cfg["responseSchema"]
		}
		if schema != nil {
			r.Format = Object{"type": "json_schema", "name": "response", "schema": geminiSchema(schema)}
		}
		r.System += "\nReturn a valid JSON value."
	}
	if th := obj(cfg["thinkingConfig"]); th != nil {
		r.Think = true
		r.Off = th["thinkingBudget"] != nil && integer(th["thinkingBudget"]) == 0
		r.Effort = strings.ToLower(str(th["thinkingLevel"]))
		if r.Effort == "" && integer(th["thinkingBudget"]) > 0 {
			r.Effort = effortBudget(integer(th["thinkingBudget"]))
		}
	}
	pending := map[string][]string{}
	last := map[string]string{}
	for _, v := range list(body["contents"]) {
		m := obj(v)
		role := "user"
		if m["role"] == "model" || m["role"] == "assistant" {
			role = "assistant"
		}
		for _, x := range list(m["parts"]) {
			part := obj(x)
			var p Part
			useRole := role
			switch {
			case part["text"] != nil:
				k := "text"
				if part["thought"] == true {
					k = "thinking"
				}
				p = Part{Kind: k, Text: str(part["text"]), SealedBy: "gemini", Signature: str(part["thoughtSignature"])}
			case part["inlineData"] != nil || part["fileData"] != nil:
				p = geminiAttachment(part)
			case part["functionCall"] != nil:
				sig := str(part["thoughtSignature"])
				ps, e := geminiThinking(sig)
				if e != nil {
					return nil, e
				}
				if len(ps) > 0 {
					push(&r.Turns, role, ps...)
					sig = ""
				}
				call := obj(part["functionCall"])
				name := str(call["name"])
				if name == "" {
					return nil, invalid("Gemini 工具缺少名称")
				}
				id := str(call["id"])
				if id == "" {
					id = uid("call_")
				}
				pending[name] = append(pending[name], id)
				last[name] = id
				p = Part{Kind: "call", ID: id, Name: name, Args: call["args"], Signature: sig, SealedBy: "gemini"}
			case part["functionResponse"] != nil:
				res := obj(part["functionResponse"])
				name := str(res["name"])
				id := str(res["id"])
				if id == "" {
					if len(pending[name]) > 0 {
						id = pending[name][0]
						pending[name] = pending[name][1:]
					} else {
						id = last[name]
					}
				} else {
					for i, x := range pending[name] {
						if x == id {
							pending[name] = append(pending[name][:i], pending[name][i+1:]...)
							break
						}
					}
				}
				if id == "" {
					id = uid("orphan_")
				}
				txt := encode(res["response"])
				p = Part{Kind: "result", ID: id, Name: name, Text: txt, Content: []Part{{Kind: "text", Text: txt}}, Raw: res}
				useRole = "user"
				for _, v := range list(res["parts"]) {
					a := obj(v)
					if a["inlineData"] != nil || a["fileData"] != nil {
						p.Content = append(p.Content, geminiAttachment(a))
					} else if a["text"] != nil {
						p.Content = append(p.Content, Part{Kind: "text", Text: str(a["text"])})
					}
				}
			default:
				return nil, invalid("不支持的 Gemini part，不能丢弃后继续请求")
			}
			push(&r.Turns, useRole, p)
		}
	}
	if len(r.Turns) == 0 {
		return nil, invalid("缺少 Gemini contents")
	}
	for _, v := range list(body["tools"]) {
		tool := obj(v)
		for k, x := range tool {
			if k == "googleSearch" || k == "googleSearchRetrieval" {
				r.WebSearch = true
				r.HostedTools = append(r.HostedTools, Object{k: x})
			} else if k != "functionDeclarations" {
				r.HostedTools = append(r.HostedTools, Object{k: x})
			}
		}
		for _, d := range list(tool["functionDeclarations"]) {
			f := obj(d)
			schema := f["parametersJsonSchema"]
			if schema == nil {
				schema = f["parameters"]
			}
			if schema == nil {
				schema = Object{"type": "object", "properties": Object{}}
			}
			name := str(f["name"])
			if name == "" {
				return nil, invalid("Gemini 工具缺少名称")
			}
			r.Tools = append(r.Tools, Tool{Name: name, Original: name, Description: str(f["description"]), Schema: geminiSchema(schema)})
		}
	}
	mode := strings.ToUpper(str(field(body, "toolConfig", "functionCallingConfig", "mode")))
	if mode == "NONE" {
		r.Choice = "none"
	}
	if mode == "ANY" || mode == "VALIDATED" {
		allowed := list(field(body, "toolConfig", "functionCallingConfig", "allowedFunctionNames"))
		if len(allowed) > 0 {
			kept := []Tool{}
			for _, t := range r.Tools {
				for _, n := range allowed {
					if n == t.Name {
						kept = append(kept, t)
						break
					}
				}
			}
			r.Tools = kept
		}
		if mode == "ANY" {
			if len(r.Tools) == 0 {
				return nil, invalid("Gemini ANY 没有可调用工具")
			}
			r.Choice = "required"
			if len(r.Tools) == 1 {
				r.Choice = "name:" + r.Tools[0].Name
			}
		}
	}
	return r, nil
}
func geminiAttachment(p Object) Part {
	data := obj(p["inlineData"])
	f := Object{}
	if data != nil {
		f["media_type"] = data["mimeType"]
		f["data"] = data["data"]
	} else {
		data = obj(p["fileData"])
		f["media_type"] = data["mimeType"]
		f["url"] = data["fileUri"]
	}
	if strings.HasPrefix(str(f["media_type"]), "image/") {
		return Part{Kind: "image", Image: f}
	}
	return Part{Kind: "file", File: f}
}
func geminiPart(p Part) Object {
	switch p.Kind {
	case "text":
		v := Object{"text": p.Text}
		if p.SealedBy == "gemini" && p.Signature != "" {
			v["thoughtSignature"] = p.Signature
		}
		return v
	case "thinking":
		if p.Text != "" || p.SealedBy == "gemini" && p.Signature != "" {
			v := Object{"text": p.Text, "thought": true}
			if p.SealedBy == "gemini" && p.Signature != "" {
				v["thoughtSignature"] = p.Signature
			}
			return v
		}
	case "image":
		return geminiMedia(p.Image)
	case "file":
		return geminiMedia(p.File)
	case "call":
		v := Object{"functionCall": Object{"id": p.ID, "name": p.Name, "args": obj(p.Args)}}
		if p.SealedBy == "gemini" && p.Signature != "" {
			v["thoughtSignature"] = p.Signature
		}
		return v
	}
	return nil
}
func renderGemini(r Result, model string) Object {
	parts := []any{}
	var held []Part
	for _, p := range r.Parts {
		if p.Kind == "thinking" && (p.SealedBy == "messages" || p.SealedBy == "responses") {
			held = append(held, p)
		}
		if v := geminiPart(p); v != nil {
			if p.Kind == "call" && len(held) > 0 {
				v["thoughtSignature"] = seal(geminiSeal, Object{"parts": held})
				held = nil
			}
			parts = append(parts, v)
		}
	}
	stop := "STOP"
	if r.Stop == "length" {
		stop = "MAX_TOKENS"
	}
	if r.Stop == "filter" {
		stop = "SAFETY"
	}
	u := usageTo(Chat, r.Usage)
	return Object{"responseId": r.ID, "modelVersion": model, "candidates": []any{Object{"index": 0, "content": Object{"role": "model", "parts": parts}, "finishReason": stop}}, "usageMetadata": Object{"promptTokenCount": u["prompt_tokens"], "candidatesTokenCount": max(0, integer(u["completion_tokens"])-integer(r.Usage["reasoning_tokens"])), "totalTokenCount": u["total_tokens"], "thoughtsTokenCount": r.Usage["reasoning_tokens"], "cachedContentTokenCount": r.Usage["cached_tokens"]}}
}
func (e *Encoder) geminiEvent(ev Event) error {
	if e.finished {
		return nil
	}
	if ev.Kind == "usage" {
		e.usage = ev.Usage
		return nil
	}
	chunk := func(p Object) error {
		return e.send("", Object{"responseId": e.id, "modelVersion": e.model, "candidates": []any{Object{"index": 0, "content": Object{"role": "model", "parts": []any{p}}}}})
	}
	switch ev.Kind {
	case "part":
		e.slots[ev.Index] = &outputSlot{Part: ev.Part}
		e.order = append(e.order, ev.Index)
		if ev.Part.Kind != "call" && ev.Part.Text != "" {
			return chunk(geminiPart(ev.Part))
		}
	case "part_update":
		if s := e.slots[ev.Index]; s != nil {
			s.Part = ev.Part
		}
	case "delta":
		s := e.slots[ev.Index]
		if s == nil {
			return invalid("Gemini 流式内容缺少起始事件")
		}
		if ev.Field == "text" {
			p := Object{"text": ev.Text}
			if s.Part.Kind == "thinking" {
				p["thought"] = true
			}
			return chunk(p)
		}
	case "part_done":
		if slot := e.slots[ev.Index]; slot != nil {
			slot.Part = ev.Part
		}
		if (ev.Part.Kind == "text" || ev.Part.Kind == "thinking") && ev.Part.SealedBy == "gemini" && ev.Part.Signature != "" {
			return chunk(Object{"text": "", "thoughtSignature": ev.Part.Signature})
		}
		if ev.Part.Kind == "image" || ev.Part.Kind == "file" {
			return chunk(geminiPart(ev.Part))
		}
		if ev.Part.Kind == "call" {
			if p := geminiPart(ev.Part); p != nil {
				ps := []Part{}
				for _, i := range e.order {
					slot := e.slots[i]
					if slot.Part.Kind == "thinking" && !slot.Closed && (slot.Part.SealedBy == "messages" || slot.Part.SealedBy == "responses") {
						ps = append(ps, slot.Part)
						slot.Closed = true
					}
				}
				if len(ps) > 0 {
					p["thoughtSignature"] = seal(geminiSeal, Object{"parts": ps})
				}
				return chunk(p)
			}
		}
	case "finish":
		e.finished = true
		if ev.Usage != nil {
			e.usage = ev.Usage
		}
		v := renderGemini(Result{ID: e.id, Stop: ev.Stop, Usage: e.usage}, e.model)
		return e.send("", v)
	}
	return nil
}
