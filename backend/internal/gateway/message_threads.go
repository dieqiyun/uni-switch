package gateway

import (
	"net/http"

	"github.com/tidwall/gjson"
)

// Claude Code's Message Threads (the message-threads-2026-08-12 beta):
// a turn names a thread, {"type":"create"} on its first and
// {"type":"continue","previous_message_id":…} after, and a continue
// carries only what came after that message, leaving the earlier
// history, the system prompt and the tools to the server. uni-switch keeps
// no thread: it routes each request on its own, often to an upstream
// that never saw the first, and a continue it passed on reached the
// model as the latest turn alone, which answered as if the conversation
// had just begun (#2480, zane668; Vincentzyx's 50,381 input tokens one
// turn and 2,034 the next).
//
// So a request that names a thread is refused before it is routed, as
// Anthropic refuses one a thread can't serve: HTTP 400 with
// error.details.error_code thread_unsupported_request. Claude Code
// (2.1.316, its tether's "unsupported_request") then sends the same turn
// again stateless, with the whole history, system and tools, and keeps
// that thread stateless on that model for the rest of the session.

// threadUnsupportedCode is the code Claude Code reads from
// error.details.error_code.
const threadUnsupportedCode = "thread_unsupported_request"

// namesThread says a Messages request names a thread: a top-level
// "thread" that is not null, of any type, create as well as continue (a
// create would be followed by continues).
func namesThread(body []byte) bool {
	t := gjson.GetBytes(body, "thread")
	return t.Exists() && t.Type != gjson.Null
}

// refuseThread answers a request that names a thread as Anthropic's API
// answers one it can't serve on a thread.
func refuseThread(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, map[string]any{"type": "error", "error": map[string]any{
		"type":    "invalid_request_error",
		"message": "Message Threads are not supported by this gateway; send the full conversation without thread.",
		"details": map[string]any{"error_code": threadUnsupportedCode},
	}})
}
