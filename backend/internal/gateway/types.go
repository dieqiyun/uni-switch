// Package gateway implements our protocol gateway from the reviewed Magpie
// parse -> shared representation -> build/decode -> render design. No Magpie
// package, executable or data directory is used at build time or at runtime.
package gateway

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

type Object = map[string]any
type Protocol string

const (
	Messages  Protocol = "messages"
	Responses Protocol = "responses"
	Chat      Protocol = "chat"
	Gemini    Protocol = "gemini"
	maxBody            = 16 << 20
	// Product-owned envelopes; the contents and protocol ownership follow
	// claudeseal and sealedBy, not a new vendor encryption format.
	claudeSeal  = "uni-claude1:"
	compactSeal = "uni-checkpoint1:"
	callMark    = "us_"
)

type Part struct {
	Kind, Text, ID, Name, Signature, SealedBy string
	ClientID                                  string
	Args                                      any
	Image                                     Object
	File                                      Object
	Content                                   []Part
	Raw                                       Object
	IsError                                   bool
}
type Turn struct {
	Role  string
	Parts []Part
}
type Tool struct {
	Name, Namespace, Original, Description string
	Schema                                 any
	Custom, Strict                         bool
}
type Request struct {
	Source                        Protocol
	Model, System, Effort, Choice string
	Turns                         []Turn
	Tools                         []Tool
	Max                           int
	Stream, Think, Off            bool
	Resume                        bool
	Parallel                      *bool
	Temp, TopP                    any
	Stop                          []any
	Format                        Object
	Original                      Object
	GenConfig                     Object
	HostedTools                   []any
	WebSearch                     bool
}
type Result struct {
	ID, Model, Stop string
	Parts           []Part
	Usage           Object
}

func obj(v any) Object {
	if raw, ok := v.(json.RawMessage); ok {
		var m Object
		_ = decode(raw, &m)
		return m
	}
	m, _ := v.(map[string]any)
	return m
}
func list(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	if a, ok := v.([]string); ok {
		out := make([]any, len(a))
		for i, s := range a {
			out[i] = s
		}
		return out
	}
	return nil
}
func str(v any) string { s, _ := v.(string); return s }
func integer(v any) int {
	switch n := v.(type) {
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}
func field(v any, keys ...string) any {
	for _, k := range keys {
		v = obj(v)[k]
	}
	return v
}
func clone(m Object) Object {
	b, _ := json.Marshal(m)
	var c Object
	_ = json.Unmarshal(b, &c)
	return c
}
func encode(v any) string { b, _ := json.Marshal(v); return string(b) }
func decode(b []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return invalid("JSON 包含多余内容")
	}
	return nil
}
func uid(prefix string) string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return prefix + hex.EncodeToString(b[:])
}
func push(ts *[]Turn, role string, ps ...Part) {
	if len(ps) == 0 {
		return
	}
	n := len(*ts)
	if n > 0 && (*ts)[n-1].Role == role {
		(*ts)[n-1].Parts = append((*ts)[n-1].Parts, ps...)
	} else {
		*ts = append(*ts, Turn{role, ps})
	}
}
func allText(ps []Part) string {
	var out []string
	for _, p := range ps {
		if p.Kind == "text" {
			out = append(out, p.Text)
		}
	}
	return strings.Join(out, "\n\n")
}
func seal(prefix string, v any) string {
	return prefix + base64.StdEncoding.EncodeToString([]byte(encode(v)))
}
func openSeal(s, prefix string) (Object, bool) {
	v, ok := strings.CutPrefix(s, prefix)
	if !ok {
		return nil, false
	}
	b, e := base64.StdEncoding.DecodeString(v)
	if e != nil {
		return nil, true
	}
	var m Object
	if decode(b, &m) != nil {
		return nil, true
	}
	return m, true
}
func safeID(s string) string {
	if s == "" {
		s = uid("call_")
	}
	if safeChars(s) && !strings.HasPrefix(s, callMark) && !strings.HasPrefix(s, repeatMark) {
		return s
	}
	return callMark + base64.RawURLEncoding.EncodeToString([]byte(s))
}
func rawID(s string) string {
	if original, ok := repeatedID(s); ok {
		return original
	}
	if !strings.HasPrefix(s, callMark) {
		return s
	}
	b, e := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, callMark))
	if e != nil || !utf8.Valid(b) || len(b) == 0 || safeID(string(b)) != s {
		return s
	}
	return string(b)
}
func safeChars(s string) bool {
	if s == "" {
		return false
	}
	for _, b := range []byte(s) {
		if !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '-') {
			return false
		}
	}
	return true
}
func effort(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "minimal", "none", "light":
		return "low"
	case "ultra":
		return "max"
	case "extra_high":
		return "xhigh"
	case "low", "medium", "high", "xhigh", "max":
		return s
	}
	return ""
}
func effortBudget(n int) string {
	switch {
	case n <= 0:
		return ""
	case n <= 4096:
		return "low"
	case n <= 12000:
		return "medium"
	case n <= 24000:
		return "high"
	}
	return "xhigh"
}
func budget(s string) int {
	switch effort(s) {
	case "low":
		return 4096
	case "high":
		return 24000
	case "xhigh", "max":
		return 32000
	}
	return 10000
}
func invalid(what string) error { return fmt.Errorf("协议请求无效：%s", what) }

