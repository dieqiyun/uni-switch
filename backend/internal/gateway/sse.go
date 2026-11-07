package gateway

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Frame struct{ Name, Data string }

// ReadSSE frames bytes before decoding UTF-8. Network boundaries never become
// event boundaries. Like Magpie's readSSE, a final data event is flushed at EOF;
// the protocol decoder separately decides whether the reply completed.
func ReadSSE(r io.Reader, receive func(Frame) error) error {
	return readRawSSE(r, func(frame Frame, raw []byte) error {
		if frame.Data == "" {
			hasData := false
			for len(raw) > 0 {
				n, line, _ := sseLine(raw, true)
				raw = raw[n:]
				line = bytes.TrimRight(line, "\r\n")
				hasData = hasData || bytes.HasPrefix(line, []byte("data:")) || bytes.Equal(line, []byte("data"))
			}
			if !hasData {
				return nil
			}
		}
		return receive(frame)
	})
}

// Keep the raw event available to same-protocol relay. Only events requiring
// an ID repair are reconstructed; comments and extension fields survive.
func readRawSSE(r io.Reader, receive func(Frame, []byte) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	sc.Split(sseLine)
	name := ""
	var data []string
	var raw bytes.Buffer
	size := 0
	first := true
	flush := func() error {
		if raw.Len() == 0 && len(data) == 0 {
			return nil
		}
		f := Frame{name, strings.Join(data, "\n")}
		err := receive(f, raw.Bytes())
		name = ""
		data = nil
		size = 0
		raw.Reset()
		return err
	}
	for sc.Scan() {
		b := sc.Bytes()
		raw.Write(b)
		if raw.Len() > 4<<20 {
			return errors.New("SSE 事件超过 4 MiB")
		}
		b = bytes.TrimSuffix(bytes.TrimSuffix(b, []byte{'\n'}), []byte{'\r'})
		if first {
			b = bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})
			first = false
		}
		if !utf8.Valid(b) {
			return errors.New("SSE 含无效 UTF-8")
		}
		s := string(b)
		if s == "" {
			if e := flush(); e != nil {
				return e
			}
			continue
		}
		if strings.HasPrefix(s, ":") {
			if len(data) == 0 && name == "" {
				if err := flush(); err != nil {
					return err
				}
			}
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(s), "{") {
			var v Object
			if decode([]byte(s), &v) == nil && (v["error"] != nil || v["type"] == "error") {
				return errors.New("上游在 SSE 流中返回 JSON 错误")
			}
		}
		key, value, _ := strings.Cut(s, ":")
		value = strings.TrimPrefix(value, " ")
		switch key {
		case "event":
			name = value
		case "data":
			size += len(value) + 1
			if size > 4<<20 {
				return errors.New("SSE 事件超过 4 MiB")
			}
			data = append(data, value)
		}
	}
	if err := flush(); err != nil {
		return err
	}
	return sc.Err()
}

func sseLine(b []byte, eof bool) (int, []byte, error) {
	for i, c := range b {
		if c != '\r' && c != '\n' {
			continue
		}
		if c == '\r' && i+1 == len(b) && !eof {
			return 0, nil, nil
		}
		n := i + 1
		if c == '\r' && n < len(b) && b[n] == '\n' {
			n++
		}
		return n, b[:n], nil
	}
	if eof && len(b) > 0 {
		return len(b), b, nil
	}
	return 0, nil, nil
}

type Event struct {
	Kind, Field, Text string
	Index             int
	Part              Part
	Usage             Object
	Stop              string
}
type streamPart struct {
	Part            Part
	Args            strings.Builder
	Closed, Started bool
	Custom          bool
}
type streamDecoder struct {
	proto     Protocol
	parts     map[int]*streamPart
	order     []int
	stop      string
	finished  bool
	choice    string
	choiceSet bool
	usage     Object
	emit      func(Event) error
	chatCalls map[int]int
	nextCall  int
}

