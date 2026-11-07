package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

func relayHTTP(ctx context.Context, response *http.Response, sink Sink, protocol Protocol, ids *messageIDs) error {
	headers := map[string]string{}
	for _, key := range []string{"Content-Type", "Cache-Control", "Retry-After", "X-Request-Id"} {
		if v := response.Header.Get(key); v != "" {
			headers[key] = v
		}
	}
	if e := sink(Packet{Type: "head", Status: response.StatusCode, Headers: headers}); e != nil {
		return e
	}
	timer := time.AfterFunc(90*time.Second, func() { _ = response.Body.Close() })
	defer timer.Stop()
	reader := &idleReader{response.Body, timer}
	tidy := nativeTidy{protocol: protocol, ids: ids, chatSeen: map[string]bool{}}
	if response.StatusCode >= 200 && response.StatusCode < 300 && strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		// Preserve raw vendor events while observing completion. Only an event
		// requiring a tool-ID repair is reconstructed, keeping its extensions.
		guard := relayCompletion{protocol: protocol, blocks: map[int]bool{}}
		err := readRawSSE(reader, func(f Frame, raw []byte) error {
			var observed error
			if f.Data != "" {
				observed = guard.accept(f)
				if changed := tidy.event(f); changed != nil {
					raw = replaceSSEData(raw, changed)
				}
			}
			if _, err := (packetWriter{sink}).Write(raw); err != nil {
				return err
			}
			return observed
		})
		if err != nil && !errors.Is(err, errRelayDone) {
			return err
		}
		if !guard.finished && !(protocol == Chat && guard.stop) {
			return errTruncated
		}
		return sink(Packet{Type: "end"})
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 && (protocol == Messages || protocol == Chat) {
		body, err := limited(reader)
		if err != nil {
			return err
		}
		if changed := tidy.whole(body); changed != nil {
			body = changed
		}
		if _, err := (packetWriter{sink}).Write(body); err != nil {
			return err
		}
		return sink(Packet{Type: "end"})
	}
	buf := make([]byte, 32<<10)
	for {
		n, e := reader.Read(buf)
		if n > 0 {
			if err := sink(Packet{Type: "data", Data: base64.StdEncoding.EncodeToString(buf[:n])}); err != nil {
				return err
			}
		}
		if e == io.EOF {
			return sink(Packet{Type: "end"})
		}
		if e != nil {
			return e
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

type nativeTidy struct {
	protocol Protocol
	ids      *messageIDs
	chatSeen map[string]bool
}

func (t *nativeTidy) event(f Frame) []byte {
	var v Object
	if decode([]byte(f.Data), &v) != nil {
		return nil
	}
	changed := false
	if t.protocol == Messages && (v["type"] == "content_block_start" || f.Name == "content_block_start") {
		b := obj(v["content_block"])
		if b["type"] == "tool_use" && str(b["id"]) != "" {
			id := str(b["id"])
			b["id"] = t.ids.next(id)
			changed = b["id"] != id
		}
	}
	if t.protocol == Chat {
		for choiceAt, value := range list(v["choices"]) {
			c := obj(value)
			choice := c["index"]
			if choice == nil {
				choice = choiceAt
			}
			for callAt, value := range list(field(c, "delta", "tool_calls")) {
				call := obj(value)
				if call == nil {
					continue
				}
				index := call["index"]
				if index == nil {
					index = callAt
				}
				key := encode(choice) + ":" + encode(index)
				if str(call["id"]) == "" {
					if !t.chatSeen[key] {
						call["id"] = uid("call_")
					} else {
						delete(call, "id")
					}
					changed = true
				}
				t.chatSeen[key] = true
			}
		}
	}
	if changed {
		return []byte(encode(v))
	}
	return nil
}

func (t *nativeTidy) whole(raw []byte) []byte {
	var v Object
	if decode(raw, &v) != nil {
		return nil
	}
	changed := false
	if t.protocol == Messages {
		for _, value := range list(v["content"]) {
			b := obj(value)
			if b["type"] == "tool_use" && str(b["id"]) != "" {
				id := str(b["id"])
				b["id"] = t.ids.next(id)
				changed = changed || b["id"] != id
			}
		}
	} else if t.protocol == Chat {
		for _, value := range list(v["choices"]) {
			m := obj(field(value, "message"))
			calls := list(m["tool_calls"])
			kept := []any{}
			for _, call := range calls {
				c := obj(call)
				if str(field(c, "function", "name")) == "" {
					changed = true
					continue
				}
				if str(c["id"]) == "" {
					c["id"] = uid("call_")
					changed = true
				}
				kept = append(kept, c)
			}
			if len(calls) > 0 {
				if len(kept) == 0 {
					delete(m, "tool_calls")
					if reason := str(field(value, "finish_reason")); reason == "tool_calls" || reason == "function_call" {
						obj(value)["finish_reason"] = "stop"
					}
				} else {
					m["tool_calls"] = kept
				}
			}
		}
	}
	if changed {
		return []byte(encode(v))
	}
	return nil
}

func replaceSSEData(raw, data []byte) []byte {
	var out bytes.Buffer
	written := false
	for len(raw) > 0 {
		n, line, _ := sseLine(raw, true)
		raw = raw[n:]
		if bytes.HasPrefix(line, []byte("data:")) {
			if !written {
				ending := line[len(bytes.TrimRight(line, "\r\n")):]
				out.WriteString("data: ")
				out.Write(data)
				out.Write(ending)
				written = true
			}
		} else {
			out.Write(line)
		}
	}
	return out.Bytes()
}

type packetWriter struct{ sink Sink }

func (w packetWriter) Write(b []byte) (int, error) {
	if err := w.sink(Packet{Type: "data", Data: base64.StdEncoding.EncodeToString(b)}); err != nil {
		return 0, err
	}
	return len(b), nil
}

var errRelayDone = errors.New("native stream completed")

type relayCompletion struct {
	protocol       Protocol
	blocks         map[int]bool
	stop, finished bool
}

func (g *relayCompletion) accept(f Frame) error {
	if strings.TrimSpace(f.Data) == "[DONE]" {
		if g.protocol != Chat || !g.stop {
			return errTruncated
		}
		g.finished = true
		return errRelayDone
	}
	var v Object
	if decode([]byte(f.Data), &v) != nil {
		return errors.New("原生 SSE 事件不是 JSON")
	}
	typ := str(v["type"])
	if typ == "" {
		typ = f.Name
	}
	if v["error"] != nil || typ == "error" || typ == "response.failed" || field(v, "response", "status") == "failed" || field(v, "response", "error") != nil {
		return errors.New("上游返回流式错误")
	}
	switch g.protocol {
	case Responses:
		g.finished = typ == "response.completed" || typ == "response.incomplete"
	case Messages:
		switch typ {
		case "content_block_start":
			g.blocks[integer(v["index"])] = true
		case "content_block_stop":
			delete(g.blocks, integer(v["index"]))
		case "message_delta":
			g.stop = g.stop || str(field(v, "delta", "stop_reason")) != ""
		case "message_stop":
			if !g.stop || len(g.blocks) > 0 {
				return errTruncated
			}
			g.finished = true
		}
	case Chat:
		for _, c := range list(v["choices"]) {
			g.stop = g.stop || str(field(c, "finish_reason")) != ""
		}
	case Gemini:
		if wrapped := obj(v["response"]); wrapped != nil {
			v = wrapped
		}
		if v["error"] != nil {
			return errors.New("Gemini 上游返回流式错误")
		}
		g.finished = g.finished || field(v, "promptFeedback", "blockReason") != nil
		for _, c := range list(v["candidates"]) {
			g.finished = g.finished || str(field(c, "finishReason")) != ""
		}
	}
	if g.finished && g.protocol != Gemini {
		return errRelayDone
	}
	return nil
}
