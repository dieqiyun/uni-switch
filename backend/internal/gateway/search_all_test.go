package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// Settings' SearchAll (cybercaius on Discord: "能否强制支持联网搜索的供应商
// 模型走代搜 API"): a provider that searches the web by itself is sent the
// client's web_search as sent while it is off; on, uni-switch's search APIs
// answer it and the provider is sent no search tool of its own. With no
// search uni-switch can run, the provider keeps its own: the client's search
// isn't lost.
func TestSearchAllTakesTheSearchFromAProviderThatSearches(t *testing.T) {
	var mu sync.Mutex
	var got []string
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"data":[{"id":"claude-haiku-4-5"}]}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var q map[string]json.RawMessage
		json.Unmarshal(b, &q)
		mu.Lock()
		got = append(got, string(q["stream"])+" "+string(q["tools"]))
		mu.Unlock()
		if string(q["stream"]) == "true" && strings.Contains(string(q["tools"]), "input_schema") && !strings.Contains(string(b), "tool_result") {
			// uni-switch's own web_search tool: the model calls it
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, sse(`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"claude-haiku-4-5","content":[],"usage":{"input_tokens":5}}}`,
				`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"web_search","input":{}}}`,
				`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"latest go release\"}"}}`,
				`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":0}`,
				`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
				`event: message_stop`+"\n"+`data: {"type":"message_stop"}`))
			return
		}
		if string(q["stream"]) == "true" {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, sse(`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"claude-haiku-4-5","content":[],"usage":{"input_tokens":5}}}`,
				`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Go 1.27.1 is out."}}`,
				`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":0}`,
				`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
				`event: message_stop`+"\n"+`data: {"type":"message_stop"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"m","type":"message","role":"assistant","model":"claude-haiku-4-5","content":[{"type":"text","text":"Go 1.27.1 is out."}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":5}}`)
	}))
	defer relay.Close()
	var asked atomic.Int32
	tavily := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"query":"x","results":[{"title":"Go downloads","url":"https://go.dev/dl/","content":"go1.27.1","score":0.9}]}`)
	}))
	defer tavily.Close()

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	setHome(t, t.TempDir()) // no signed-in agent searches for it
	p := provider.Provider{ID: "relay", Name: "Relay", Key: "k", Anthropic: relay.URL, Models: []string{"claude-haiku-4-5"}, Searches: true}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	body := `{"model":"relay/claude-haiku-4-5","max_tokens":1000,"stream":false,
		"messages":[{"role":"user","content":"What is the latest Go release? Search the web."}],
		"tools":[{"type":"web_search_20250305","name":"web_search","max_uses":8}],
		"metadata":{"user_id":"u"}}`
	ask := func() (int, string) {
		t.Helper()
		mu.Lock()
		got = nil
		mu.Unlock()
		asked.Store(0)
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		mu.Lock()
		defer mu.Unlock()
		if len(got) == 0 {
			t.Fatal("the provider was asked nothing")
		}
		return int(asked.Load()), got[0]
	}
	all := func(on bool) {
		t.Helper()
		st := settings.Load()
		st.SearchAll = on
		if err := settings.Save(st); err != nil {
			t.Fatal(err)
		}
	}

	// on, but uni-switch can't search: the provider keeps its own, relayed as
	// sent (not built again, which streams)
	all(true)
	if n, tools := ask(); n != 0 || !strings.HasPrefix(tools, "false ") || !strings.Contains(tools, "web_search_20250305") {
		t.Fatalf("no search of uni-switch's: %d asks, provider sent %s", n, tools)
	}

	forgetSearchKeys()
	if err := provider.SetSearchAPI(provider.SearchAPI{Vendor: "tavily", Key: "tvly-good-0001", URL: tavily.URL}); err != nil {
		t.Fatal(err)
	}
	all(false)
	if n, tools := ask(); n != 0 || !strings.Contains(tools, "web_search_20250305") {
		t.Fatalf("off: %d search API asks, provider sent %s", n, tools)
	}

	all(true)
	if n, tools := ask(); n == 0 || strings.Contains(tools, "web_search_20250305") {
		t.Fatalf("on: %d search API asks, provider sent %s", n, tools)
	}
	if !uniSwitchSearches(context.Background(), p) || uniSwitchSearches(context.WithValue(context.Background(), searchingKey{}, true), p) {
		t.Error("uni-switch's own searcher request lost the provider's search, or a client's kept it")
	}
}
