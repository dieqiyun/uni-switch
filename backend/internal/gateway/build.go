package gateway

import (
	"fmt"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/modelmeta"
)

type Model struct {
	ID       string              `json:"id"`
	Enabled  bool                `json:"enabled"`
	Metadata *modelmeta.Resolved `json:"metadata,omitempty"`
}

func (m Model) profile(base string) Object {
	if m.Metadata != nil {
		return m.Metadata.Profile
	}
	return modelmeta.Compatibility(m.ID, base)
}
func imageURL(p Part) string {
	if u := str(p.Image["url"]); u != "" {
		return u
	}
	return "data:" + str(p.Image["media_type"]) + ";base64," + str(p.Image["data"])
}
func imageBlock(p Part) Object {
	u := imageURL(p)
	if rest, ok := strings.CutPrefix(u, "data:"); ok {
		mime, data, ok := strings.Cut(rest, ";base64,")
		if ok {
			return Object{"type": "image", "source": Object{"type": "base64", "media_type": mime, "data": data}}
		}
	}
	return Object{"type": "image", "source": Object{"type": "url", "url": u}}
}
func replayText(model, base string) bool {
	s := strings.ToLower(model + " " + base)
	return strings.Contains(s, "deepseek") || strings.Contains(s, "kimi")
}
func Build(input *Request, to Protocol, model Model, base string) (Object, error) {
	r := *input
	r.Turns = append([]Turn(nil), input.Turns...)
	if r.Source != Responses || to != Responses {
		r.instructions()
	}
	r.pairTools()
	r.Tools = append([]Tool(nil), r.Tools...)
	declared := map[string]bool{}
	for _, t := range r.Tools {
		declared[t.Name] = true
	}
	hadTools := len(r.Tools) > 0
	for _, m := range r.Turns {
		for _, p := range m.Parts {
			if p.Kind == "call" && !declared[p.Name] {
				declared[p.Name] = true
				r.Tools = append(r.Tools, Tool{Name: p.Name, Original: p.Name, Schema: Object{"type": "object", "properties": Object{}}})
			}
		}
	}
	if !hadTools && len(r.Tools) > 0 {
		r.Choice = "none"
	}
	if to == Gemini {
		return buildGemini(&r, model)
	}
	profile := model.profile(base)
	endpointName := string(to)
	if to == Chat {
		endpointName = "chatCompletions"
	}
	if field(profile, "endpoints", endpointName) == false {
		return nil, invalid("模型没有声明该上游接口")
	}
	maxTokens := r.Max
	if maxTokens <= 0 {
		maxTokens = integer(profile["maxOutputTokens"])
		if maxTokens <= 0 {
			maxTokens = 16384
		}
	}
	if cap := integer(profile["maxOutputTokens"]); cap > 0 && maxTokens > cap {
		maxTokens = cap
	}
	out := Object{"model": model.ID, "stream": r.Stream}
	var turns []any
	for _, m := range r.Turns {
		var parts []any
		flush := func() {
			if len(parts) == 0 {
				return
			}
			if to == Messages {
				turns = append(turns, Object{"role": m.Role, "content": parts})
			} else if to == Chat {
				turns = append(turns, Object{"role": m.Role, "content": parts})
			} else {
				turns = append(turns, Object{"type": "message", "role": m.Role, "content": parts})
			}
			parts = nil
		}
		for _, p := range m.Parts {
			switch p.Kind {
			case "text":
				typ := "text"
				if to == Responses {
					typ = "input_text"
					if m.Role == "assistant" {
						typ = "output_text"
					}
				}
				parts = append(parts, Object{"type": typ, "text": p.Text})
			case "image":
				if to == Messages {
					parts = append(parts, imageBlock(p))
				} else if to == Responses {
					parts = append(parts, Object{"type": "input_image", "image_url": imageURL(p), "detail": "auto"})
				} else {
					parts = append(parts, Object{"type": "image_url", "image_url": Object{"url": imageURL(p)}})
				}
			case "file":
				u := str(p.File["url"])
				data := str(p.File["data"])
				mime := str(p.File["media_type"])
				if to == Messages {
					source := Object{"type": "base64", "media_type": mime, "data": data}
					if u != "" {
						source = Object{"type": "url", "url": u}
					}
					parts = append(parts, Object{"type": "document", "source": source})
				} else if to == Responses {
					f := Object{"type": "input_file"}
					if u != "" {
						f["file_url"] = u
					} else {
						f["file_data"] = "data:" + mime + ";base64," + data
						f["filename"] = "attachment"
					}
					parts = append(parts, f)
				} else {
					return nil, invalid("Chat Completions 不支持此文件附件，请选择 Responses 或 Gemini 上游")
				}
			case "thinking":
				if to == Messages {
					if p.SealedBy == "messages" && str(p.Raw["type"]) == "redacted_thinking" {
						parts = append(parts, p.Raw)
					} else if p.SealedBy == "messages" && p.Signature != "" || replayText(model.ID, base) && p.Text != "" {
						parts = append(parts, Object{"type": "thinking", "thinking": p.Text, "signature": p.Signature})
					}
				} else if to == Responses {
					if p.SealedBy == "responses" && str(p.Raw["encrypted_content"]) != "" {
						flush()
						turns = append(turns, p.Raw)
					} else if replayText(model.ID, base) && p.Text != "" {
						flush()
						turns = append(turns, Object{"type": "reasoning", "summary": []any{}, "content": []any{Object{"type": "reasoning_text", "text": p.Text}}})
					}
				} else if to == Chat && replayText(model.ID, base) && p.Text != "" {
					flush()
					turns = append(turns, Object{"role": "assistant", "content": nil, "reasoning_content": p.Text})
				}
			case "call":
				if to == Messages {
					parts = append(parts, Object{"type": "tool_use", "id": safeID(p.ID), "name": p.Name, "input": p.Args})
				} else {
					flush()
					if to == Responses {
						turns = append(turns, Object{"type": "function_call", "call_id": p.ID, "name": p.Name, "arguments": rawArguments(p.Args)})
					} else {
						turns = append(turns, Object{"role": "assistant", "content": nil, "tool_calls": []any{Object{"id": p.ID, "type": "function", "function": Object{"name": p.Name, "arguments": rawArguments(p.Args)}}}})
					}
				}
			case "result":
				var output any = p.Text
				if len(p.Content) > 0 {
					var blocks []any
					for _, part := range p.Content {
						if part.Kind == "image" {
							if to == Messages {
								blocks = append(blocks, imageBlock(part))
							} else if to == Responses {
								blocks = append(blocks, Object{"type": "input_image", "image_url": imageURL(part)})
							} else {
								blocks = append(blocks, Object{"type": "image_url", "image_url": Object{"url": imageURL(part)}})
							}
						} else if part.Kind == "text" {
							typ := "text"
							if to == Responses {
								typ = "input_text"
							}
							blocks = append(blocks, Object{"type": typ, "text": part.Text})
						}
					}
					output = blocks
				}
				if to == Messages {
					parts = append(parts, Object{"type": "tool_result", "tool_use_id": safeID(p.ID), "content": output, "is_error": p.IsError})
				} else {
					flush()
					if to == Responses {
						turns = append(turns, Object{"type": "function_call_output", "call_id": p.ID, "output": output})
					} else {
						turns = append(turns, Object{"role": "tool", "tool_call_id": p.ID, "content": output})
					}
				}
			}
		}
		flush()
	}
	if to == Chat {
		turns = mergeChatAssistant(turns)
		if r.Resume && len(turns) > 0 {
			if mode := prefillMode(base, to, model.ID); mode == "prefix" || mode == "partial" {
				obj(turns[len(turns)-1])[mode] = true
			}
		}
	}
	if turns == nil {
		turns = []any{}
	}
	if to == Responses {
		out["input"] = turns
		out["store"] = false
		out["max_output_tokens"] = maxTokens
		if r.System != "" {
			out["instructions"] = r.System
		}
	} else {
		out["messages"] = turns
		out["max_tokens"] = maxTokens
		if r.System != "" {
			if to == Messages {
				out["system"] = []any{Object{"type": "text", "text": r.System, "cache_control": Object{"type": "ephemeral"}}}
			} else {
				out["messages"] = append([]any{Object{"role": "system", "content": r.System}}, turns...)
			}
		}
	}
	if len(r.Stop) > 0 {
		if to == Responses {
			return nil, invalid("Responses 不支持 stop_sequences，请使用 Chat Completions")
		}
		key := "stop"
		if to == Messages {
			key = "stop_sequences"
		}
		out[key] = r.Stop
	}
	if r.Temp != nil && profile["samplingParameters"] != false {
		out["temperature"] = r.Temp
	}
	if r.TopP != nil && profile["samplingParameters"] != false {
		out["top_p"] = r.TopP
	}
	e := effort(r.Effort)
	if model.Metadata != nil {
		if v := model.Metadata.EffortMapping[e]; v != "" {
			e = effort(v)
		}
	}
	levels := []string{}
	for _, value := range list(profile["reasoningEfforts"]) {
		if level := str(value); level != "ultra" {
			levels = append(levels, level)
		}
	}
	if e != "" {
		e = modelmeta.NearestEffort(e, levels)
	}
	think := (r.Think || e != "") && !r.Off
	if to == Messages {
		if r.Off {
			out["thinking"] = Object{"type": "disabled"}
			if profile["thinkingBetweenTools"] == true {
				out["thinking"] = Object{"type": "between_tools"}
				out["output_config"] = Object{"effort": "low"}
			}
		}
		if think {
			if profile["thinkingFormat"] == "adaptive" {
				out["thinking"] = Object{"type": "adaptive", "display": "summarized"}
				if e != "" {
					out["output_config"] = Object{"effort": e}
				}
			} else if profile["thinkingFormat"] == "deepseek" {
				out["thinking"] = Object{"type": "enabled"}
				if e != "" {
					out["output_config"] = Object{"effort": e}
				}
			} else if r.Max <= 0 || r.Max > 2048 {
				b := budget(e)
				if r.Max > 0 {
					b = min(b, maxTokens-1024)
				} else {
					maxTokens = max(maxTokens, b+4096)
					if cap := integer(profile["maxOutputTokens"]); cap > 0 {
						maxTokens = min(maxTokens, cap)
						b = min(b, maxTokens-1024)
					}
				}
				if b >= 1024 {
					out["thinking"] = Object{"type": "enabled", "budget_tokens": b}
					out["max_tokens"] = maxTokens
				}
			}
		}
		if out["thinking"] != nil {
			delete(out, "temperature")
			delete(out, "top_p")
		}
		if r.Off && profile["thinkingAlwaysOn"] == true && profile["thinkingBetweenTools"] != true {
			delete(out, "thinking")
			out["output_config"] = Object{"effort": "low"}
		}
	} else {
		if r.Off {
			e = "none"
		}
		if e != "" || r.Think {
			if to == Responses {
				reason := Object{"summary": "auto"}
				if e != "" {
					reason["effort"] = e
				}
				out["reasoning"] = reason
				out["include"] = []any{"reasoning.encrypted_content"}
			} else if e != "" {
				out["reasoning_effort"] = e
			}
		}
	}
	if r.WebSearch && to == Responses {
		out["tools"] = []any{Object{"type": "web_search"}}
	}
	if r.WebSearch && to == Messages {
		out["tools"] = []any{Object{"type": "web_search_20250305", "name": "web_search"}}
	}
	if len(r.Tools) > 0 {
		ts := list(out["tools"])
		for _, t := range r.Tools {
			if to == Messages {
				ts = append(ts, Object{"name": t.Name, "description": t.Description, "input_schema": t.Schema})
			} else {
				spec := Object{"name": t.Name, "description": t.Description, "parameters": t.Schema, "strict": t.Strict}
				if to == Responses {
					spec["type"] = "function"
					ts = append(ts, spec)
				} else {
					ts = append(ts, Object{"type": "function", "function": spec})
				}
			}
		}
		out["tools"] = ts
		choice := r.Choice
		name, forced := strings.CutPrefix(choice, "name:")
		if to == Messages {
			tc := Object{}
			switch choice {
			case "none", "auto":
				tc["type"] = choice
			case "required":
				tc["type"] = "any"
			}
			if forced {
				tc["type"] = "tool"
				tc["name"] = name
			}
			if r.Parallel != nil && !*r.Parallel {
				if len(tc) == 0 {
					tc["type"] = "auto"
				}
				tc["disable_parallel_tool_use"] = true
			}
			if len(tc) > 0 {
				out["tool_choice"] = tc
			}
			if forced || choice == "required" {
				if profile["forcedToolChoice"] == false {
					return nil, invalid("模型不支持强制工具选择")
				}
				delete(out, "thinking")
				delete(out, "output_config")
			}
		} else {
			if forced {
				if to == Responses {
					out["tool_choice"] = Object{"type": "function", "name": name}
				} else {
					out["tool_choice"] = Object{"type": "function", "function": Object{"name": name}}
				}
			} else if choice != "" {
				out["tool_choice"] = choice
			}
			if r.Parallel != nil {
				out["parallel_tool_calls"] = *r.Parallel
			}
		}
	}
	if r.Format != nil {
		format := clone(r.Format)
		if format["type"] == "json_schema" {
			if nested := obj(format["json_schema"]); nested != nil {
				format = clone(nested)
				format["type"] = "json_schema"
			}
			if to == Messages {
				delete(format, "name")
				delete(format, "strict")
			} else if to == Chat {
				inner := clone(format)
				delete(inner, "type")
				format = Object{"type": "json_schema", "json_schema": inner}
			} else if str(format["name"]) == "" {
				format["name"] = "response"
			}
		}
		if to == Messages {
			oc := obj(out["output_config"])
			if oc == nil {
				oc = Object{}
			}
			oc["format"] = format
			out["output_config"] = oc
		} else if to == Responses {
			out["text"] = Object{"format": format}
		} else {
			out["response_format"] = format
		}
	}
	// Protocol-owned extension fields are carried only to their owning API.
	if input.Source == to {
		for _, k := range []string{"metadata", "client_metadata", "prompt_cache_key", "service_tier", "text", "safeguards", "access_programs"} {
			if v := input.Original[k]; v != nil && out[k] == nil {
				out[k] = v
			}
		}
	}
	if to == Messages {
		if ms := list(out["messages"]); len(ms) > 0 {
			tail := obj(ms[len(ms)-1])
			ps := list(tail["content"])
			for i := len(ps) - 1; i >= 0; i-- {
				p := obj(ps[i])
				if p["type"] != "thinking" && p["type"] != "redacted_thinking" {
					p["cache_control"] = Object{"type": "ephemeral"}
					break
				}
			}
		}
	}
	if len(encode(out)) > maxBody {
		return nil, fmt.Errorf("上游请求超过大小限制")
	}
	return out, nil
}
