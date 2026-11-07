package gateway

import "strings"

type outputSlot struct {
	Part       Part
	Item       Object
	Index      int
	Summary    bool
	Closed     bool
	Custom     customReader
	CustomText string
}
type Encoder struct {
	started      bool
	waiting      map[int][]Event
	pendingOrder []int
	active       int
	hasActive    bool
	protocol     Protocol
	model        string
	tools        []Tool
	id           string
	sequence     int
	slots        map[int]*outputSlot
	order        []int
	write        func(Frame) error
	finished     bool
	usage        Object
}

func NewEncoder(p Protocol, model string, tools []Tool, write func(Frame) error) *Encoder {
	return &Encoder{protocol: p, model: model, tools: tools, id: uid("resp_"), slots: map[int]*outputSlot{}, write: write, usage: Object{}}
}
func (e *Encoder) send(typ string, v Object) error {
	if e.protocol == Responses {
		v["sequence_number"] = e.sequence
		e.sequence++
	}
	if typ != "" {
		v["type"] = typ
	}
	name := typ
	if e.protocol == Chat {
		name = ""
	}
	return e.write(Frame{name, encode(v)})
}
func (e *Encoder) Start() error {
	if e.protocol == Messages {
		return nil
	}
	return e.begin()
}
func (e *Encoder) begin() error {
	if e.started {
		return nil
	}
	e.started = true
	switch e.protocol {
	case Messages:
		return e.send("message_start", Object{"message": Object{"id": "msg_" + e.id, "type": "message", "role": "assistant", "model": e.model, "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": usageTo(Messages, e.usage)}})
	case Responses:
		r := Render(Result{ID: e.id, Usage: e.usage}, Responses, e.model, nil)
		r["status"] = "in_progress"
		r["usage"] = nil
		if err := e.send("response.created", Object{"response": r}); err != nil {
			return err
		}
		return e.send("response.in_progress", Object{"response": r})
	case Gemini:
		return nil
	case Chat:
		return e.chat(Object{"role": "assistant", "content": ""}, nil, nil)
	}
	return invalid("客户端协议无效")
}
func (e *Encoder) chat(delta Object, stop any, usage any) error {
	return e.send("", Object{"id": e.id, "object": "chat.completion.chunk", "model": e.model, "choices": []any{Object{"index": 0, "delta": delta, "finish_reason": stop}}, "usage": usage})
}

