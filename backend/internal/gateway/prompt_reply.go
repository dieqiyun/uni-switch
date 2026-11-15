package gateway

// The reply beside the prompt's text (wangbinyq on Discord): the context
// window card's text view shows what the model answered too, read from the
// reply as the agent got it, from the Recent calls in memory or from the
// request archive's copy. Credentials are removed from these diagnostic
// copies; personal text is preserved.

import (
	"github.com/dieqiyun/uni-switch/backend/internal/logsafe"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// PromptReply is a request's reply, piece by piece: the answer's text, its
// thinking and its tool calls.
type PromptReply struct {
	ID     int64         `json:"id"`
	Pieces []PromptPiece `json:"pieces"`
	// Cut: only the reply's start was kept, or it ended before it was whole
	Cut bool `json:"cut,omitempty"`
	// From: "memory" (the Recent calls) or "archive"
	From string `json:"from"`
}

// ReplyBody is the reply to request route id as the Recent calls keep it,
// with the protocol it was written in; false once the call has left them.
func (s *Server) ReplyBody(id int64) (provider.Protocol, []byte, bool, bool) {
	c, ok := s.RouteCall(id)
	if !ok || c.ResponseBody == "" {
		return "", nil, false, false
	}
	return c.From, []byte(c.ResponseBody), c.ResponseTruncated, true
}

// ReplyPieces reads a reply body (JSON or a stream) written in proto into
// pieces, with credentials removed; false when nothing in it can be read.
func ReplyPieces(proto provider.Protocol, body []byte, cut bool) ([]PromptPiece, bool, bool) {
	parts, outCut := conversationOutput(proto, logsafe.ScrubJSON(body))
	var pieces []PromptPiece
	for _, p := range parts {
		pc := PromptPiece{Role: "assistant", Text: p.Text}
		switch p.Kind {
		case "thinking":
			pc.Role = "thinking"
		case "tool_use":
			pc.Role, pc.Name = "call", p.Name
		}
		pieces = append(pieces, pc)
	}
	if len(pieces) == 0 {
		return nil, false, false
	}
	for i := range pieces {
		if pieces[i].Role == "call" {
			pieces[i] = rawPiece("call", pieces[i].Name, []byte(pieces[i].Text)).read()
		}
	}
	return pieces, cut || outCut, true
}
