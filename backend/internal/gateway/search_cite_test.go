package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// citedBlock is a text block of an Anthropic reply, with its citations.
type citedBlock struct {
	Text      string
	Citations []map[string]any
}

// textBlocksOf are an Anthropic reply's text blocks, from its stream or
// its body, and the types of all its blocks in order.
func textBlocksOf(t *testing.T, body string, stream bool) ([]citedBlock, []string) {
	t.Helper()
	var blocks []citedBlock
	var types []string
	if !stream {
		var out struct {
			Content []struct {
				Type      string           `json:"type"`
				Text      string           `json:"text"`
				Citations []map[string]any `json:"citations"`
			} `json:"content"`
		}
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatalf("%v: %s", err, body)
		}
		for _, c := range out.Content {
			types = append(types, c.Type)
			if c.Type == "text" {
				blocks = append(blocks, citedBlock{c.Text, c.Citations})
			}
		}
		return blocks, types
	}
	at := map[float64]int{} // a stream's block index, to its text block
	for _, ev := range events(body) {
		switch ev["type"] {
		case "content_block_start":
			b := ev["content_block"].(map[string]any)
			types = append(types, b["type"].(string))
			if b["type"] == "text" {
				at[ev["index"].(float64)] = len(blocks)
				blocks = append(blocks, citedBlock{})
			}
		case "content_block_delta":
			i, ok := at[ev["index"].(float64)]
			if !ok {
				continue
			}
			d := ev["delta"].(map[string]any)
			switch d["type"] {
			case "text_delta":
				blocks[i].Text += d["text"].(string)
			case "citations_delta":
				// a citation comes before the text it is for, as
				// Anthropic's stream sends it
				if blocks[i].Text != "" {
					t.Fatalf("citation after the block's text: %s", body)
				}
				blocks[i].Citations = append(blocks[i].Citations, d["citation"].(map[string]any))
			}
		}
	}
	return blocks, types
}

