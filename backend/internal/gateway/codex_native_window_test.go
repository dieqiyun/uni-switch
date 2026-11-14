package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// netube on Discord: a Codex Plus account's turn showed a 922K context
// window on Routing, models.dev's input limit for gpt-5.5 (OpenAI's API's),
// where Codex holds the conversation to the 272K its ChatGPT list gives.
// A turn Codex sends through uni-switch's Codex backend takes Codex's window;
// one the user set on the codex provider comes first; a model of another
// provider, of the same id, keeps models.dev's.
func TestCodexNativeTurnTakesCodexWindow(t *testing.T) {
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Content-Type"] = nil // as the ChatGPT backend sends it
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"r1"}}`,
			`data: {"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":9000,"output_tokens":2}}}`))
	})
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	if err := os.WriteFile(catalog.CachePath(), []byte(`{"openai":{"id":"openai","models":{
	  "gpt-5.5":{"id":"gpt-5.5","limit":{"context":1050000,"input":922000,"output":128000}}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	// Codex CLI's own list, as the ChatGPT backend gave it for the plan
	codexChatGPTAuth(t, "plus@example.com") // the codex subscription, for its window
	codexHome := os.Getenv("CODEX_HOME")
	if err := os.WriteFile(filepath.Join(codexHome, "models_cache.json"), []byte(`{"etag":"\"e1\"","client_version":"0.159.0","models":[
	  {"slug":"gpt-5.5","display_name":"GPT-5.5","priority":1,"visibility":"list","context_window":272000,"max_context_window":272000}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New()
	h := s.Handler()
	turn := func() *Route {
		t.Helper()
		before := len(s.Trace(t.Context(), 0, 0).Routes)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(`{"model":"gpt-5.5","stream":true,"input":[
		  {"type":"message","role":"user","content":[{"type":"input_text","text":"refactor the parser"}]}]}`))
		req.Header.Set("Authorization", "Bearer chatgpt-token")
		req.Header.Set("chatgpt-account-id", "acct-1")
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		rs := s.Trace(t.Context(), 0, 0).Routes
		if len(rs) != before+1 {
			t.Fatalf("%d routes, want %d", len(rs), before+1)
		}
		for i := range rs {
			if rs[i].ID == maxRouteID(rs) {
				return &rs[i]
			}
		}
		return nil
	}

	if r := turn(); r.Prompt == nil || r.Prompt.Window != 272000 {
		t.Errorf("Codex's own gpt-5.5: prompt %+v, want Codex's window 272000, not models.dev's 922000", r.Prompt)
	}
	// the window uni-switch's Codex subscription's list gives comes before
	// Codex CLI's cache
	if err := catalog.SaveLive("codex", provider.CodexBase, []catalog.Model{{ID: "gpt-5.5", Context: 258000}}); err != nil {
		t.Fatal(err)
	}
	if r := turn(); r.Prompt == nil || r.Prompt.Window != 258000 {
		t.Errorf("with the subscription's list: prompt %+v, want 258000", r.Prompt)
	}
	// the user's own window on the codex provider is the one Codex is told
	if err := provider.Save(provider.Provider{ID: "codex", Contexts: map[string]int{"gpt-5.5": 400000}}); err != nil {
		t.Fatal(err)
	}
	if r := turn(); r.Prompt == nil || r.Prompt.Window != 400000 {
		t.Errorf("with a window set: prompt %+v, want the user's 400000", r.Prompt)
	}

	// another provider's gpt-5.5, on OpenAI's API, keeps models.dev's
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`data: {"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"}}]}`,
			`data: {"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			`data: {"id":"x","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":9000,"completion_tokens":5}}`,
			`data: [DONE]`))
	}))
	defer up.Close()
	if err := provider.Save(provider.Provider{ID: "oai", Name: "OAI", Key: "k", Models: []string{"gpt-5.5"}, Chat: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	before := len(s.Trace(t.Context(), 0, 0).Routes)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"oai/gpt-5.5","stream":true,"input":[
	  {"type":"message","role":"user","content":[{"type":"input_text","text":"refactor the parser"}]}]}`))
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("oai: %d %s", rec.Code, rec.Body)
	}
	rs := s.Trace(t.Context(), 0, 0).Routes
	if len(rs) != before+1 {
		t.Fatalf("%d routes, want %d", len(rs), before+1)
	}
	for _, r := range rs {
		if r.ID == maxRouteID(rs) && (r.Prompt == nil || r.Prompt.Window != 922000) {
			t.Errorf("another provider's gpt-5.5: prompt %+v, want models.dev's 922000", r.Prompt)
		}
	}
}

func maxRouteID(rs []Route) int64 {
	var n int64
	for _, r := range rs {
		n = max(n, r.ID)
	}
	return n
}