func (d *streamDecoder) start(i int, p Part) error {
	if _, ok := d.parts[i]; ok {
		return errors.New("上游重复内容索引")
	}
	s := &streamPart{Part: p}
	d.parts[i] = s
	d.order = append(d.order, i)
	return d.offer(i)
}
func (d *streamDecoder) offer(i int) error {
	s := d.parts[i]
	if s.Started {
		return nil
	}
	if s.Part.Kind == "call" && (s.Part.ID == "" || s.Part.Name == "") {
		return nil
	}
	s.Started = true
	if e := d.emit(Event{Kind: "part", Index: i, Part: s.Part}); e != nil {
		return e
	}
	if s.Args.Len() > 0 {
		return d.emit(Event{Kind: "delta", Index: i, Field: "arguments", Text: s.Args.String()})
	}
	return nil
}
func (d *streamDecoder) delta(i int, field, text string) error {
	s := d.parts[i]
	if s == nil || s.Closed {
		return errors.New("上游增量没有有效的内容开始")
	}
	switch field {
	case "text":
		s.Part.Text += text
	case "signature":
		s.Part.Signature += text
	case "arguments":
		s.Args.WriteString(text)
	}
	if s.Args.Len()+len(s.Part.Text)+len(s.Part.Signature) > maxBody {
		return errors.New("上游内容超过大小限制")
	}
	if s.Started {
		return d.emit(Event{Kind: "delta", Index: i, Field: field, Text: text})
	}
	return nil
}
func (d *streamDecoder) close(i int) error {
	s := d.parts[i]
	if s == nil {
		return errors.New("上游结束了不存在的内容")
	}
	if s.Closed {
		return nil
	}
	if e := d.offer(i); e != nil {
		return e
	}
	if !s.Started {
		if d.proto == Chat && s.Part.Kind == "call" && s.Part.Name == "" {
			s.Closed = true
			return nil
		}
		return errors.New("上游工具调用缺少 ID 或名称")
	}
	if s.Part.Kind == "call" {
		if s.Args.Len() > 0 {
			if decode([]byte(s.Args.String()), &s.Part.Args) != nil {
				return errors.New("上游工具参数不完整或不是 JSON")
			}
			s.Part.Args = json.RawMessage(s.Args.String())
		} else if s.Part.Args == nil {
			s.Part.Args = Object{}
		}
	}
	s.Closed = true
	return d.emit(Event{Kind: "part_done", Index: i, Part: s.Part})
}
func (d *streamDecoder) finish(stop string) error {
	if d.finished {
		return nil
	}
	for _, i := range d.order {
		if e := d.close(i); e != nil {
			return e
		}
	}
	if d.proto == Chat && stop == "tool" {
		named := false
		for _, part := range d.parts {
			named = named || part.Part.Kind == "call" && part.Started
		}
		if !named {
			stop = "stop"
		}
	}
	d.finished = true
	return d.emit(Event{Kind: "finish", Stop: stop, Usage: d.usage})
}
func (d *streamDecoder) messages(v Object) error {
	i := integer(v["index"])
	switch str(v["type"]) {
	case "message_start":
		d.usage = usageFrom(Messages, obj(field(v, "message", "usage")))
		return d.emit(Event{Kind: "usage", Usage: d.usage})
	case "content_block_start":
		ps, e := content([]any{v["content_block"]}, Messages)
		if e != nil {
			return e
		}
		if len(ps) == 0 {
			return nil
		}
		return d.start(i, ps[0])
	case "content_block_delta":
		delta := obj(v["delta"])
		switch str(delta["type"]) {
		case "text_delta":
			return d.delta(i, "text", str(delta["text"]))
		case "thinking_delta":
			return d.delta(i, "text", str(delta["thinking"]))
		case "signature_delta":
			return d.delta(i, "signature", str(delta["signature"]))
		case "input_json_delta":
			return d.delta(i, "arguments", str(delta["partial_json"]))
		default:
			return errors.New("不支持的 Messages 增量类型")
		}
	case "content_block_stop":
		return d.close(i)
	case "message_delta":
		if s := str(field(v, "delta", "stop_reason")); s != "" {
			d.stop = stopFrom(Messages, s)
		}
		u := obj(v["usage"])
		if n, ok := u["output_tokens"]; ok {
			d.usage["output_tokens"] = n
		}
		return d.emit(Event{Kind: "usage", Usage: d.usage})
	case "message_stop":
		if d.stop == "" {
			return errTruncated
		}
		for _, p := range d.parts {
			if !p.Closed {
				return errTruncated
			}
		}
		return d.finish(d.stop)
	case "ping":
		return nil
	case "error":
		return errors.New("Messages 上游返回流式错误")
	}
	return nil // Unknown non-content bookkeeping events carry no semantic delta.
}
func responsePart(v Object) Part {
	switch str(v["type"]) {
	case "message":
		return Part{Kind: "text"}
	case "function_call":
		return Part{Kind: "call", ID: str(v["call_id"]), Name: str(v["name"])}
	case "custom_tool_call":
		return Part{Kind: "call", ID: str(v["call_id"]), Name: str(v["name"])}
	case "reasoning":
		return Part{Kind: "thinking", Raw: v, SealedBy: "responses"}
	}
	return Part{}
}
func (d *streamDecoder) responses(v Object) error {
	i := integer(v["output_index"])
	typ := str(v["type"])
	switch typ {
	case "response.created", "response.in_progress", "response.queued", "response.content_part.added", "response.content_part.done", "response.reasoning_summary_part.added", "response.reasoning_summary_part.done":
		return nil
	case "response.output_item.added":
		p := responsePart(obj(v["item"]))
		if p.Kind == "" {
			return errors.New("不支持的 Responses 输出类型")
		}
		if e := d.start(i, p); e != nil {
			return e
		}
		d.parts[i].Custom = field(v, "item", "type") == "custom_tool_call"
		return nil
	case "response.output_text.delta", "response.reasoning_summary_text.delta", "response.reasoning_text.delta", "response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
		if d.parts[i] == nil {
			kind := "text"
			if strings.Contains(typ, "reasoning") {
				kind = "thinking"
			}
			if strings.Contains(typ, "call") {
				return errors.New("工具增量缺少输出条目")
			}
			if e := d.start(i, Part{Kind: kind}); e != nil {
				return e
			}
		}
		f := "text"
		if typ == "response.function_call_arguments.delta" {
			f = "arguments"
		}
		if typ == "response.custom_tool_call_input.delta" {
			return d.customDelta(i, str(v["delta"]))
		}
		return d.delta(i, f, str(v["delta"]))
	case "response.output_item.done":
		it := obj(v["item"])
		p := responsePart(it)
		if d.parts[i] == nil {
			if e := d.start(i, p); e != nil {
				return e
			}
			d.parts[i].Custom = it["type"] == "custom_tool_call"
		}
		s := d.parts[i]
		if s.Closed {
			if s.Part.Kind == "thinking" {
				s.Part.Raw = it
				s.Part.SealedBy = "responses"
				return d.emit(Event{Kind: "part_update", Index: i, Part: s.Part})
			}
			return nil
		}
		if s.Part.Kind == "call" {
			if s.Part.ID == "" {
				s.Part.ID = str(it["call_id"])
			}
			if s.Part.Name == "" {
				s.Part.Name = str(it["name"])
			}
			args := str(it["arguments"])
			if s.Custom {
				if s.Args.Len() == 0 {
					if e := d.customDelta(i, str(it["input"])); e != nil {
						return e
					}
				}
				if e := d.delta(i, "arguments", `"}`); e != nil {
					return e
				}
				args = ""
			}
			if args != "" && s.Args.Len() == 0 {
				if e := d.delta(i, "arguments", args); e != nil {
					return e
				}
			}
		} else if s.Part.Kind == "thinking" {
			s.Part.Raw = it
			s.Part.SealedBy = "responses"
		}
		if s.Part.Text == "" {
			var ps []Part
			if p.Kind == "text" {
				ps, _ = content(it["content"], Responses)
			} else if p.Kind == "thinking" {
				ps, _ = content(it["content"], Responses)
				if allText(ps) == "" {
					ps, _ = content(it["summary"], Responses)
				}
			}
			if t := allText(ps); t != "" {
				if e := d.delta(i, "text", t); e != nil {
					return e
				}
			}
		}
		return d.close(i)
	case "response.completed", "response.incomplete":
		response := obj(v["response"])
		d.usage = usageFrom(Responses, obj(response["usage"]))
		// The terminal response is authoritative even if the relay omitted
		// output_item.done. Complete every item, including opaque reasoning.
		for i, it := range list(response["output"]) {
			if e := d.responses(Object{"type": "response.output_item.done", "output_index": i, "item": it}); e != nil {
				return e
			}
		}
		stop := "stop"
		if typ == "response.incomplete" {
			stop = stopFrom(Responses, str(field(response, "incomplete_details", "reason")))
		}
		for _, p := range d.parts {
			if p.Part.Kind == "call" {
				stop = "tool"
			}
		}
		return d.finish(stop)
	case "response.failed", "error":
		return errors.New("Responses 上游返回流式错误")
	case "response.output_text.done", "response.reasoning_summary_text.done", "response.function_call_arguments.done", "response.custom_tool_call_input.done":
		return nil
	}
	return nil
}