var errTruncated = errors.New("上游流在完整结束事件前断开")

// Instructions are hoisted only before the first assistant/tool exchange.
// Later instructions remain in place as a user reminder (responseInstructions).
func (r *Request) instructions() {
	var turns []Turn
	replied := false
	for _, m := range r.Turns {
		if m.Role == "assistant" {
			replied = true
		}
		for _, p := range m.Parts {
			if p.Kind == "call" || p.Kind == "result" || p.Kind == "thinking" {
				replied = true
			}
		}
		if m.Role == "system" || m.Role == "developer" {
			t := allText(m.Parts)
			if t != "" {
				if replied {
					push(&turns, "user", Part{Kind: "text", Text: "<system-reminder>\n" + t + "\n</system-reminder>"})
				} else {
					if r.System != "" {
						r.System += "\n\n"
					}
					r.System += t
				}
			}
		} else {
			push(&turns, m.Role, m.Parts...)
		}
	}
	r.Turns = turns
}

// Repair tool exchanges at the shared boundary. The first result wins; orphan
// results keep their content as user data; missing results say interrupted.
// This is pairToolBlocks/pairToolMessages, not successful synthetic execution.
func (r *Request) pairTools() {
	var out []Turn
	pending := []string{}
	answered := map[string]bool{}
	closeCalls := func() {
		for _, id := range pending {
			if !answered[id] {
				push(&out, "user", Part{Kind: "result", ID: id, Text: "Tool execution was interrupted.", IsError: true})
			}
		}
		pending = nil
		answered = map[string]bool{}
	}
	for _, m := range r.Turns {
		if m.Role == "system" || m.Role == "developer" {
			closeCalls()
			push(&out, m.Role, m.Parts...)
			continue
		}
		if m.Role == "assistant" {
			// Consecutive assistant parts form one tool turn.
			if len(out) > 0 && out[len(out)-1].Role != "assistant" {
				closeCalls()
			}
			for _, p := range m.Parts {
				if p.Kind == "call" {
					pending = append(pending, p.ID)
				}
			}
			push(&out, m.Role, m.Parts...)
			continue
		}
		var results, rest []Part
		for _, p := range m.Parts {
			if p.Kind != "result" {
				rest = append(rest, p)
				continue
			}
			found := false
			for _, id := range pending {
				if id == p.ID {
					found = true
					break
				}
			}
			if !found {
				if len(p.Content) > 0 {
					rest = append(rest, p.Content...)
				} else {
					t := p.Text
					if t == "" {
						t = "Tool result received."
					}
					rest = append(rest, Part{Kind: "text", Text: t})
				}
			} else if !answered[p.ID] {
				answered[p.ID] = true
				results = append(results, p)
			}
		}
		for _, id := range pending {
			if !answered[id] {
				results = append(results, Part{Kind: "result", ID: id, Text: "Tool execution was interrupted.", IsError: true})
			}
		}
		push(&out, "user", append(results, rest...)...)
		pending = nil
		answered = map[string]bool{}
	}
	closeCalls()
	r.Turns = out
}
