package gateway

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

func geminiMedia(f Object) Object {
	if u := str(f["url"]); u != "" {
		if rest, ok := strings.CutPrefix(u, "data:"); ok {
			mime, b, ok := strings.Cut(rest, ";base64,")
			if ok {
				return Object{"inlineData": Object{"mimeType": mime, "data": b}}
			}
		}
		return Object{"fileData": Object{"mimeType": f["media_type"], "fileUri": u}}
	}
	return Object{"inlineData": Object{"mimeType": f["media_type"], "data": f["data"]}}
}

// A shared request is built once for both the public Gemini API and the
// subscription Code Assist envelope. No vendor SDK or Magpie runtime is used.
func buildGemini(r *Request, model Model) (Object, error) {
	cfg := clone(r.GenConfig)
	if cfg == nil {
		cfg = Object{}
	}
	if r.Max > 0 {
		cfg["maxOutputTokens"] = r.Max
	}
	if r.Temp != nil {
		cfg["temperature"] = r.Temp
	}
	if r.TopP != nil {
		cfg["topP"] = r.TopP
	}
	if len(r.Stop) > 0 {
		cfg["stopSequences"] = r.Stop
	}
	if r.Format != nil {
		cfg["responseMimeType"] = "application/json"
		format := r.Format
		if n := obj(format["json_schema"]); n != nil {
			format = n
		}
		if schema := format["schema"]; schema != nil {
			cfg["responseJsonSchema"] = schema
		}
	}
	if r.Think || r.Effort != "" || r.Off {
		th := Object{"includeThoughts": true}
		lower := strings.ToLower(model.ID)
		if strings.Contains(lower, "gemini-3") || strings.Contains(lower, "gemini-4") {
			level := effort(r.Effort)
			if level == "" {
				level = "high"
			}
			if r.Off || level == "minimal" || level == "none" {
				level = "low"
			}
			if strings.Contains(lower, "gemini-3") && !strings.Contains(lower, "flash") && level == "medium" {
				level = "high"
			}
			if strings.Contains(lower, "image") && level == "low" {
				level = "high"
			}
			if level == "xhigh" || level == "max" {
				level = "high"
			}
			th["thinkingLevel"] = level
		} else {
			b := budget(r.Effort)
			if r.Off {
				b = 0
				if strings.Contains(lower, "gemini-2.5-pro") {
					b = 128
				}
			}
			th["thinkingBudget"] = b
		}
		cfg["thinkingConfig"] = th
	}
	out := Object{"model": model.ID, "stream": r.Stream, "generationConfig": cfg}
	if r.System != "" {
		out["systemInstruction"] = Object{"parts": []any{Object{"text": r.System}}}
	}
	names := map[string]string{}
	for _, m := range r.Turns {
		for _, p := range m.Parts {
			if p.Kind == "call" {
				names[p.ID] = p.Name
			}
		}
	}
	contents := []any{}
	for _, m := range r.Turns {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		parts := []any{}
		signedStep := false
		for _, p := range m.Parts {
			signedStep = signedStep || p.Kind == "call" && p.SealedBy == "gemini" && p.Signature != ""
		}
		for _, p := range m.Parts {
			switch p.Kind {
			case "text", "image", "file", "call":
				if v := geminiPart(p); v != nil {
					if p.Kind == "call" && !signedStep {
						v["thoughtSignature"] = "skip_thought_signature_validator"
					}
					parts = append(parts, v)
				}
			case "thinking":
				if p.SealedBy == "gemini" {
					if v := geminiPart(p); v != nil {
						parts = append(parts, v)
					}
				}
			case "result":
				name := p.Name
				if name == "" {
					name = names[p.ID]
				}
				response := obj(p.Raw["response"])
				if response == nil {
					var v any
					if decode([]byte(p.Text), &v) == nil {
						response = obj(v)
					}
					if response == nil {
						response = Object{"result": p.Text}
					}
				}
				res := Object{"id": p.ID, "name": name, "response": response}
				media := []any{}
				for _, x := range p.Content {
					if x.Kind == "image" {
						media = append(media, geminiMedia(x.Image))
					} else if x.Kind == "file" {
						media = append(media, geminiMedia(x.File))
					}
				}
				if len(media) > 0 {
					res["parts"] = media
				}
				parts = append(parts, Object{"functionResponse": res})
			}
		}
		if len(parts) > 0 {
			contents = append(contents, Object{"role": role, "parts": parts})
		}
	}
	if len(contents) > 0 && field(contents[len(contents)-1], "role") == "model" {
		contents = append(contents, Object{"role": "user", "parts": []any{Object{"text": r.modelLastAsk()}}})
	}
	out["contents"] = contents
	tools := append([]any(nil), r.HostedTools...)
	if len(r.Tools) > 0 {
		declarations := []any{}
		for _, t := range r.Tools {
			declarations = append(declarations, Object{"name": t.Name, "description": t.Description, "parametersJsonSchema": geminiInputSchema(t.Schema)})
		}
		tools = append(tools, Object{"functionDeclarations": declarations})
		tc := Object{"mode": "AUTO"}
		switch r.Choice {
		case "none":
			tc["mode"] = "NONE"
		case "required":
			tc["mode"] = "ANY"
		}
		if name, ok := strings.CutPrefix(r.Choice, "name:"); ok {
			tc["mode"] = "ANY"
			tc["allowedFunctionNames"] = []any{name}
		}
		out["toolConfig"] = Object{"functionCallingConfig": tc}
	}
	if r.WebSearch && len(r.HostedTools) == 0 {
		tools = append(tools, Object{"googleSearch": Object{}})
	}
	if len(tools) > 0 {
		out["tools"] = tools
	}
	return out, nil
}

