package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// mantle plays Bedrock's Mantle endpoint (@marktoown on X): Anthropic's
// Messages under /anthropic, and its OpenAI APIs under two prefixes, each
// model at one of them: those in openai at /openai/v1, the rest at /v1. A
// model asked at the other prefix, or one it serves at neither (none), is
// refused as Mantle refuses it.
type mantle struct {
	openai, none map[string]bool
	mu           sync.Mutex
	calls        []bedrockCall
}

func (b *mantle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var m map[string]any
	json.Unmarshal(body, &m)
	model, _ := m["model"].(string)
	b.mu.Lock()
	b.calls = append(b.calls, bedrockCall{r.URL.Path, r.Header.Clone(), model, m})
	b.mu.Unlock()
	if r.URL.Path == "/anthropic/v1/messages" {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"msg_1","model":"`+model+`","usage":{"input_tokens":5}}}`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"from claude"}}`,
			`data: {"type":"content_block_stop","index":0}`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
			`data: {"type":"message_stop"}`))
		return
	}
	rest, openai := strings.CutPrefix(r.URL.Path, "/openai/v1/")
	if !openai {
		var ok bool
		if rest, ok = strings.CutPrefix(r.URL.Path, "/v1/"); !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}
	if b.none[model] || b.openai[model] != openai {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"code":"model_not_supported","message":"model `+"`"+model+"`"+` isn't supported on this route","param":"model","type":"invalid_request_error"}}`)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	switch rest {
	case "chat/completions":
		io.WriteString(w, sse(
			`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"from chat"}}]}`,
			`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
			`data: [DONE]`))
	case "responses":
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"r1","status":"in_progress"}}`,
			`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"m1","role":"assistant","content":[]}}`,
			`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"item_id":"m1","delta":"from responses"}`,
			`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"from responses"}]}}`,
			`data: {"type":"response.completed","response":{"id":"r1","status":"completed","usage":{"input_tokens":5,"output_tokens":2}}}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// paths is where the requests since the n-th were asked.
func (b *mantle) paths(n int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, c := range b.calls[n:] {
		out = append(out, c.path)
	}
	return out
}

func (b *mantle) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.calls)
}

// The Mantle preset at a fake Mantle: each model is asked at the prefix
// that serves it (gpt-oss at /v1, OpenAI's closed GPTs at /openai/v1),
// Claude at /anthropic with the key as x-api-key and without metadata, as
// on Bedrock's runtime. A model whose prefix the id doesn't tell is asked
// again at the other when Mantle refuses it for its route, and from then
// on there; one served at neither gets Mantle's refusal, and is guessed
// again next time.
func TestBedrockMantleRoutes(t *testing.T) {
	fresh(t)
	up := &mantle{
		openai: map[string]bool{"openai.gpt-5.5": true, "newmaker.model-1": true},
		none:   map[string]bool{"ghost.model-1": true},
	}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	p, err := provider.FromPreset("bedrock-mantle")
	if err != nil {
		t.Fatal(err)
	}
	p.Key = "ABSK-test"
	p.Chat, p.Responses, p.Anthropic = srv.URL+"/v1", srv.URL+"/v1", srv.URL+"/anthropic"
	p.Models = []string{"anthropic.claude-opus-4-7", "openai.gpt-oss-120b", "openai.gpt-5.5", "newmaker.model-1", "ghost.model-1"}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	h := New().Handler() // one gateway: what it learns is kept between requests
	ask := func(path, body string) (int, string) {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
		return rec.Code, rec.Body.String()
	}
	chat := func(model string) string {
		return `{"model":"bedrock-mantle/` + model + `","max_tokens":20,"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	}
	same := func(got []string, want ...string) bool {
		return strings.Join(got, " ") == strings.Join(want, " ")
	}

	n := up.count()
	if code, body := ask("/v1/chat/completions", chat("openai.gpt-oss-120b")); code != 200 || !strings.Contains(body, "from chat") {
		t.Fatalf("gpt-oss: %d %s", code, body)
	}
	if got := up.paths(n); !same(got, "/v1/chat/completions") {
		t.Fatalf("gpt-oss asked at %v", got)
	}
	if c := up.calls[n]; c.head.Get("Authorization") != "Bearer ABSK-test" || c.body["max_completion_tokens"] != float64(20) {
		t.Fatalf("gpt-oss: %v %v", c.head, c.body)
	}

	n = up.count()
	if code, body := ask("/v1/responses", `{"model":"bedrock-mantle/openai.gpt-5.5","stream":true,"input":[{"role":"user","content":[{"type":"input_text","text":"hi"}]}]}`); code != 200 || !strings.Contains(body, "from responses") {
		t.Fatalf("gpt-5.5: %d %s", code, body)
	}
	if got := up.paths(n); !same(got, "/openai/v1/responses") {
		t.Fatalf("gpt-5.5 asked at %v", got)
	}

	n = up.count()
	if code, body := ask("/v1/messages", `{"model":"bedrock-mantle/anthropic.claude-opus-4-7","max_tokens":20,"stream":true,"metadata":{"user_id":"{\"device_id\":\"d\"}"},"messages":[{"role":"user","content":"hi"}]}`); code != 200 || !strings.Contains(body, "from claude") {
		t.Fatalf("claude: %d %s", code, body)
	}
	if got := up.paths(n); !same(got, "/anthropic/v1/messages") {
		t.Fatalf("claude asked at %v", got)
	}
	if c := up.calls[n]; c.head.Get("x-api-key") != "ABSK-test" || c.head.Get("Authorization") != "" || c.body["metadata"] != nil {
		t.Fatalf("claude: %v %v", c.head, c.body)
	}

	// a maker the rule doesn't know, served at /openai/v1: asked there
	// again, and remembered
	n = up.count()
	if code, body := ask("/v1/chat/completions", chat("newmaker.model-1")); code != 200 || !strings.Contains(body, "from chat") {
		t.Fatalf("newmaker: %d %s", code, body)
	}
	if got := up.paths(n); !same(got, "/v1/chat/completions", "/openai/v1/chat/completions") {
		t.Fatalf("newmaker asked at %v", got)
	}
	n = up.count()
	if code, body := ask("/v1/chat/completions", chat("newmaker.model-1")); code != 200 || !strings.Contains(body, "from chat") {
		t.Fatalf("newmaker again: %d %s", code, body)
	}
	if got := up.paths(n); !same(got, "/openai/v1/chat/completions") {
		t.Fatalf("newmaker not remembered: %v", got)
	}

	// served at neither: Mantle's refusal for its route, and nothing
	// learned from it
	for range 2 {
		n = up.count()
		code, body := ask("/v1/chat/completions", chat("ghost.model-1"))
		if code != 400 || !strings.Contains(body, "isn't supported on this route") {
			t.Fatalf("ghost: %d %s", code, body)
		}
		if got := up.paths(n); len(got) < 2 || got[0] != "/v1/chat/completions" || got[1] != "/openai/v1/chat/completions" {
			t.Fatalf("ghost asked at %v", got)
		}
	}
}
