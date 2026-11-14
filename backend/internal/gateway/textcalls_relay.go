package gateway

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

// responsesTextCalls does what textCallTidy does in a Responses reply
// relayed as it came (passthrough): a Responses client (Codex) on a
// provider that speaks Responses itself, whose model wrote its calls into
// its text (#4743: it reached the agent as text, and the turn ended with
// nothing run). Only a translated reply was read for them before. Text
// from where a call begins is held; the message item's output_text.done,
// content_part.done and output_item.done wait with it; at the reply's end
// the held calls become function_call items after it, in the events and
// in response.completed's output, which must match. Held text that isn't
// calls goes on as it came. A reply that came whole is read as
// wholeTextCalls reads a Chat one.

// geminiToolNames are the function tools of a Gemini request.
func geminiToolNames(body []byte) map[string]bool {
	var q struct {
		Tools []struct {
			FunctionDeclarations []struct {
				Name string `json:"name"`
			} `json:"functionDeclarations"`
		} `json:"tools"`
	}
	if decodeJSON(body, &q) != nil || len(q.Tools) == 0 {
		return nil
	}
	m := map[string]bool{}
	for _, t := range q.Tools {
		for _, d := range t.FunctionDeclarations {
			if d.Name != "" {
				m[d.Name] = true
			}
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

type responsesTextCalls struct {
	names map[string]bool
	sse   bool
	buf   []byte
	seq   int // the last sequence_number sent on, -1 before the first
	// numbers: the stream numbers its events, so the reply's own are too
	numbers bool

	pending string
	held    string
	holding bool
	native  bool // the reply made a call itself

	itemID   string // the held text's item, as its deltas name it
	itemIdx  int    // its output_index
	partIdx  int    // its content_index
	top      int    // one past the highest output_index seen
	textDone []byte // its output_text.done, held
	partDone []byte // its content_part.done, held
	itemDone []byte // its output_item.done, held
}

func (t *responsesTextCalls) write(b []byte) []byte {
	t.buf = append(t.buf, b...)
	if !t.sse {
		return nil
	}
	out, rest := tidyEvents(t.buf, t.event)
	t.buf = rest
	return t.numbered(out)
}

func (t *responsesTextCalls) flush() []byte {
	b := t.buf
	t.buf = nil
	if !t.sse {
		return t.whole(b)
	}
	var out []byte
	if len(b) > 0 {
		out = t.event(b)
	}
	return t.numbered(append(out, t.release()...))
}

// numbered is out with no event's sequence_number at or below the last
// one sent, which a Responses client takes as the order: the calls made
// of the text are events of the reply's own, and the done events held for
// them go after them, so the upstream's numbers would go back or repeat.
// Such an event goes as one past the last sent; one already past it keeps
// its number, so a stream nothing was made in goes on as it came.
func (t *responsesTextCalls) numbered(out []byte) []byte {
	if len(out) == 0 {
		return out
	}
	var res []byte
	for rest := out; len(rest) > 0; {
		n := eventEnd(rest)
		if n < 0 {
			n = len(rest)
		}
		ev := rest[:n]
		rest = rest[n:]
		data := eventData(ev)
		sq := gjson.GetBytes(data, "sequence_number")
		if sq.Type != gjson.Number || sq.Index <= 0 {
			res = append(res, ev...)
			continue
		}
		want := int(sq.Int())
		if t.seq >= 0 && want <= t.seq {
			want = t.seq + 1
		}
		t.seq = want
		if int(sq.Int()) == want {
			res = append(res, ev...)
			continue
		}
		nd := append(append(append([]byte(nil), data[:sq.Index]...), strconv.Itoa(want)...), data[sq.Index+len(sq.Raw):]...)
		res = append(res, withData(ev, nd)...)
	}
	return res
}

// reData is ev with its data given again as v said, nil when it can't be
// said.
func reData(ev []byte, v any) []byte {
	nb, err := marshalPlain(v)
	if err != nil {
		return nil
	}
	return withData(ev, nb)
}

// send is one event of the reply's own, numbered on from the last one
// sent (numbered) when the stream numbers its events.
func (t *responsesTextCalls) send(typ string, fields map[string]any) []byte {
	if t.numbers {
		fields["sequence_number"] = t.seq + 1
	}
	fields["type"] = typ
	b, err := marshalPlain(fields)
	if err != nil {
		return nil
	}
	return []byte("event: " + typ + "\ndata: " + string(b) + "\n\n")
}

// textDelta is the held text's part given another delta of text.
func (t *responsesTextCalls) textDelta(s string) []byte {
	if s == "" {
		return nil
	}
	return t.send("response.output_text.delta", map[string]any{"item_id": t.itemID,
		"output_index": t.itemIdx, "content_index": t.partIdx, "delta": s, "logprobs": []any{}})
}

// release hands back what is held: the text as a delta of its own, the
// part's and the item's done events as they came.
func (t *responsesTextCalls) release() []byte {
	out := t.textDelta(t.pending + t.held)
	t.pending, t.held, t.holding = "", "", false
	out = append(out, t.textDone...)
	out = append(out, t.partDone...)
	out = append(out, t.itemDone...)
	t.textDone, t.partDone, t.itemDone = nil, nil, nil
	return out
}

// doneText rewrites the text of the held done events to s.
func (t *responsesTextCalls) doneText(s string) []byte {
	var out []byte
	for _, ev := range [][]byte{t.textDone, t.partDone} {
		if ev == nil {
			continue
		}
		var e map[string]any
		ne := ev
		if decodeNumbers(eventData(ev), &e) == nil {
			if _, ok := e["part"]; ok {
				if part, _ := e["part"].(map[string]any); part != nil {
					part["text"] = s
				}
			} else {
				e["text"] = s
			}
			if nb := reData(ev, e); nb != nil {
				ne = nb
			}
		}
		out = append(out, ne...)
	}
	return out
}

// itemText rewrites the held message item's part's text to s.
func (t *responsesTextCalls) itemText(s string) []byte {
	if t.itemDone == nil {
		return nil
	}
	var e map[string]any
	ne := t.itemDone
	if decodeNumbers(eventData(t.itemDone), &e) == nil {
		if item, _ := e["item"].(map[string]any); item != nil {
			if content, _ := item["content"].([]any); len(content) > t.partIdx {
				if part, _ := content[t.partIdx].(map[string]any); part != nil && part["type"] == "output_text" {
					part["text"] = s
					if nb := reData(t.itemDone, e); nb != nil {
						ne = nb
					}
				}
			}
		}
	}
	return ne
}

// event is one whole event of the stream, given on, held or made into
// others.
func (t *responsesTextCalls) event(ev []byte) []byte {
	data := eventData(ev)
	typ := gjson.GetBytes(data, "type").String()
	if !t.numbers && gjson.GetBytes(data, "sequence_number").Exists() {
		t.numbers = true
	}
	if oi := gjson.GetBytes(data, "output_index"); oi.Exists() {
		t.top = max(t.top, int(oi.Int())+1)
	}
	switch typ {
	case "response.output_item.added":
		// a call the reply made itself (a function_call, Codex's
		// custom_tool_call and tool_search_call, a shell's or search's):
		// no call is read out of the text beside it
		if s := gjson.GetBytes(data, "item.type").String(); strings.HasSuffix(s, "_call") {
			t.native = true
		}
		return ev
	case "response.output_text.delta":
		t.itemID = gjson.GetBytes(data, "item_id").String()
		t.itemIdx = int(gjson.GetBytes(data, "output_index").Int())
		t.partIdx = int(gjson.GetBytes(data, "content_index").Int())
		s := t.pending + gjson.GetBytes(data, "delta").String()
		t.pending = ""
		if t.holding {
			t.held += s
			return nil
		}
		if at, tail := textCallAt(s); at >= 0 {
			t.holding, t.held, s = true, s[at:], s[:at]
		} else {
			t.pending, s = s[len(s)-tail:], s[:len(s)-tail]
		}
		t.pending, t.held, s = spaceBack(t.native, t.holding, t.pending, t.held, s)
		if s == "" {
			return nil
		}
		var e map[string]any
		if decodeNumbers(data, &e) != nil {
			return ev
		}
		e["delta"] = s
		if nb := reData(ev, e); nb != nil {
			return nb
		}
		return ev
	case "response.output_text.done":
		if !t.holding {
			// a mark's beginning cut short at the part's end is just text
			out := t.textDelta(t.pending)
			t.pending = ""
			return append(out, ev...)
		}
		if t.textDone == nil {
			t.textDone = append([]byte(nil), ev...)
			return nil
		}
		return ev
	case "response.content_part.done":
		if t.holding && t.partDone == nil {
			t.partDone = append([]byte(nil), ev...)
			return nil
		}
		return ev
	case "response.output_item.done":
		if t.holding && t.itemDone == nil && gjson.GetBytes(data, "item.type").String() == "message" &&
			gjson.GetBytes(data, "item.id").String() == t.itemID {
			t.itemDone = append([]byte(nil), ev...)
			return nil
		}
		return ev
	case "response.completed":
		return t.completed(ev, data)
	case "response.incomplete", "response.failed", "error":
		return append(t.release(), ev...)
	}
	return ev
}

// completed is the reply's end: the held calls become the items the reply
// made, in the events and in the completed response's output.
func (t *responsesTextCalls) completed(ev, data []byte) []byte {
	if !t.holding {
		return append(t.release(), ev...)
	}
	prefix := func(done []byte) string {
		s := gjson.GetBytes(eventData(done), "text").String()
		if s == "" {
			if part := gjson.GetBytes(eventData(done), "part.text"); part.Exists() {
				s = part.String()
			}
		}
		return strings.TrimSuffix(s, t.held)
	}
	if t.native {
		// tags left in the text of a reply that made calls itself go, as
		// a Chat reply's do; anything else goes as it came
		if !tagsOnly(t.held) {
			return append(t.release(), ev...)
		}
		s := strings.TrimRight(prefix(t.textDone), " \t\r\n")
		out := t.doneText(s)
		out = append(out, t.itemText(s)...)
		t.pending, t.held, t.holding = "", "", false
		t.textDone, t.partDone, t.itemDone = nil, nil, nil
		return append(out, t.rewritten(ev, data, s, nil)...)
	}
	calls, rest, ok := parseTextCalls(t.held, t.names)
	if !ok {
		return append(t.release(), ev...)
	}
	s := prefix(t.textDone) + rest
	out := t.doneText(s)
	var items []map[string]any
	next := t.top
	if t.itemDone == nil {
		// nothing of the item's end went: the calls take its place from
		// its own index on
		next = t.itemIdx
	} else {
		// the item's done, with its part's text said again
		out = append(out, t.itemText(s)...)
	}
	t.pending, t.held, t.holding = "", "", false
	t.textDone, t.partDone, t.itemDone = nil, nil, nil
	for _, c := range calls {
		id, callID := "fc_"+newID(), "call_"+newID()
		item := map[string]any{"type": "function_call", "id": id, "call_id": callID,
			"name": c.Name, "arguments": string(c.Args), "status": "completed"}
		out = append(out, t.send("response.output_item.added", map[string]any{"output_index": next,
			"item": map[string]any{"type": "function_call", "id": id, "call_id": callID,
				"name": c.Name, "arguments": "", "status": "in_progress"}})...)
		out = append(out, t.send("response.function_call_arguments.delta", map[string]any{"item_id": id,
			"output_index": next, "delta": string(c.Args)})...)
		out = append(out, t.send("response.function_call_arguments.done", map[string]any{"item_id": id,
			"output_index": next, "call_id": callID, "arguments": string(c.Args)})...)
		out = append(out, t.send("response.output_item.done", map[string]any{"output_index": next, "item": item})...)
		items = append(items, item)
		next++
	}
	t.top = next
	return append(out, t.rewritten(ev, data, s, items)...)
}

// rewritten is the reply's last event with its output saying what the
// client was told: the held text's item's part saying s, the calls made
// items of the reply.
func (t *responsesTextCalls) rewritten(ev, data []byte, s string, items []map[string]any) []byte {
	if len(items) == 0 && t.itemDone == nil {
		return ev
	}
	var e map[string]any
	if decodeNumbers(data, &e) != nil {
		return ev
	}
	res, _ := e["response"].(map[string]any)
	if res == nil {
		return ev
	}
	out, _ := res["output"].([]any)
	changed := len(items) > 0
	for _, it := range out {
		m, _ := it.(map[string]any)
		if m == nil || m["id"] != t.itemID {
			continue
		}
		content, _ := m["content"].([]any)
		if len(content) > t.partIdx {
			if part, _ := content[t.partIdx].(map[string]any); part != nil && part["type"] == "output_text" {
				part["text"] = s
				changed = true
			}
		}
		break
	}
	for _, it := range items {
		out = append(out, it)
	}
	if !changed {
		return ev
	}
	res["output"] = out
	if nb := reData(ev, e); nb != nil {
		return nb
	}
	return ev
}

// whole reads the calls written into the last text part of a Responses
// reply that came whole: they become function_call items of the reply's
// output. A reply that made a call itself, or whose written blocks aren't
// all calls of names, goes as it came.
func (t *responsesTextCalls) whole(b []byte) []byte {
	if !mayWriteCalls(b, t.names) {
		return b
	}
	var reply map[string]any
	if decodeNumbers(b, &reply) != nil || reply == nil {
		return b
	}
	out, _ := reply["output"].([]any)
	if len(out) == 0 {
		return b
	}
	native := false
	for _, it := range out {
		if m, _ := it.(map[string]any); m != nil {
			if s, _ := m["type"].(string); strings.HasSuffix(s, "_call") {
				native = true
			}
		}
	}
	at := -1
	var part map[string]any
	for _, it := range out {
		m, _ := it.(map[string]any)
		if m == nil || m["type"] != "message" {
			continue
		}
		content, _ := m["content"].([]any)
		for _, c := range content {
			p, _ := c.(map[string]any)
			if p == nil || p["type"] != "output_text" {
				continue
			}
			s, _ := p["text"].(string)
			if n, _ := textCallAt(s); n >= 0 {
				at, part = n, p
			}
		}
	}
	if part == nil {
		return b
	}
	s, _ := part["text"].(string)
	if native {
		// tags left in the text of a reply that made calls itself go, as
		// a Chat reply's do
		if !tagsOnly(s[at:]) {
			return b
		}
		part["text"] = strings.TrimRight(s[:at], " \t\r\n")
	} else {
		calls, rest, ok := parseTextCalls(s[at:], t.names)
		if !ok {
			return b
		}
		part["text"] = s[:at] + rest
		for _, c := range calls {
			out = append(out, map[string]any{"type": "function_call", "id": "fc_" + newID(),
				"call_id": "call_" + newID(), "name": c.Name, "arguments": string(c.Args), "status": "completed"})
		}
	}
	reply["output"] = out
	nb, err := marshalPlain(reply)
	if err != nil {
		return b
	}
	return nb
}

// geminiTextCalls does the same in a Gemini reply relayed as it came
// (#4743): a Gemini client (Gemini CLI) on a custom provider's Gemini URL
// or Vertex AI, whose model wrote its calls into its text. Text parts are
// read as the chunks come; at the candidate's finishReason the held calls
// become functionCall parts after the text. A reply that came whole
// (Vertex AI's generateContent) is read the same over its
// candidates[].content.parts.

type gemHold struct {
	pending string
	held    string
	holding bool
	native  bool // the candidate made a call itself
}

type geminiTextCalls struct {
	names map[string]bool
	sse   bool
	buf   []byte
	holds map[int]*gemHold
}

func (t *geminiTextCalls) hold(i int) *gemHold {
	if t.holds == nil {
		t.holds = map[int]*gemHold{}
	}
	if t.holds[i] == nil {
		t.holds[i] = &gemHold{}
	}
	return t.holds[i]
}

func (t *geminiTextCalls) write(b []byte) []byte {
	t.buf = append(t.buf, b...)
	if !t.sse {
		return nil
	}
	i := bytes.LastIndexByte(t.buf, '\n')
	if i < 0 {
		return nil
	}
	var out []byte
	for _, line := range bytes.SplitAfter(t.buf[:i+1], []byte("\n")) {
		out = append(out, t.line(line)...)
	}
	t.buf = append(t.buf[:0], t.buf[i+1:]...)
	return out
}

func (t *geminiTextCalls) flush() []byte {
	b := t.buf
	t.buf = nil
	if !t.sse {
		return t.whole(b)
	}
	var out []byte
	if len(b) > 0 {
		out = t.line(b)
	}
	return out
}

func (t *geminiTextCalls) line(line []byte) []byte {
	body := bytes.TrimRight(line, "\r\n")
	data, ok := bytes.CutPrefix(body, []byte("data:"))
	if !ok {
		return line
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "[DONE]" {
		return line
	}
	var ch map[string]any
	if decodeNumbers(data, &ch) != nil || ch == nil {
		return line
	}
	if !t.candidates(ch) {
		return line
	}
	nb, err := marshalPlain(ch)
	if err != nil {
		return line
	}
	return append(append([]byte("data: "), nb...), line[len(body):]...)
}

// candidates reads a chunk's candidates in place: text parts held or
// trimmed, the held calls made functionCall parts at the finishReason. It
// says whether the chunk changed.
func (t *geminiTextCalls) candidates(ch map[string]any) bool {
	cands, _ := ch["candidates"].([]any)
	changed := false
	for i, ci := range cands {
		c, _ := ci.(map[string]any)
		if c == nil {
			continue
		}
		h := t.hold(i)
		content, _ := c["content"].(map[string]any)
		parts, _ := content["parts"].([]any)
		keep := parts[:0:0]
		for _, pi := range parts {
			p, _ := pi.(map[string]any)
			switch {
			case p == nil:
				keep = append(keep, p)
			case p["functionCall"] != nil:
				h.native = true
				keep = append(keep, p)
			default:
				was, isText := p["text"].(string)
				if !isText {
					keep = append(keep, p)
					continue
				}
				if thought, _ := p["thought"].(bool); thought {
					keep = append(keep, p)
					continue
				}
				s := h.pending + was
				h.pending = ""
				if h.holding {
					h.held += s
					changed = true
					continue
				}
				if at, tail := textCallAt(s); at >= 0 {
					h.holding, h.held, s = true, s[at:], s[:at]
				} else {
					h.pending, s = s[len(s)-tail:], s[:len(s)-tail]
				}
				if s != was {
					changed = true
				}
				if s != "" {
					p["text"] = s
					keep = append(keep, p)
				}
			}
		}
		if fin, _ := c["finishReason"].(string); fin != "" {
			// the candidate is done: what it held becomes calls, or goes
			// back as the text it was; tags left beside calls of its own go
			s := h.pending
			h.pending = ""
			if h.holding {
				h.holding = false
				switch {
				case !h.native:
					if calls, rest, ok := parseTextCalls(h.held, t.names); ok {
						h.held = ""
						s += rest
						for _, c := range calls {
							keep = append(keep, map[string]any{"functionCall": map[string]any{
								"name": c.Name, "args": json.RawMessage(c.Args)}})
						}
					} else {
						s += h.held
					}
					h.held = ""
				case tagsOnly(h.held):
					h.held = ""
				default:
					s += h.held
					h.held = ""
				}
				changed = true
			}
			if s != "" {
				keep = append(keep, map[string]any{"text": s})
				changed = true
			}
		}
		if changed && content != nil {
			content["parts"] = keep
		}
	}
	return changed
}

// whole reads the calls written into the last text part of each of a
// Gemini reply's candidates: they become functionCall parts after it. A
// candidate that made a call itself goes as it came.
func (t *geminiTextCalls) whole(b []byte) []byte {
	if !mayWriteCalls(b, t.names) {
		return b
	}
	var ch map[string]any
	if decodeNumbers(b, &ch) != nil || ch == nil {
		return b
	}
	cands, _ := ch["candidates"].([]any)
	changed := false
	for _, ci := range cands {
		c, _ := ci.(map[string]any)
		if c == nil {
			continue
		}
		content, _ := c["content"].(map[string]any)
		parts, _ := content["parts"].([]any)
		native, part := false, map[string]any(nil)
		for _, pi := range parts {
			p, _ := pi.(map[string]any)
			if p == nil {
				continue
			}
			if p["functionCall"] != nil {
				native = true
				continue
			}
			if _, ok := p["text"].(string); ok {
				if thought, _ := p["thought"].(bool); !thought {
					part = p
				}
			}
		}
		if part == nil || native {
			continue
		}
		s, _ := part["text"].(string)
		at, _ := textCallAt(s)
		if at < 0 {
			continue
		}
		calls, rest, ok := parseTextCalls(s[at:], t.names)
		if !ok {
			continue
		}
		part["text"] = s[:at] + rest
		for _, c := range calls {
			parts = append(parts, map[string]any{"functionCall": map[string]any{
				"name": c.Name, "args": json.RawMessage(c.Args)}})
		}
		content["parts"] = parts
		changed = true
	}
	if !changed {
		return b
	}
	nb, err := marshalPlain(ch)
	if err != nil {
		return b
	}
	return nb
}