// Infer unambiguous missing types; preserve property names and union schemas.
func geminiInputSchema(v any) any {
	if b, ok := v.(bool); ok {
		if b {
			return Object{}
		}
		return Object{"not": Object{}}
	}
	if a := list(v); a != nil {
		out := []any{}
		for _, x := range a {
			out = append(out, geminiInputSchema(x))
		}
		return out
	}
	m := obj(v)
	if m == nil {
		return v
	}
	out := clone(m)
	for _, k := range []string{"properties", "$defs", "definitions"} {
		if props := obj(out[k]); props != nil {
			for name, s := range props {
				props[name] = geminiInputSchema(s)
			}
		}
	}
	for _, k := range []string{"items", "additionalProperties", "anyOf", "oneOf", "allOf", "not"} {
		if x, ok := out[k]; ok {
			out[k] = geminiInputSchema(x)
		}
	}
	if out["type"] == nil && out["anyOf"] == nil && out["oneOf"] == nil && out["allOf"] == nil && out["$ref"] == nil {
		if out["properties"] != nil || out["required"] != nil {
			out["type"] = "object"
		} else if out["items"] != nil {
			out["type"] = "array"
		} else {
			values := list(out["enum"])
			if value, ok := out["const"]; ok {
				values = []any{value}
			}
			if len(values) == 0 && out["default"] != nil {
				values = []any{out["default"]}
			}
			typ := ""
			for _, value := range values {
				next := ""
				switch value.(type) {
				case string:
					next = "string"
				case bool:
					next = "boolean"
				case float64, int, int64, json.Number:
					next = "number"
				case []any:
					next = "array"
				case map[string]any:
					next = "object"
				case nil:
					continue
				}
				if typ != "" && typ != next {
					typ = ""
					break
				}
				typ = next
			}
			if typ != "" {
				out["type"] = typ
			}
		}
	}
	return out
}
func decodeGemini(v Object) (Result, error) {
	if wrapped := obj(v["response"]); wrapped != nil {
		v = wrapped
	}
	r := Result{ID: str(v["responseId"]), Model: str(v["modelVersion"]), Stop: "stop", Usage: geminiUsage(obj(v["usageMetadata"]))}
	if v["error"] != nil {
		return r, errors.New("Gemini 上游返回错误")
	}
	cs := list(v["candidates"])
	if len(cs) == 0 {
		if field(v, "promptFeedback", "blockReason") != nil {
			r.Stop = "filter"
			return r, nil
		}
		return r, errors.New("Gemini 回复缺少 candidates")
	}
	c := obj(cs[0])
	reason := str(c["finishReason"])
	switch reason {
	case "MAX_TOKENS":
		r.Stop = "length"
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY":
		r.Stop = "filter"
	case "MALFORMED_FUNCTION_CALL", "UNEXPECTED_TOOL_CALL":
		return r, errors.New("Gemini 返回无效工具调用")
	}
	for _, v := range list(field(c, "content", "parts")) {
		p := obj(v)
		sig := str(p["thoughtSignature"])
		var part Part
		switch {
		case p["text"] != nil:
			k := "text"
			if p["thought"] == true {
				k = "thinking"
			}
			part = Part{Kind: k, Text: str(p["text"]), Signature: sig, SealedBy: "gemini"}
		case p["functionCall"] != nil:
			call := obj(p["functionCall"])
			id := str(call["id"])
			if id == "" {
				id = uid("call_")
			}
			part = Part{Kind: "call", ID: id, Name: str(call["name"]), Args: call["args"], Signature: sig, SealedBy: "gemini"}
			if part.Name == "" {
				return r, errors.New("Gemini 工具调用缺少名称")
			}
			r.Stop = "tool"
		case p["inlineData"] != nil || p["fileData"] != nil:
			part = geminiAttachment(p)
		default:
			if sig != "" {
				part = Part{Kind: "thinking", Signature: sig, SealedBy: "gemini"}
			} else {
				continue
			}
		}
		r.Parts = append(r.Parts, part)
	}
	if reason == "" {
		return r, errors.New("Gemini 回复缺少完成状态")
	}
	return r, nil
}
func geminiUsage(u Object) Object {
	thought := integer(u["thoughtsTokenCount"])
	return Object{"input_tokens": integer(u["promptTokenCount"]), "output_tokens": integer(u["candidatesTokenCount"]) + thought, "reasoning_tokens": thought, "cached_tokens": integer(u["cachedContentTokenCount"])}
}
func decodeGeminiStream(reader io.Reader, emit func(Event) error) error {
	d := streamDecoder{proto: Gemini, parts: map[int]*streamPart{}, usage: Object{}, emit: emit}
	finished := false
	err := ReadSSE(reader, func(f Frame) error {
		var v Object
		if decode([]byte(f.Data), &v) != nil {
			return errors.New("Gemini SSE 事件不是 JSON")
		}
		if o := obj(v["response"]); o != nil {
			v = o
		}
		if v["error"] != nil {
			return errors.New("Gemini 流返回错误")
		}
		if u := obj(v["usageMetadata"]); u != nil {
			d.usage = geminiUsage(u)
		}
		cs := list(v["candidates"])
		if len(cs) == 0 {
			if field(v, "promptFeedback", "blockReason") != nil {
				d.stop = "filter"
				finished = true
			}
			return nil
		}
		c := obj(cs[0])
		for _, x := range list(field(c, "content", "parts")) {
			raw := obj(x)
			probe := Object{"candidates": []any{Object{"content": Object{"parts": []any{raw}}, "finishReason": "STOP"}}}
			result, e := decodeGemini(probe)
			if e != nil {
				return e
			}
			for _, p := range result.Parts {
				// Text deltas of the same kind share an output block. Calls and media are
				// complete parts in Gemini, so their arguments are never concatenated.
				i := -1
				if len(d.order) > 0 {
					last := d.order[len(d.order)-1]
					prev := d.parts[last]
					if !prev.Closed && (p.Kind == "text" || p.Kind == "thinking") && prev.Part.Kind == p.Kind {
						i = last
					}
				}
				if i < 0 {
					for _, j := range d.order {
						if !d.parts[j].Closed {
							if e = d.close(j); e != nil {
								return e
							}
						}
					}
					i = len(d.order)
					initial := p
					if p.Kind == "text" || p.Kind == "thinking" {
						initial.Text = ""
						initial.Signature = ""
					}
					if e = d.start(i, initial); e != nil {
						return e
					}
				}
				if p.Kind == "text" || p.Kind == "thinking" {
					if p.Text != "" {
						if e = d.delta(i, "text", p.Text); e != nil {
							return e
						}
					}
					if p.Signature != "" {
						d.parts[i].Part.Signature = p.Signature
						if e = d.emit(Event{Kind: "part_update", Index: i, Part: d.parts[i].Part}); e != nil {
							return e
						}
					}
				} else {
					if p.Kind == "call" {
						d.stop = "tool"
					}
					if e = d.close(i); e != nil {
						return e
					}
				}
			}
		}
		if reason := str(c["finishReason"]); reason != "" {
			finished = true
			switch reason {
			case "MAX_TOKENS":
				d.stop = "length"
			case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY":
				d.stop = "filter"
			case "MALFORMED_FUNCTION_CALL", "UNEXPECTED_TOOL_CALL":
				return errors.New("Gemini 返回无效工具调用")
			default:
				if d.stop == "" {
					d.stop = "stop"
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !finished {
		return errTruncated
	}
	return d.finish(d.stop)
}