// A custom tool's free text becomes an incrementally escaped JSON string in
// the shared function-call representation. Never wait for the final input.
func (d *streamDecoder) customDelta(i int, text string) error {
	if d.parts[i].Args.Len() == 0 {
		if err := d.delta(i, "arguments", `{"input":"`); err != nil {
			return err
		}
	}
	quoted := encode(text)
	return d.delta(i, "arguments", quoted[1:len(quoted)-1])
}

func (d *streamDecoder) chat(v Object) error {
	if v["error"] != nil {
		return errors.New("Chat Completions 上游返回流式错误")
	}
	if u := obj(v["usage"]); u != nil {
		d.usage = usageFrom(Chat, u)
	}
	for _, value := range list(v["choices"]) {
		c := obj(value)
		if idx, ok := c["index"]; ok && idx != nil {
			key := str(idx)
			if key == "" {
				key = encode(idx)
			}
			if n, e := strconv.ParseFloat(key, 64); e == nil {
				key = strconv.FormatFloat(n, 'g', -1, 64)
			}
			if !d.choiceSet {
				d.choiceSet = true
				d.choice = key
			}
			if key != d.choice {
				continue
			}
		}
		delta := obj(c["delta"])
		if str(delta["reasoning_content"]) != "" {
			delete(delta, "reasoning")
		}
		for _, f := range []string{"reasoning_content", "reasoning", "content"} {
			t := str(delta[f])
			if t == "" {
				continue
			}
			i, kind := 0, "text"
			if f != "content" {
				i, kind = 1, "thinking"
			}
			if d.parts[i] == nil {
				if e := d.start(i, Part{Kind: kind}); e != nil {
					return e
				}
			}
			if e := d.delta(i, "text", t); e != nil {
				return e
			}
		}
		for at, v := range list(delta["tool_calls"]) {
			t := obj(v)
			index := at
			if value, ok := t["index"]; ok {
				index = integer(value)
			}
			if d.chatCalls == nil {
				d.chatCalls = map[int]int{}
				d.nextCall = 2
			}
			i, exists := d.chatCalls[index]
			id := str(t["id"])
			if exists && id != "" && d.parts[i].Part.ID != id {
				if e := d.close(i); e != nil {
					return e
				}
				exists = false
			}
			if !exists {
				i = d.nextCall
				d.nextCall++
				d.chatCalls[index] = i
				if id == "" {
					id = uid("call_")
				}
				if e := d.start(i, Part{Kind: "call", ID: id}); e != nil {
					return e
				}
			}
			p := d.parts[i]
			if p.Part.Name == "" {
				p.Part.Name = str(field(t, "function", "name"))
			}
			if e := d.offer(i); e != nil {
				return e
			}
			if a := str(field(t, "function", "arguments")); a != "" {
				if e := d.delta(i, "arguments", a); e != nil {
					return e
				}
			}
		}
		if f := str(c["finish_reason"]); f != "" {
			d.stop = stopFrom(Chat, f)
		}
	}
	return nil
}
func DecodeStream(p Protocol, r io.Reader, emit func(Event) error) error {
	if p == Gemini {
		return decodeGeminiStream(r, emit)
	}
	d := streamDecoder{proto: p, parts: map[int]*streamPart{}, usage: Object{}, emit: emit}
	errFinished := errors.New("gateway stream completed")
	e := ReadSSE(r, func(f Frame) (err error) {
		defer func() {
			if err == nil && d.finished {
				err = errFinished
			}
		}()
		if d.finished {
			return nil
		}
		if strings.TrimSpace(f.Data) == "[DONE]" {
			if p != Chat || d.stop == "" {
				return errTruncated
			}
			return d.finish(d.stop)
		}
		var v Object
		if decode([]byte(f.Data), &v) != nil {
			return errors.New("SSE 事件不是 JSON")
		}
		if v["type"] == nil && f.Name != "" {
			v["type"] = f.Name
		}
		if v["error"] != nil || v["type"] == "error" {
			return errors.New("上游返回流式错误")
		}
		switch p {
		case Messages:
			return d.messages(v)
		case Responses:
			return d.responses(v)
		case Chat:
			return d.chat(v)
		}
		return errors.New("无效流式协议")
	})
	if errors.Is(e, errFinished) {
		return nil
	}
	if e != nil {
		return e
	}
	if p == Chat && !d.finished && d.stop != "" {
		return d.finish(d.stop)
	}
	if !d.finished {
		return errTruncated
	}
	return nil
}
