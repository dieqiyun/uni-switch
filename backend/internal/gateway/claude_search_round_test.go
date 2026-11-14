package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// strictClaude is a Messages upstream that checks the thinking blocks of
// the latest assistant message as Claude on Bedrock does: each must be
// one Claude wrote, at the place in its message it wrote it, or the
// request is refused 400 as Bedrock refused oreoft's (Discord). The
// first request is answered with thinking, a line and uni-switch's
// web_search call; the one with the search's result with more thinking
// and the answer; any later one with a line.
type strictClaude struct {
	mu      sync.Mutex
	sent    []string
	wrote   [][]map[string]any // each message it answered with
	refused int
}

func (u *strictClaude) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var q struct {
		Messages []struct {
			Role    string           `json:"role"`
			Content []map[string]any `json:"content"`
		} `json:"messages"`
	}
	json.Unmarshal(b, &q)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.sent = append(u.sent, string(b))
	thinking := func(blocks []map[string]any) map[int]string {
		out := map[int]string{}
		for i, c := range blocks {
			if c["type"] == "thinking" || c["type"] == "redacted_thinking" {
				delete(c, "cache_control")
				j, _ := json.Marshal(c)
				out[i] = string(j)
			}
		}
		return out
	}
	assistants := 0
	for i := len(q.Messages) - 1; i >= 0; i-- {
		if q.Messages[i].Role != "assistant" {
			continue
		}
		assistants++
		if assistants > 1 {
			break
		}
		got := thinking(q.Messages[i].Content)
		if len(got) == 0 {
			continue
		}
		ok := false
		for _, m := range u.wrote {
			want := thinking(m)
			same := len(want) == len(got)
			for k, v := range want {
				same = same && got[k] == v
			}
			ok = ok || same
		}
		if !ok {
			u.refused++
			w.WriteHeader(400)
			io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"messages.`+itoa(i)+`.content.1: `+"`thinking`"+` or `+"`redacted_thinking`"+` blocks in the latest assistant message cannot be modified. These blocks must remain as they were in the original response."}}`)
			return
		}
	}
	last := q.Messages[len(q.Messages)-1]
	result := false
	for _, c := range last.Content {
		result = result || c["type"] == "tool_result"
	}
	var blocks []map[string]any
	switch {
	case assistants == 0:
		blocks = []map[string]any{
			{"type": "thinking", "thinking": "I should look this up.", "signature": "EqA-before-search"},
			{"type": "text", "text": "Let me check."},
			{"type": "tool_use", "id": "toolu_ws1", "name": "web_search", "input": map[string]any{"query": "latest go"}},
		}
	case result:
		blocks = []map[string]any{
			{"type": "thinking", "thinking": "The page says 1.27.1.", "signature": "EqB-after-search"},
			{"type": "text", "text": "Go 1.27.1 is out."},
		}
	default:
		blocks = []map[string]any{{"type": "text", "text": "You're welcome."}}
	}
	u.wrote = append(u.wrote, blocks)
	w.Header().Set("Content-Type", "text/event-stream")
	send := func(v map[string]any) {
		j, _ := json.Marshal(v)
		io.WriteString(w, "event: "+v["type"].(string)+"\ndata: "+string(j)+"\n\n")
	}
	send(map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_s", "type": "message", "role": "assistant", "model": "claude-sonnet-4-5", "content": []any{}, "usage": map[string]any{"input_tokens": 10, "output_tokens": 1}}})
	stop := "end_turn"
	for i, c := range blocks {
		switch c["type"] {
		case "thinking":
			send(map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "thinking", "thinking": "", "signature": ""}})
			send(map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "thinking_delta", "thinking": c["thinking"]}})
			send(map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "signature_delta", "signature": c["signature"]}})
		case "text":
			send(map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "text", "text": ""}})
			send(map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "text_delta", "text": c["text"]}})
		case "tool_use":
			stop = "tool_use"
			in, _ := json.Marshal(c["input"])
			send(map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "tool_use", "id": c["id"], "name": c["name"], "input": map[string]any{}}})
			send(map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(in)}})
		}
		send(map[string]any{"type": "content_block_stop", "index": i})
	}
	send(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stop, "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 9}})
	send(map[string]any{"type": "message_stop"})
}

// codexSearchAsk is codexLoopAsk with Codex's web_search tool offered.
func codexSearchAsk(stream bool, input string) string {
	return strings.Replace(codexLoopAsk(stream, input), `"tools":[`, `"tools":[{"type":"web_search"},`, 1)
}

// Codex on a Claude model that uni-switch searches for (oreoft on Discord):
// the turn after a search is sent with Claude's thinking where Claude
// wrote it, the search round put back between the two messages of the
// reply, so an upstream that checks the latest assistant message's
// thinking (Claude on Bedrock) takes it, streamed or not. Before, the
// reply went back as one message, its second thinking block moved, and
// the strict upstream refused every retry.
func TestClaudeThinkingAroundUniSwitchSearchGoesBackAsWritten(t *testing.T) {
	for _, stream := range []bool{true, false} {
		fresh(t)
		t.Setenv("HOME", t.TempDir()) // no signed-in agent searches
		t.Setenv("USERPROFILE", os.Getenv("HOME"))
		up := &strictClaude{}
		srv := httptest.NewServer(up)
		t.Cleanup(srv.Close)
		if err := provider.Save(provider.Provider{ID: "a", Name: "Bedrock line", Key: "k", Anthropic: srv.URL, Models: []string{"claude-sonnet-4-5"}}); err != nil {
			t.Fatal(err)
		}
		apis := newSearchAPIs(t)
		if err := provider.SetSearchAPI(apis.api("tavily", "tvly-k")); err != nil {
			t.Fatal(err)
		}
		s := New()
		user := `{"type":"message","role":"user","content":[{"type":"input_text","text":"What is the latest Go?"}]}`
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(codexSearchAsk(stream, "["+user+"]"))))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Go 1.27.1 is out.") {
			t.Fatalf("stream=%t turn 1: %d %s", stream, rec.Code, rec.Body)
		}
		input := []string{user}
		var kinds []string
		for _, it := range replyItems(t, rec.Body.Bytes(), stream) {
			delete(it, "status")
			if it["type"] == "reasoning" {
				delete(it, "id")
			}
			kinds = append(kinds, it["type"].(string))
			b, _ := json.Marshal(it)
			input = append(input, string(b))
		}
		if strings.Join(kinds, ",") != "reasoning,message,web_search_call,reasoning,message" {
			t.Fatalf("stream=%t: turn 1 items %v", stream, kinds)
		}
		input = append(input, `{"type":"message","role":"user","content":[{"type":"input_text","text":"Thanks"}]}`)
		rec = httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(codexSearchAsk(stream, "["+strings.Join(input, ",")+"]"))))
		up.mu.Lock()
		refused, last := up.refused, up.sent[len(up.sent)-1]
		up.mu.Unlock()
		if rec.Code != 200 || refused > 0 || !strings.Contains(rec.Body.String(), "You're welcome.") {
			t.Fatalf("stream=%t turn 2: %d (refused %d) %s\nsent %s", stream, rec.Code, refused, rec.Body, last)
		}
		// the round is back: uni-switch's call, and a result naming the query
		var q struct {
			Messages []struct {
				Role    string           `json:"role"`
				Content []map[string]any `json:"content"`
			} `json:"messages"`
		}
		json.Unmarshal([]byte(last), &q)
		var shape []string
		for _, m := range q.Messages {
			var types []string
			for _, c := range m.Content {
				types = append(types, c["type"].(string))
				if c["type"] == "tool_result" && !strings.Contains(c["content"].(string), "latest go") {
					t.Fatalf("stream=%t: result %v", stream, c)
				}
			}
			shape = append(shape, m.Role+":"+strings.Join(types, "+"))
		}
		want := "user:text|assistant:thinking+text+tool_use|user:tool_result|assistant:thinking+text|user:text"
		if strings.Join(shape, "|") != want {
			t.Fatalf("stream=%t: sent %s, want %s\n%s", stream, strings.Join(shape, "|"), want, last)
		}
	}
}

// A search beside no thinking of Claude's (another model's turn, a reply
// without signed thinking) is left out, as before: no call is made up for
// it.
func TestSearchWithoutClaudeThinkingStaysOut(t *testing.T) {
	body := `{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"q"}]},` +
		`{"type":"reasoning","summary":[{"type":"summary_text","text":"thought"}]},` +
		`{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","query":"latest go"}},` +
		`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Go 1.27.1."}]}]}`
	r, err := parseResponses([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Messages) != 2 || r.Messages[1].Role != "assistant" || len(r.Messages[1].Parts) != 2 {
		t.Fatalf("messages %+v", r.Messages)
	}
	for _, p := range r.Messages[1].Parts {
		if p.Kind == ToolCall || p.Kind == Search {
			t.Fatalf("made up %+v", p)
		}
	}
}
