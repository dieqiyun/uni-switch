package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/budget"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// mcpPost sends one JSON-RPC body to the web search MCP server through h,
// from addr, with hdr as name, value pairs.
func mcpPost(h http.Handler, addr, body string, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, SearchMCPPath, strings.NewReader(body))
	r.RemoteAddr = addr
	r.Host = "127.0.0.1:3425"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

type mcpReply struct {
	ID     json.RawMessage `json:"id"`
	Result struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct{ Name string }
		Tools           []struct {
			Name        string
			InputSchema struct {
				Required []string
			}
		}
		Content []struct{ Type, Text string }
		IsError bool
	}
	Error *struct {
		Code    int
		Message string
	}
}

func readMCP(t *testing.T, w *httptest.ResponseRecorder) mcpReply {
	t.Helper()
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("%d %s: %s", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	var m mcpReply
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("%v: %s", err, w.Body.String())
	}
	return m
}

const mcpSearchCall = `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"web_search","arguments":{"query":"latest go"}}}`

// uni-switch's web search is an MCP server of its own on the gateway (Kayphoon
// on Discord): an MCP client initializes, lists its web_search tool and
// calls it, and the search goes where a model's web_search would go — here
// the search API the user set up — the pages it found in the tool's text.
func TestSearchMCPServesWebSearch(t *testing.T) {
	fresh(t)
	apis := newSearchAPIs(t)
	if err := provider.SetSearchAPI(apis.api("tavily", "tvly-k")); err != nil {
		t.Fatal(err)
	}
	h := localGuard(New().Handler()) // as the real server has it
	const here = "127.0.0.1:50000"

	init := readMCP(t, mcpPost(h, here, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"cursor","version":"1"}}}`))
	if init.Error != nil || init.Result.ProtocolVersion != "2025-03-26" || init.Result.ServerInfo.Name != "uni-switch-web-search" || string(init.ID) != "1" {
		t.Fatalf("initialize: %+v", init)
	}
	// a version it doesn't speak is answered in its newest
	if v := readMCP(t, mcpPost(h, here, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2099-01-01"}}`)).Result.ProtocolVersion; v != searchMCPVersions[0] {
		t.Fatalf("unknown version answered in %q", v)
	}
	if w := mcpPost(h, here, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); w.Code != http.StatusAccepted || w.Body.Len() != 0 {
		t.Fatalf("a notification: %d %q", w.Code, w.Body.String())
	}
	list := readMCP(t, mcpPost(h, here, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`))
	if len(list.Result.Tools) != 2 || list.Result.Tools[0].Name != "web_search" || !slices.Equal(list.Result.Tools[0].InputSchema.Required, []string{"query"}) ||
		list.Result.Tools[1].Name != "web_fetch" || !slices.Equal(list.Result.Tools[1].InputSchema.Required, []string{"url"}) {
		t.Fatalf("tools/list: %+v", list)
	}
	call := readMCP(t, mcpPost(h, here, mcpSearchCall, "User-Agent", "Cursor/1.7"))
	if call.Result.IsError || len(call.Result.Content) != 1 || call.Result.Content[0].Type != "text" ||
		!strings.Contains(call.Result.Content[0].Text, "Go downloads — https://go.dev/dl/") || !strings.Contains(call.Result.Content[0].Text, "go1.27.1 is the latest release.") {
		t.Fatalf("tools/call: %+v", call)
	}
	if strings.Join(apis.asked, "|") != "tavily: latest go" {
		t.Fatalf("asked %q", apis.asked)
	}

	// a batch is answered as one, its notification left out
	w := mcpPost(h, here, `[{"jsonrpc":"2.0","id":"a","method":"ping"},{"jsonrpc":"2.0","method":"notifications/initialized"},{"jsonrpc":"2.0","id":"b","method":"resources/list"}]`)
	var batch []mcpReply
	if err := json.Unmarshal(w.Body.Bytes(), &batch); err != nil || len(batch) != 2 || batch[0].Error != nil || batch[1].Error == nil || batch[1].Error.Code != -32601 {
		t.Fatalf("batch: %s", w.Body.String())
	}
	// another tool, no query, a body that isn't JSON
	if m := readMCP(t, mcpPost(h, here, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"fetch","arguments":{}}}`)); m.Error == nil || m.Error.Code != -32602 {
		t.Fatalf("unknown tool: %+v", m)
	}
	if m := readMCP(t, mcpPost(h, here, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"web_search","arguments":{"query":"  "}}}`)); !m.Result.IsError {
		t.Fatalf("no query: %+v", m)
	}
	if w := mcpPost(h, here, `nope`); w.Code != 400 || !strings.Contains(w.Body.String(), "-32700") {
		t.Fatalf("not JSON: %d %s", w.Code, w.Body.String())
	}
	// no stream of its own to GET
	r := httptest.NewRequest(http.MethodGet, SearchMCPPath, nil)
	r.RemoteAddr = here
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "POST" {
		t.Fatalf("GET: %d %v", rec.Code, rec.Header())
	}
}

// Nothing to search with: the call says so as the tool's error, with where
// to set a search up, rather than failing the request.
func TestSearchMCPWithNoSearcher(t *testing.T) {
	fresh(t)
	m := readMCP(t, mcpPost(localGuard(New().Handler()), "127.0.0.1:50000", mcpSearchCall))
	if !m.Result.IsError || len(m.Result.Content) != 1 || !strings.Contains(m.Result.Content[0].Text, "Settings → Models → Web search") {
		t.Fatalf("%+v", m)
	}
}

// The server is a gateway route like the others: another computer reaches
// it only while uni-switch is shared and with an enabled gateway key, a web
// page — one rebound to 127.0.0.1, or one on this computer — is turned
// away, and the search API is asked for none of them.
func TestSearchMCPGuarded(t *testing.T) {
	fresh(t)
	t.Setenv("UNI_SWITCH_ADDR", "")
	t.Setenv("UNI_SWITCH_PUBLIC_URL", "")
	apis := newSearchAPIs(t)
	if err := provider.SetSearchAPI(apis.api("tavily", "tvly-k")); err != nil {
		t.Fatal(err)
	}
	h := localGuard(New().Handler())
	const there = "192.0.2.7:4000"
	if w := mcpPost(h, there, mcpSearchCall); w.Code != http.StatusForbidden {
		t.Fatalf("not shared: %d %s", w.Code, w.Body.String())
	}
	_, secrets := newCaller(t, "Laptop") // shares uni-switch
	if w := mcpPost(h, there, mcpSearchCall); w.Code != http.StatusUnauthorized {
		t.Fatalf("shared, no key: %d %s", w.Code, w.Body.String())
	}
	if w := mcpPost(h, there, mcpSearchCall, "Authorization", "Bearer sk-uni-switch-wrong"); w.Code != http.StatusUnauthorized {
		t.Fatalf("shared, a wrong key: %d %s", w.Code, w.Body.String())
	}
	chrome := []string{"User-Agent", "Mozilla/5.0 (Macintosh) Chrome/140", "Sec-Fetch-Site", "same-origin", "Sec-Fetch-Mode", "cors"}
	r := httptest.NewRequest(http.MethodPost, SearchMCPPath, strings.NewReader(mcpSearchCall))
	r.RemoteAddr = "127.0.0.1:50000"
	r.Host = "rebind.attacker.example:3425"
	for i := 0; i+1 < len(chrome); i += 2 {
		r.Header.Set(chrome[i], chrome[i+1])
	}
	r.Header.Set("Origin", "http://rebind.attacker.example:3425")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a rebound page: %d %s", rec.Code, rec.Body.String())
	}
	if w := mcpPost(h, "127.0.0.1:50000", mcpSearchCall, "Origin", "http://localhost:5173"); w.Code != http.StatusForbidden {
		t.Fatalf("a page on this computer: %d %s", w.Code, w.Body.String())
	}
	if w := mcpPost(h, "127.0.0.1:50000", mcpSearchCall, "Origin", "https://evil.example"); w.Code != http.StatusForbidden {
		t.Fatalf("another site's page: %d %s", w.Code, w.Body.String())
	}
	if len(apis.asked) != 0 {
		t.Fatalf("searched for a refused call: %q", apis.asked)
	}
	m := readMCP(t, mcpPost(h, there, mcpSearchCall, "Authorization", "Bearer "+secrets[0]))
	if m.Result.IsError || !strings.Contains(m.Result.Content[0].Text, "https://go.dev/dl/") {
		t.Fatalf("shared, with its key: %+v", m)
	}
}

// A gateway key past its limit searches no more through the server: the
// call says the limit, and no search is made.
func TestSearchMCPHeldToKeyLimit(t *testing.T) {
	fresh(t)
	budget.Forget()
	pinLimitClock(t)
	calls := limitedUpstream(t, 0)
	apis := newSearchAPIs(t)
	if err := provider.SetSearchAPI(apis.api("tavily", "tvly-k")); err != nil {
		t.Fatal(err)
	}
	keys, secrets := newCaller(t, "Capped")
	setLimit(t, keys[0].ID, &access.Limit{Period: "day", Tokens: 700})
	h := localGuard(New().Handler())
	for range 2 {
		r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatReq))
		r.RemoteAddr = "127.0.0.1:5000"
		r.Header.Set("Authorization", "Bearer "+secrets[0])
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if calls.Load() != 2 {
		t.Fatal(calls.Load())
	}
	apis.asked = nil
	m := readMCP(t, mcpPost(h, "127.0.0.1:5000", mcpSearchCall, "Authorization", "Bearer "+secrets[0]))
	if !m.Result.IsError || !strings.Contains(m.Result.Content[0].Text, `"Capped"`) {
		t.Fatalf("a spent key: %+v", m)
	}
	if len(apis.asked) != 0 {
		t.Fatalf("a spent key searched: %q", apis.asked)
	}
	// without a key, from this computer, it searches
	if m := readMCP(t, mcpPost(h, "127.0.0.1:5000", mcpSearchCall)); m.Result.IsError {
		t.Fatalf("keyless: %+v", m)
	}
}

// A search a provider's model runs for the server is named in the Routing
// view as the agent's MCP call, not as a search for a model of its.
func TestSearchMCPRoutedAsTheAgentsCall(t *testing.T) {
	searchSandbox(t)
	ds := newFakeDeepSeek(t)
	searchesOn(t, provider.Responses, ds.URL)
	save(t, provider.Provider{ID: "ds", Name: "DeepSeek", Key: "k", Chat: ds.URL + "/v1", Responses: ds.URL + "/v1", Models: []string{"deepseek-flash"}})
	st := settings.Load()
	st.Searcher = ""
	settings.Save(st)
	s := New()
	m := readMCP(t, mcpPost(localGuard(s.Handler()), "127.0.0.1:5000", mcpSearchCall, "User-Agent", "claude-code/2.1.0"))
	if m.Result.IsError || !strings.Contains(m.Result.Content[0].Text, "22/17℃") || !strings.Contains(m.Result.Content[0].Text, "https://www.weather.com.cn/") {
		t.Fatalf("%+v", m)
	}
	var searched *Route
	for _, r := range s.Trace(context.Background(), 0, 0).Routes {
		if r.Kind == "web_search" {
			searched = &r
		}
	}
	if searched == nil || searched.For == nil || !searched.For.MCP || searched.For.Agent != "claude-code" {
		t.Fatalf("route %+v", searched)
	}
}
