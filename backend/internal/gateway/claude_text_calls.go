package gateway

import "strings"

// A run started anew, or a turn that goes on with the client's own calls in
// it, is told the conversation as text, each past call a "[tool call …]"
// line (renderCachedParts). The model can take that for how it calls a
// tool and write such lines itself: Claude Code then has no call to make,
// the turn ends, and the client shows the lines as the reply's words
// (Zhenzhen on Discord, Claude Desktop on Fable 5.1).
const textCallOpen = "[tool call "

// textCallOpens are the lines that open a call told in text: the form
// above, and Claude's own call written out (packinggoodsllc_07668 on
// Discord: Claude Code through a routing group of Claude models was shown
// <invoke name="ToolSearch"><parameter name="query">…</parameter></invoke>
// and a "*Result: …*" the model made up, with nothing run), bare or in
// its <function_calls>.
var textCallOpens = []string{textCallOpen, "<invoke name=", "<function_calls>"}

// textCallsAsk is what a run whose reply wrote its calls as text is told,
// once, so it makes them (readOutput).
const textCallsAsk = `Your last message wrote tool calls out as text instead of calling the tools. ` +
	`Text like that runs nothing: no tool ran, and the user doesn't see those lines. Make the calls with your tools now.`

// pastCallsNote ends a prompt that tells past calls as text: the lines
// record them, and writing one calls nothing.
const pastCallsNote = "(The \"[tool call …]\" and \"[tool result …]\" lines above record calls made earlier. " +
	"Writing such a line calls nothing: call a tool only with the tool itself.)"

// textCalls holds back a reply's words from the first line that opens as a
// call told in text (textCallOpens), so the client isn't shown it. Words up
// to that line go on as they come; a line is held only while it could
// still open so.
type textCalls struct {
	mid  bool   // the words so far end inside a line that can't be one
	line string // the start of a line that may open as one
	seen bool   // a line opened as one: the rest is held
	held strings.Builder
}

// push takes the next of the reply's words and says which go to the client
// now.
func (c *textCalls) push(s string) string {
	if c.seen {
		c.held.WriteString(s)
		return ""
	}
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if c.mid {
			out.WriteByte(ch)
			c.mid = ch != '\n'
			continue
		}
		line := c.line + s[i:i+1] // a byte, as a UTF-8 rune may be cut across chunks
		open := strings.TrimLeft(line, " \t")
		opens, may := false, false
		for _, o := range textCallOpens {
			opens = opens || strings.HasPrefix(open, o)
			may = may || strings.HasPrefix(o, open)
		}
		switch {
		case opens:
			c.seen, c.line = true, ""
			c.held.WriteString(line)
			c.held.WriteString(s[i+1:])
			return out.String()
		case may:
			c.line = line
		default:
			out.WriteString(line)
			c.line, c.mid = "", ch != '\n'
		}
	}
	return out.String()
}

// end is what was held at the reply's end: the words from the call told in
// text on when seen, else a line's start that never became one.
func (c *textCalls) end() (held string, seen bool) {
	if !c.seen {
		held, c.line = c.line, ""
		return held, false
	}
	return c.held.String(), true
}
