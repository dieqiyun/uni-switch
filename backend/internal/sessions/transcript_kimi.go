package sessions

import (
	"bytes"
	"cmp"
	"encoding/json"
	"path/filepath"
)

// Kimi Code's conversation, read from a session's main wire.jsonl (#2056,
// infinitr0us). From protocol 1.4 on a turn is its turn.prompt (the user's
// input; one whose origin isn't the user, a skill's activation or a
// system_trigger, is shown folded, but for a subagent's task in its own
// file) and the loop events written as it
// runs: content.part (text or think), tool.call (name, args, toolCallId)
// and tool.result (toolCallId, result.output). These alone are read, since
// they are there for every 1.4 and 1.5 session, finished or still running:
// Kimi Code 2.1.1 writes a turn's agent.message.appended only as it ends,
// and older 1.5 sessions have none. A turn.steer is what came in during a
// turn: the user's words, or a background task's news.
//
// What a session imported from the Python kimi-cli said is in
// context.append_message records with no time (protocol 1.0, and the head
// of a ses_* session Kimi Code opened again): role user, assistant or tool,
// text and think blocks, an assistant's toolCalls ({id, name, arguments} or
// OpenAI's {id, function: {name, arguments}}) and a tool message's
// toolCallId. Kimi Code's own context.append_message records carry a time
// and say again what the prompt and loop events said, so they are passed
// over.
//
// A fork's file starts with its source's records, copied up to its forked
// marker; they are read too, as the conversation the fork goes on from.

type kimiLine struct {
	Type  string `json:"type"`
	Time  int64  `json:"time"`
	Input []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"input"`
	Origin struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"origin"`
	Event struct {
		Type       string          `json:"type"`
		Name       string          `json:"name"`
		ToolCallID string          `json:"toolCallId"`
		Args       json.RawMessage `json:"args"`
		Part       struct {
			Type  string `json:"type"`
			Text  string `json:"text"`
			Think string `json:"think"`
		} `json:"part"`
		Result struct {
			Output json.RawMessage `json:"output"`
		} `json:"result"`
	} `json:"event"`
	Message struct {
		Role    string `json:"role"`
		Content []struct {
			Type  string `json:"type"`
			Text  string `json:"text"`
			Think string `json:"think"`
		} `json:"content"`
		ToolCalls []struct {
			ID        string          `json:"id"`
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
			Function  struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"function"`
		} `json:"toolCalls"`
		ToolCallID string `json:"toolCallId"`
	} `json:"message"`
}

var kimiSaid = [][]byte{[]byte(`"turn.prompt"`), []byte(`"turn.steer"`), []byte(`"content.part"`), []byte(`"tool.call"`), []byte(`"tool.result"`), []byte(`"context.append_message"`)}

// kimiTranscript tells add what a Kimi Code session said, in the order of
// its main wire file, decoding only the lines keep says yes to.
func kimiTranscript(path string, keep func([]byte) bool, add func(bool, Part) bool) error {
	names := map[string]string{} // a tool call's name, by its id
	_, err := scanAt(path, 0, nil, func(b []byte, _, _ int64) bool {
		said := false
		for _, k := range kimiSaid {
			if bytes.Contains(b, k) {
				said = true
				break
			}
		}
		if !said || (keep != nil && !keep(b)) {
			return true
		}
		var l kimiLine
		if json.Unmarshal(b, &l) != nil {
			return true
		}
		if kimiTask(path, l) {
			l.Origin.Kind = "user"
		}
		for _, p := range kimiParts(l, names) {
			if !add(p.Role != "assistant", p) {
				return false
			}
		}
		return true
	})
	return err
}

// kimiTask is whether a line is the task a subagent was given: in its own
// wire file (agents/agent-<n>), a turn.prompt from Kimi Code's system_trigger
// named subagent (#2290), its first message, as a Claude Code or OpenCode
// subagent's is.
func kimiTask(path string, l kimiLine) bool {
	return l.Type == "turn.prompt" && l.Origin.Kind == "system_trigger" && l.Origin.Name == "subagent" &&
		filepath.Base(filepath.Dir(path)) != "main"
}

// kimiParts are what one line of a wire file said.
func kimiParts(l kimiLine, names map[string]string) []Part {
	switch l.Type {
	case "turn.prompt", "turn.steer":
		var parts []Part
		for _, p := range l.Input {
			if p.Type != "text" {
				continue
			}
			if l.Origin.Kind == "user" {
				parts = append(parts, textPart("user", p.Text))
			} else {
				// a skill's activation, a background task's news: folded
				parts = append(parts, Part{Role: "user", Kind: "context", Text: p.Text})
			}
		}
		return parts
	case "context.append_loop_event":
		e := l.Event
		switch e.Type {
		case "content.part":
			switch e.Part.Type {
			case "text":
				return []Part{textPart("assistant", e.Part.Text)}
			case "think":
				return []Part{{Role: "assistant", Kind: "thinking", Text: e.Part.Think}}
			}
		case "tool.call":
			names[e.ToolCallID] = e.Name
			return []Part{{Role: "assistant", Kind: "tool_use", Name: e.Name, Text: pretty(e.Args)}}
		case "tool.result":
			return []Part{{Role: "tool", Kind: "tool_result", Name: names[e.ToolCallID], Text: resultText(e.Result.Output)}}
		}
	case "context.append_message":
		if l.Time != 0 {
			return nil
		}
		return kimiMessageParts(l, names)
	}
	return nil
}

// kimiMessageParts are what a message imported from kimi-cli said.
func kimiMessageParts(l kimiLine, names map[string]string) []Part {
	m := l.Message
	var parts []Part
	switch m.Role {
	case "user", "assistant":
		for _, c := range m.Content {
			switch c.Type {
			case "text":
				parts = append(parts, textPart(m.Role, c.Text))
			case "think":
				parts = append(parts, Part{Role: m.Role, Kind: "thinking", Text: c.Think})
			}
		}
		for _, c := range m.ToolCalls {
			name := cmp.Or(c.Name, c.Function.Name)
			names[c.ID] = name
			args := c.Arguments
			if len(args) == 0 {
				args = c.Function.Arguments
			}
			parts = append(parts, Part{Role: "assistant", Kind: "tool_use", Name: name, Text: kimiArgs(args)})
		}
	case "tool":
		var text []byte
		for _, c := range m.Content {
			if c.Type == "text" {
				if len(text) > 0 {
					text = append(text, '\n')
				}
				text = append(text, c.Text...)
			}
		}
		parts = append(parts, Part{Role: "tool", Kind: "tool_result", Name: names[m.ToolCallID], Text: string(text)})
	}
	return parts
}

// kimiArgs is a call's arguments: an OpenAI-style call has them as a string
// of JSON.
func kimiArgs(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return pretty(json.RawMessage(s))
	}
	return pretty(raw)
}