// Messages content blocks must be emitted sequentially even when Chat
// tool deltas are interleaved. Queue only blocks waiting behind the active one.
func (e *Encoder) Accept(ev Event) error {
	if e.protocol == Gemini {
		return e.geminiEvent(ev)
	}
	if e.protocol != Messages {
		return e.accept(ev)
	}
	if !e.started {
		if ev.Kind == "usage" {
			e.usage = ev.Usage
		}
		if err := e.begin(); err != nil {
			return err
		}
	}
	if e.waiting == nil {
		e.waiting = map[int][]Event{}
	}
	if ev.Kind == "part" {
		if !e.hasActive {
			e.active = ev.Index
			e.hasActive = true
		} else {
			e.waiting[ev.Index] = []Event{ev}
			e.pendingOrder = append(e.pendingOrder, ev.Index)
			return nil
		}
	}
	if queue, ok := e.waiting[ev.Index]; ok && (ev.Kind == "delta" || ev.Kind == "part_done") {
		e.waiting[ev.Index] = append(queue, ev)
		return nil
	}
	if err := e.accept(ev); err != nil {
		return err
	}
	if ev.Kind == "part_done" && e.hasActive && e.active == ev.Index {
		e.hasActive = false
		for len(e.pendingOrder) > 0 {
			next := e.pendingOrder[0]
			e.pendingOrder = e.pendingOrder[1:]
			events := e.waiting[next]
			delete(e.waiting, next)
			e.active = next
			e.hasActive = true
			for _, queued := range events {
				if err := e.accept(queued); err != nil {
					return err
				}
			}
			if !e.slots[next].Closed {
				break
			}
			e.hasActive = false
		}
	}
	return nil
}
func (e *Encoder) accept(ev Event) error {
	if e.finished {
		return nil
	}
	if ev.Kind == "usage" {
		e.usage = ev.Usage
		return nil
	}
	if ev.Kind == "part_update" {
		if slot := e.slots[ev.Index]; slot != nil {
			slot.Part = ev.Part
			if e.protocol == Responses {
				item := partOutput(ev.Part, Responses, e.tools)
				if item != nil {
					item["id"] = slot.Item["id"]
					slot.Item = item
				}
			}
		}
		return nil
	}
	if ev.Kind == "part" {
		p := ev.Part
		slot := &outputSlot{Part: p, Index: len(e.order)}
		e.slots[ev.Index] = slot
		e.order = append(e.order, ev.Index)
		if e.protocol == Messages {
			block := partOutput(p, Messages, e.tools)
			if block == nil {
				block = Object{"type": "thinking", "thinking": "", "signature": ""}
			}
			if p.Kind == "text" {
				block["text"] = ""
			}
			if p.Kind == "thinking" && block["type"] != "redacted_thinking" {
				block["thinking"] = ""
				block["signature"] = ""
			}
			if p.Kind == "call" {
				block["input"] = Object{}
			}
			if err := e.send("content_block_start", Object{"index": slot.Index, "content_block": block}); err != nil {
				return err
			}
		} else if e.protocol == Responses {
			item := partOutput(p, Responses, e.tools)
			if item == nil {
				return invalid("无法编码输出内容")
			}
			slot.Item = clone(item)
			item["status"] = "in_progress"
			switch p.Kind {
			case "text":
				item["content"] = []any{}
			case "thinking":
				item["summary"] = []any{}
				delete(item, "encrypted_content")
			case "call":
				if item["type"] == "custom_tool_call" {
					item["input"] = ""
				} else {
					item["arguments"] = ""
				}
			}
			if err := e.send("response.output_item.added", Object{"output_index": slot.Index, "item": item}); err != nil {
				return err
			}
			if p.Kind == "text" {
				if err := e.send("response.content_part.added", Object{"item_id": item["id"], "output_index": slot.Index, "content_index": 0, "part": Object{"type": "output_text", "text": "", "annotations": []any{}}}); err != nil {
					return err
				}
			}
		} else if p.Kind == "call" {
			if err := e.chat(Object{"tool_calls": []any{Object{"index": slot.Index, "id": p.ID, "type": "function", "function": Object{"name": p.Name, "arguments": ""}}}}, nil, nil); err != nil {
				return err
			}
		}
		if p.Text != "" {
			if err := e.delta(ev.Index, "text", p.Text); err != nil {
				return err
			}
		}
		if p.Signature != "" && p.SealedBy == "messages" {
			if err := e.delta(ev.Index, "signature", p.Signature); err != nil {
				return err
			}
		}
		if p.Kind == "call" && p.Args != nil && rawArguments(p.Args) != "{}" {
			return e.delta(ev.Index, "arguments", rawArguments(p.Args))
		}
		return nil
	}
	if ev.Kind == "delta" {
		return e.delta(ev.Index, ev.Field, ev.Text)
	}
	if ev.Kind == "part_done" {
		slot := e.slots[ev.Index]
		if slot == nil {
			return invalid("缺少输出内容")
		}
		if slot.Closed {
			return nil
		}
		slot.Closed = true
		slot.Part = ev.Part
		if e.protocol == Messages {
			return e.send("content_block_stop", Object{"index": slot.Index})
		}
		if e.protocol == Chat {
			return nil
		}
		item := partOutput(ev.Part, Responses, e.tools)
		if item == nil {
			return invalid("无法结束输出内容")
		}
		item["id"] = slot.Item["id"]
		slot.Item = item
		common := Object{"item_id": item["id"], "output_index": slot.Index}
		switch ev.Part.Kind {
		case "text":
			v := clone(common)
			v["content_index"] = 0
			v["text"] = ev.Part.Text
			if err := e.send("response.output_text.done", v); err != nil {
				return err
			}
			v = clone(common)
			v["content_index"] = 0
			v["part"] = list(item["content"])[0]
			if err := e.send("response.content_part.done", v); err != nil {
				return err
			}
		case "thinking":
			if slot.Summary {
				v := clone(common)
				v["summary_index"] = 0
				v["text"] = ev.Part.Text
				if err := e.send("response.reasoning_summary_text.done", v); err != nil {
					return err
				}
				v = clone(common)
				v["summary_index"] = 0
				v["part"] = Object{"type": "summary_text", "text": ev.Part.Text}
				if err := e.send("response.reasoning_summary_part.done", v); err != nil {
					return err
				}
			}
		case "call":
			if item["type"] == "custom_tool_call" {
				v := clone(common)
				full := str(item["input"])
				if !strings.HasPrefix(full, slot.CustomText) {
					return invalid("自由格式工具增量与最终输入不一致")
				}
				rest := strings.TrimPrefix(full, slot.CustomText)
				if rest != "" {
					v["delta"] = rest
					if err := e.send("response.custom_tool_call_input.delta", v); err != nil {
						return err
					}
				}
				v = clone(common)
				v["input"] = item["input"]
				if err := e.send("response.custom_tool_call_input.done", v); err != nil {
					return err
				}
			} else {
				v := clone(common)
				v["arguments"] = item["arguments"]
				if err := e.send("response.function_call_arguments.done", v); err != nil {
					return err
				}
			}
		}
		return e.send("response.output_item.done", Object{"output_index": slot.Index, "item": item})
	}
	if ev.Kind == "finish" {
		e.finished = true
		e.usage = ev.Usage
		if e.protocol == Messages {
			if err := e.send("message_delta", Object{"delta": Object{"stop_reason": stopTo(Messages, ev.Stop), "stop_sequence": nil}, "usage": usageTo(Messages, e.usage)}); err != nil {
				return err
			}
			return e.send("message_stop", Object{})
		}
		if e.protocol == Chat {
			if err := e.chat(Object{}, stopTo(Chat, ev.Stop), usageTo(Chat, e.usage)); err != nil {
				return err
			}
			return e.write(Frame{Data: "[DONE]"})
		}
		parts := []Part{}
		items := []any{}
		for _, i := range e.order {
			s := e.slots[i]
			parts = append(parts, s.Part)
			items = append(items, s.Item)
		}
		r := Render(Result{ID: e.id, Parts: parts, Stop: ev.Stop, Usage: e.usage}, Responses, e.model, e.tools)
		r["output"] = items
		typ := "response.completed"
		if r["status"] == "incomplete" {
			typ = "response.incomplete"
		}
		return e.send(typ, Object{"response": r})
	}
	return nil
}
func (e *Encoder) delta(i int, field, text string) error {
	s := e.slots[i]
	if s == nil || s.Closed {
		return invalid("输出增量没有有效内容")
	}
	if text == "" {
		return nil
	}
	if e.protocol == Messages {
		var d Object
		switch field {
		case "signature":
			if s.Part.SealedBy != "messages" {
				return nil
			}
			d = Object{"type": "signature_delta", "signature": text}
		case "arguments":
			d = Object{"type": "input_json_delta", "partial_json": text}
		case "text":
			if s.Part.Kind == "thinking" {
				d = Object{"type": "thinking_delta", "thinking": text}
			} else {
				d = Object{"type": "text_delta", "text": text}
			}
		}
		return e.send("content_block_delta", Object{"index": s.Index, "delta": d})
	}
	if e.protocol == Chat {
		switch field {
		case "signature":
			return nil
		case "arguments":
			return e.chat(Object{"tool_calls": []any{Object{"index": s.Index, "function": Object{"arguments": text}}}}, nil, nil)
		case "text":
			f := "content"
			if s.Part.Kind == "thinking" {
				f = "reasoning_content"
			}
			return e.chat(Object{f: text}, nil, nil)
		}
	}
	common := Object{"item_id": s.Item["id"], "output_index": s.Index, "delta": text}
	if field == "signature" {
		return nil
	}
	if field == "arguments" {
		if s.Item["type"] == "custom_tool_call" {
			delta := s.Custom.feed(text)
			if delta == "" {
				return nil
			}
			s.CustomText += delta
			common["delta"] = delta
			return e.send("response.custom_tool_call_input.delta", common)
		}
		return e.send("response.function_call_arguments.delta", common)
	}
	if s.Part.Kind == "thinking" {
		if !s.Summary {
			s.Summary = true
			if err := e.send("response.reasoning_summary_part.added", Object{"item_id": s.Item["id"], "output_index": s.Index, "summary_index": 0, "part": Object{"type": "summary_text", "text": ""}}); err != nil {
				return err
			}
		}
		common["summary_index"] = 0
		return e.send("response.reasoning_summary_text.delta", common)
	}
	common["content_index"] = 0
	return e.send("response.output_text.delta", common)
}
func (e *Encoder) Error(message string) error {
	if e.finished {
		return nil
	}
	if e.protocol == Gemini {
		return e.send("", failure(Gemini, 502, message))
	}
	return e.send("error", Object{"error": Object{"type": "api_error", "message": message}})
}
func frameBytes(f Frame) []byte {
	var b strings.Builder
	if f.Name != "" {
		b.WriteString("event: ")
		b.WriteString(f.Name)
		b.WriteByte('\n')
	}
	b.WriteString("data: ")
	b.WriteString(f.Data)
	b.WriteString("\n\n")
	return []byte(b.String())
}
