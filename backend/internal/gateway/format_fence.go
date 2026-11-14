package gateway

import (
	"encoding/json"
	"strings"
)

// unfence is said with a code fence around the whole of it taken off, and
// whether there was one. Only a whole JSON value inside an unlabelled or
// json-labelled fence is unwrapped; invalid and truncated answers stay intact.
func unfence(said string) (string, bool) {
	t := strings.TrimSpace(said)
	if len(t) < 6 || !strings.HasPrefix(t, "```") || !strings.HasSuffix(t, "```") {
		return said, false
	}
	nl := strings.IndexByte(t, '\n')
	if nl < 0 {
		return said, false
	}
	if tag := strings.TrimSpace(t[3:nl]); tag != "" && !strings.EqualFold(tag, "json") {
		return said, false
	}
	inner := strings.TrimSpace(t[nl+1 : len(t)-3])
	if !json.Valid([]byte(inner)) {
		return said, false
	}
	return inner, true
}

// unfencedResult is res with its answer's fence taken off, for a client that
// asked for structured output.
func unfencedResult(res Result, r *Request) Result {
	if !r.wantsJSON() {
		return res
	}
	var parts []Part
	for i, p := range res.Parts {
		if p.Kind != Text {
			continue
		}
		if t, ok := unfence(p.Text); ok {
			if parts == nil {
				parts = append([]Part(nil), res.Parts...)
			}
			parts[i].Text = t
		}
	}
	if parts != nil {
		res.Parts = parts
	}
	return res
}

// fenceEncoder holds a structured-output reply's text while it could still
// be a fenced answer: once it begins with anything but ``` it passes on as
// it comes; a reply that began with a fence is handed on whole, unfenced,
// when its text ends.
type fenceEncoder struct {
	streamEncoder
	held    strings.Builder
	holding bool // the text so far could be a fence's start
	past    bool // the text began otherwise; nothing is held now
}

func (e *fenceEncoder) event(ev Event) {
	if ev.Kind != KText {
		if ev.Kind != KStart && ev.Kind != KUsage {
			e.flush()
		}
		e.streamEncoder.event(ev)
		return
	}
	if e.past {
		e.streamEncoder.event(ev)
		return
	}
	e.held.WriteString(ev.Text)
	lead := strings.TrimLeft(e.held.String(), " \t\r\n")
	if lead == "" || strings.HasPrefix("```", lead) || strings.HasPrefix(lead, "```") {
		e.holding = true
		return
	}
	e.past, e.holding = true, false
	ev.Text = e.held.String()
	e.held.Reset()
	e.streamEncoder.event(ev)
}

// flush hands on the text held, unfenced when it is a fenced answer.
func (e *fenceEncoder) flush() {
	if !e.holding {
		return
	}
	e.holding, e.past = false, true
	t := e.held.String()
	e.held.Reset()
	if u, ok := unfence(t); ok {
		t = u
	}
	if t != "" {
		e.streamEncoder.event(Event{Kind: KText, Text: t})
	}
}

func (e *fenceEncoder) finish() {
	e.flush()
	e.streamEncoder.finish()
}