// The pages a search uni-switch ran found are cited in the answer's text as
// Anthropic's own web search cites them (H20 on Discord: text blocks came
// back with no citations): each line naming a page's URL is a text block
// whose citations are web_search_result_location entries, the lines
// between them a block with none, streamed or not.
func TestSearchCitedAsAnthropics(t *testing.T) {
	var mu sync.Mutex
	rounds := 0
	search := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			io.WriteString(w, `{"data":[{"id":"claude-haiku-4-5"}]}`)
		case "/v1/messages":
			// Anthropic's reply, its text citing a page as it does
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"s","type":"message","role":"assistant","model":"claude-haiku-4-5","content":[
				{"type":"server_tool_use","id":"srv","name":"web_search","input":{"query":"go"}},
				{"type":"web_search_tool_result","tool_use_id":"srv","content":[
					{"type":"web_search_result","title":"Go downloads","url":"https://go.dev/dl/","encrypted_content":"EqoBCkYIBxgC","page_age":"1 day ago"},
					{"type":"web_search_result","title":"Release History","url":"https://go.dev/doc/devel/release","encrypted_content":"EqoBCkYIBxgD"}]},
				{"type":"text","text":"The latest Go is 1.27.1.","citations":[{"type":"web_search_result_location","url":"https://go.dev/dl/","title":"Go downloads","encrypted_index":"Eo8BCioIBxgC","cited_text":"go1.27.1 (released 2026-10-01) includes security fixes."}]}],
				"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":5}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer search.Close()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		mu.Lock()
		rounds++
		n := rounds
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if n%2 == 1 {
			io.WriteString(w, sse(`data: {"id":"c","choices":[{"index":0,"delta":{"role":"assistant","content":"Let me check."}}]}`,
				`data: {"id":"c","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"latest go\"}"}}]}}]}`,
				`data: {"id":"c","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":30,"completion_tokens":7}}`,
				`data: [DONE]`))
			return
		}
		// the answer, an address cut across chunks, one line naming a
		// page the search didn't find
		io.WriteString(w, sse(`data: {"id":"d","choices":[{"index":0,"delta":{"role":"assistant","content":"Go 1.27.1 is out ([Go downloads](https://go.d"}}]}`,
			`data: {"id":"d","choices":[{"index":0,"delta":{"content":"ev/dl/)).\nIt fixes bugs; see https://example.com/notes.\n\n- History: https://go.dev/doc/devel/release"}}]}`,
			`data: {"id":"d","choices":[{"index":0,"delta":{"content":"。"}}]}`,
			`data: {"id":"d","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
			`data: [DONE]`))
	}))
	defer model.Close()

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	setHome(t, t.TempDir())
	hosts := searchHosts[provider.Anthropic]
	searchHosts[provider.Anthropic] = append(hosts, provider.HostOf(search.URL))
	defer func() { searchHosts[provider.Anthropic] = hosts }()
	for _, p := range []provider.Provider{
		{ID: "srch", Name: "Search", Key: "k", Anthropic: search.URL},
		{ID: "deep", Name: "Deep", Key: "k", Chat: model.URL, Models: []string{"deepseek-v4.1-flash"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	p, _ := provider.Find("srch")
	if _, err := p.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}

	dl := map[string]any{"type": "web_search_result_location", "url": "https://go.dev/dl/", "title": "Go downloads",
		"encrypted_index": "", "cited_text": "go1.27.1 (released 2026-10-01) includes security fixes."}
	release := map[string]any{"type": "web_search_result_location", "url": "https://go.dev/doc/devel/release", "title": "Release History",
		"encrypted_index": "", "cited_text": ""}
	want := []citedBlock{
		{Text: "Let me check."},
		{Text: "\n\nGo 1.27.1 is out ([Go downloads](https://go.dev/dl/)).\n", Citations: []map[string]any{dl}},
		{Text: "It fixes bugs; see https://example.com/notes.\n"},
		{Text: "\n- History: https://go.dev/doc/devel/release。", Citations: []map[string]any{release}},
	}
	for _, stream := range []bool{true, false} {
		t.Run(map[bool]string{true: "stream", false: "body"}[stream], func(t *testing.T) {
			body := `{"model":"deep/deepseek-v4.1-flash","max_tokens":1000,"stream":` + map[bool]string{true: "true", false: "false"}[stream] + `,
				"messages":[{"role":"user","content":"What is the latest Go?"}],
				"tools":[{"type":"web_search_20250305","name":"web_search","max_uses":8}]}`
			rec := httptest.NewRecorder()
			New().Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body)))
			if rec.Code != 200 {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
			blocks, types := textBlocksOf(t, rec.Body.String(), stream)
			if !reflect.DeepEqual(types, []string{"text", "server_tool_use", "web_search_tool_result", "text", "text", "text"}) {
				t.Fatalf("blocks %v\n%s", types, rec.Body.String())
			}
			if !reflect.DeepEqual(blocks, want) {
				t.Fatalf("text blocks\n got %#v\nwant %#v\n%s", blocks, want, rec.Body.String())
			}
		})
	}
}

// A reply with no search in it is said as before: one text block, no
// citations, though it names an address.
func TestNoSearchNoCitations(t *testing.T) {
	text := "See https://go.dev/dl/.\nAnd more.\n"
	var out struct {
		Content []map[string]any `json:"content"`
	}
	json.Unmarshal(renderAnthropic(Result{Parts: []Part{{Kind: Text, Text: text}}}, "m"), &out)
	if !reflect.DeepEqual(out.Content, []map[string]any{{"type": "text", "text": text}}) {
		t.Fatalf("%v", out.Content)
	}
	rec := httptest.NewRecorder()
	e := &anthropicEncoder{w: newSSEWriter(rec), model: "m"}
	e.event(Event{Kind: KText, Text: "See https://go.dev/dl/."})
	e.event(Event{Kind: KText, Text: "\nAnd more.\n"})
	e.finish()
	blocks, _ := textBlocksOf(t, rec.Body.String(), true)
	if !reflect.DeepEqual(blocks, []citedBlock{{Text: text}}) || strings.Contains(rec.Body.String(), "citation") {
		t.Fatalf("%#v\n%s", blocks, rec.Body.String())
	}
}

// A URL is cited only whole: a longer path, a query or another host is
// not the page found.
func TestCitesOnlyTheWholeURL(t *testing.T) {
	hits := []Hit{{Title: "A", URL: "https://x.dev/a/"}, {Title: "B", URL: "https://x.dev/b"}}
	for line, want := range map[string][]string{
		"https://x.dev/a":                      {"https://x.dev/a/"},
		"(https://x.dev/a/), https://x.dev/b.": {"https://x.dev/a/", "https://x.dev/b"},
		"**https://x.dev/b**":                  {"https://x.dev/b"},
		"https://x.dev/b then https://x.dev/a": {"https://x.dev/b", "https://x.dev/a/"},
		"https://x.dev/a.html":                 nil,
		"https://x.dev/bb":                     nil,
		"https://x.dev/b?q=1":                  nil,
		"https://x.dev/b_c":                    nil,
		"no address":                           nil,
	} {
		var got []string
		for _, c := range citesOf(line, hits) {
			got = append(got, c["url"].(string))
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q cites %v, want %v", line, got, want)
		}
	}
}
