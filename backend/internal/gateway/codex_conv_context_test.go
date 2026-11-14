package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Two Codex conversations on one model id at once, a long one and a new
// short one: each request's context is its own conversation's, in the
// usage Codex is handed back (what Codex compacts by) and in the route the
// Routing page draws its context window from. Nothing of the long one is
// added to the short one's, however their requests interleave (zhemu on
// Discord took Routing's context for the sum of every conversation on the
// model).
func TestCodexConversationsOnOneModelKeepTheirOwnContext(t *testing.T) {
	fresh(t)
	const longIn, shortIn = 90000, 5000
	var mu sync.Mutex
	seen := map[string]int{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		in, who := shortIn, "short"
		if strings.Contains(string(b), "LONG-CONVERSATION") {
			in, who = longIn, "long"
		}
		mu.Lock()
		seen[who]++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`data: {"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"}}]}`,
			`data: {"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			fmt.Sprintf(`data: {"id":"x","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":%d,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":%d}}}`, in, in/2),
			`data: [DONE]`,
		))
	}))
	defer up.Close()
	if err := provider.Save(provider.Provider{ID: "wb", Name: "WB", Key: "k", Models: []string{"flash"}, Chat: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("wb", up.URL+"/v1", []catalog.Model{{ID: "flash", Context: 200000}}); err != nil {
		t.Fatal(err)
	}
	s := New()
	h := s.Handler()

	ask := func(session, words string) (int, map[string]any) {
		body, _ := json.Marshal(map[string]any{
			"model": "wb/flash", "stream": true, "store": false, "prompt_cache_key": session,
			"input": []any{map[string]any{"type": "message", "role": "user",
				"content": []any{map[string]any{"type": "input_text", "text": words}}}},
		})
		req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(string(body)))
		req.Header.Set("session_id", session)
		req.Header.Set("originator", "codex_desktop")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var usage map[string]any
		for _, line := range strings.Split(rec.Body.String(), "\n") {
			data, ok := strings.CutPrefix(line, "data: ")
			if !ok || !strings.Contains(data, `"response.completed"`) {
				continue
			}
			var ev struct {
				Response struct {
					Usage map[string]any `json:"usage"`
				} `json:"response"`
			}
			json.Unmarshal([]byte(data), &ev)
			usage = ev.Response.Usage
		}
		return rec.Code, usage
	}

	const rounds = 6
	var wg sync.WaitGroup
	type got struct {
		session string
		code    int
		input   float64
	}
	results := make(chan got, 2*rounds)
	for _, c := range []struct{ session, words string }{
		{"thread-long", "LONG-CONVERSATION refactor the parser"},
		{"thread-new", "a new chat: say hi"},
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range rounds {
				code, u := ask(c.session, c.words)
				in, _ := u["input_tokens"].(float64)
				results <- got{c.session, code, in}
			}
		}()
	}
	wg.Wait()
	close(results)
	for r := range results {
		want := float64(shortIn)
		if r.session == "thread-long" {
			want = longIn
		}
		if r.code != 200 || r.input != want {
			t.Errorf("%s: status %d, Codex told input_tokens %v, want %v", r.session, r.code, r.input, want)
		}
	}
	if seen["long"] != rounds || seen["short"] != rounds {
		t.Fatalf("vendor saw %v", seen)
	}

	rs := s.Trace(t.Context(), 0, 0).Routes
	if len(rs) != 2*rounds {
		t.Fatalf("%d routes", len(rs))
	}
	for _, r := range rs {
		want := shortIn
		if r.Session == "thread-long" {
			want = longIn
		}
		if r.Prompt == nil || !r.Prompt.Counted || r.Prompt.Tokens != want || r.Prompt.Window != 200000 {
			t.Errorf("route %d (%s): prompt %+v, want %d of 200000", r.ID, r.Session, r.Prompt, want)
		}
	}
}
