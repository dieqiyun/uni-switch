package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// searchAPIs is a fake of every search API uni-switch asks, each on its own
// path and checking its own way of carrying the key; tavily answers 500
// while broken is set.
type searchAPIs struct {
	*httptest.Server
	mu     sync.Mutex
	asked  []string // vendor: query
	broken bool
}

func newSearchAPIs(t *testing.T) *searchAPIs {
	f := &searchAPIs{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var q struct {
			Query string `json:"query"`
		}
		json.Unmarshal(b, &q)
		vendor, key := "", ""
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/tavily/search":
			vendor, key = "tavily", r.Header.Get("Authorization")
		case r.Method == http.MethodGet && r.URL.Path == "/brave/res/v1/web/search":
			vendor, key, q.Query = "brave", r.Header.Get("X-Subscription-Token"), r.URL.Query().Get("q")
		case r.Method == http.MethodPost && r.URL.Path == "/exa/search":
			vendor, key = "exa", r.Header.Get("X-Api-Key")
		case r.Method == http.MethodPost && r.URL.Path == "/firecrawl/v2/search":
			vendor, key = "firecrawl", r.Header.Get("Authorization")
		case r.Method == http.MethodGet && r.URL.Path == "/searxng/search" && r.URL.Query().Get("format") == "json":
			vendor, key, q.Query = "searxng", "none", r.URL.Query().Get("q")
		default:
			http.NotFound(w, r)
			return
		}
		want := map[string]string{"tavily": "Bearer tvly-k", "brave": "brave-k", "exa": "exa-k", "firecrawl": "Bearer fc-k", "searxng": "none"}[vendor]
		if key != want {
			http.Error(w, `{"error":"bad key `+key+`"}`, 401)
			return
		}
		f.mu.Lock()
		f.asked = append(f.asked, vendor+": "+q.Query)
		broken := f.broken
		f.mu.Unlock()
		if vendor == "tavily" && broken {
			http.Error(w, `{"detail":{"error":"down"}}`, 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch vendor {
		case "tavily":
			io.WriteString(w, `{"query":"x","results":[{"title":"Go downloads","url":"https://go.dev/dl/","content":"go1.27.1 is the latest release.","score":0.9}]}`)
		case "brave":
			io.WriteString(w, `{"type":"search","web":{"results":[{"title":"Go &amp; <strong>downloads</strong>","url":"https://go.dev/dl/","description":"The <strong>latest</strong> Go is 1.27.1"}]}}`)
		case "exa":
			io.WriteString(w, `{"results":[{"title":"Go downloads","url":"https://go.dev/dl/","text":"Go 1.27.1 was released."}]}`)
		case "firecrawl":
			io.WriteString(w, `{"success":true,"data":{"web":[{"title":"Go downloads","url":"https://go.dev/dl/","description":"Download Go 1.27.1"}]}}`)
		case "searxng":
			io.WriteString(w, `{"query":"x","results":[{"title":"Go downloads","url":"https://go.dev/dl/","content":"Go 1.27.1 is out"},{"title":"","url":""}]}`)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *searchAPIs) api(vendor, key string) provider.SearchAPI {
	return provider.SearchAPI{Vendor: vendor, Key: key, URL: f.URL + "/" + vendor}
}

// Each search API is asked its own way and read into the pages it found.
func TestSearchAPIsEachAsked(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := newSearchAPIs(t)
	s := New()
	for _, c := range []struct {
		api  provider.SearchAPI
		text string
	}{
		{f.api("tavily", "tvly-k"), "go1.27.1 is the latest release."},
		{f.api("brave", "brave-k"), "The latest Go is 1.27.1"},
		{f.api("exa", "exa-k"), "Go 1.27.1 was released."},
		{f.api("firecrawl", "fc-k"), "Download Go 1.27.1"},
		{f.api("searxng", ""), "Go 1.27.1 is out"},
	} {
		pages, err := s.askSearchAPI(context.Background(), c.api, "latest go")
		if err != nil {
			t.Errorf("%s: %v", c.api.Vendor, err)
			continue
		}
		if len(pages) != 1 || pages[0].URL != "https://go.dev/dl/" || pages[0].Text != c.text || !strings.HasPrefix(pages[0].Title, "Go ") {
			t.Errorf("%s: %+v", c.api.Vendor, pages)
		}
	}
	if pages, _ := s.askSearchAPI(context.Background(), f.api("brave", "brave-k"), "q"); len(pages) == 1 && pages[0].Title != "Go & downloads" {
		t.Errorf("brave's title kept its tags: %q", pages[0].Title)
	}
	if _, err := s.askSearchAPI(context.Background(), f.api("exa", "wrong"), "q"); err == nil || !strings.Contains(err.Error(), "bad key") {
		t.Errorf("a refused key: %v", err)
	}
}

// With no provider that can search, a client's web search is given to the
// model as uni-switch's tool all the same, and answered by the search APIs the
// user set up, in their order: one that fails gives way to the next.
func TestWebSearchBySearchAPI(t *testing.T) {
	var mu sync.Mutex
	var asked [][]map[string]any
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
			Messages []map[string]any `json:"messages"`
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &q)
		mu.Lock()
		asked = append(asked, q.Messages)
		n := len(asked)
		mu.Unlock()
		if len(q.Tools) != 2 || q.Tools[1].Function.Name != "web_search" {
			http.Error(w, `{"error":{"message":"no search tool"}}`, 400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if n%2 == 1 {
			io.WriteString(w, sse(`data: {"id":"c","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"latest go\"}"}}]}}]}`,
				`data: {"id":"c","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":30,"completion_tokens":7}}`,
				`data: [DONE]`))
			return
		}
		io.WriteString(w, sse(`data: {"id":"d","choices":[{"index":0,"delta":{"role":"assistant","content":"Go 1.27.1 is out."}}]}`,
			`data: {"id":"d","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
			`data: [DONE]`))
	}))
	defer model.Close()
	apis := newSearchAPIs(t)

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir()) // no signed-in agent searches
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	if err := provider.Save(provider.Provider{ID: "deep", Name: "Deep", Key: "k", Chat: model.URL, Models: []string{"deep-chat"}}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := searcher(); ok {
		t.Fatal("a provider searches")
	}
	for _, a := range []provider.SearchAPI{apis.api("tavily", "tvly-k"), apis.api("searxng", "")} {
		if err := provider.SetSearchAPI(a); err != nil {
			t.Fatal(err)
		}
	}
	ask := func() (string, []Hit, string) {
		t.Helper()
		body := `{"model":"deep/deep-chat","max_tokens":1000,"stream":true,"messages":[{"role":"user","content":"What is the latest Go?"}],
			"tools":[{"name":"Read","description":"read","input_schema":{"type":"object"}},{"type":"web_search_20250305","name":"web_search","max_uses":8}]}`
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var text strings.Builder
		var found []Hit
		for _, line := range strings.Split(rec.Body.String(), "\n") {
			data, ok := strings.CutPrefix(line, "data: ")
			if !ok {
				continue
			}
			var ev struct {
				Delta struct {
					Text string `json:"text"`
				} `json:"delta"`
				ContentBlock struct {
					Type    string `json:"type"`
					Content []Hit  `json:"content"`
				} `json:"content_block"`
			}
			json.Unmarshal([]byte(data), &ev)
			if ev.ContentBlock.Type == "web_search_tool_result" {
				found = ev.ContentBlock.Content
			}
			text.WriteString(ev.Delta.Text)
		}
		mu.Lock()
		defer mu.Unlock()
		last := asked[len(asked)-1]
		if len(asked)%2 == 1 || len(last) < 2 {
			t.Fatalf("model asked %d times: %v", len(asked), asked)
		}
		result, _ := asked[len(asked)-1][len(last)-1]["content"].(string)
		return text.String(), found, result
	}

	text, found, result := ask()
	if text != "Go 1.27.1 is out." || len(found) != 1 || found[0].URL != "https://go.dev/dl/" {
		t.Fatalf("text %q found %+v", text, found)
	}
	if !strings.Contains(result, "Go downloads — https://go.dev/dl/") || !strings.Contains(result, "go1.27.1 is the latest release.") {
		t.Fatalf("tool result %q", result)
	}
	if strings.Join(apis.asked, "|") != "tavily: latest go" {
		t.Fatalf("asked %q", apis.asked)
	}

	// Tavily down: SearXNG, next in line, answers
	apis.asked, apis.broken = nil, true
	if _, found, result = ask(); len(found) != 1 || !strings.Contains(result, "Go 1.27.1 is out") {
		t.Fatalf("found %+v result %q", found, result)
	}
	if strings.Join(apis.asked, "|") != "tavily: latest go|searxng: latest go" {
		t.Fatalf("asked %q", apis.asked)
	}
}

// Without a provider that searches or a search API, the client's search
// tool is left out, as before.
func TestNoSearchAPINoTool(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	if canSearch() {
		t.Fatal("can search with nothing set up")
	}
	if err := provider.SetSearchAPI(provider.SearchAPI{Vendor: "brave", Key: "k"}); err != nil {
		t.Fatal(err)
	}
	if !canSearch() {
		t.Fatal("a search API set up, and uni-switch can't search")
	}
}

// DeepSeek's Search API (TJHHHH on Discord) is asked as its docs say
// (api-docs.deepseek.com/guides/search): POST /v1/tools/search with the
// key as a Bearer and {query, count}, a query cut to its 128 characters.
// Its reply's summary is the page's text, since content is empty without
// need_content; snippet stands in when there is no summary, and its date
// is the page's age. 402 (no balance) and 429 (5 QPS) are refusals that
// rest the key as out of credit and rate limited.
func TestDeepSeekSearchAPI(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var mu sync.Mutex
	var got []map[string]any
	var paths, auths []string
	status := 200
	f := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		json.Unmarshal(b, &body)
		mu.Lock()
		got = append(got, body)
		paths = append(paths, r.Method+" "+r.URL.Path+" "+r.Header.Get("Content-Type"))
		auths = append(auths, r.Header.Get("Authorization"))
		st := status
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch st {
		case 402:
			w.WriteHeader(402)
			io.WriteString(w, `{"error":{"message":"Insufficient Balance","type":"unknown_error","param":null,"code":"invalid_request_error"}}`)
			return
		case 429:
			w.WriteHeader(429)
			io.WriteString(w, `{"error":{"message":"Rate limit reached","type":"rate_limit_error","param":null,"code":"rate_limit_exceeded"}}`)
			return
		}
		io.WriteString(w, `{"results":[`+
			`{"rerank_score":2.1,"title":"Go downloads","url":"https://go.dev/dl/","snippet":"Download Go.","summary":"Go 1.27.1 is the latest stable release, with binaries for every platform.","content":"","site_name":"Go","site_icon":"https://go.dev/favicon.ico","date":"2026-10-01T00:00:00+00:00","authority":3},`+
			`{"rerank_score":1.2,"title":"Go release history","url":"https://go.dev/doc/devel/release","snippet":"Release history of Go.","summary":"","content":"","site_name":null,"site_icon":null,"date":null,"authority":-1}`+
			`]}`)
	}))
	t.Cleanup(f.Close)
	s := New()
	a := provider.SearchAPI{Vendor: "deepseek", Key: "sk-ds", URL: f.URL}
	long := strings.Repeat("搜", 200)
	for _, q := range []string{"latest go", long} {
		pages, err := s.askSearchAPI(context.Background(), a, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(pages) != 2 || pages[0].URL != "https://go.dev/dl/" || pages[0].Title != "Go downloads" ||
			pages[0].Text != "Go 1.27.1 is the latest stable release, with binaries for every platform." ||
			pages[0].Age != "2026-10-01T00:00:00+00:00" ||
			pages[1].Text != "Release history of Go." || pages[1].Age != "" {
			t.Fatalf("pages %+v", pages)
		}
	}
	if paths[0] != "POST /v1/tools/search application/json" || auths[0] != "Bearer sk-ds" {
		t.Fatalf("asked %q with %q", paths[0], auths[0])
	}
	if got[0]["query"] != "latest go" || got[0]["count"] != float64(searchHits) || len(got[0]) != 2 {
		t.Fatalf("body %v", got[0])
	}
	if q, _ := got[1]["query"].(string); q != strings.Repeat("搜", deepseekQueryMax) {
		t.Fatalf("a long query went as %d characters", len([]rune(q)))
	}
	for _, c := range []struct {
		status int
		why    string
	}{{402, "out of credit"}, {429, "rate limited"}} {
		mu.Lock()
		status = c.status
		mu.Unlock()
		_, err := s.askSearchAPI(context.Background(), a, "q")
		var r searchRefused
		if !errors.As(err, &r) || r.status != c.status {
			t.Fatalf("%d: %v", c.status, err)
		}
		if _, why := searchRestFor("deepseek", c.status, "", time.Now()); why != c.why {
			t.Errorf("%d rests as %q", c.status, why)
		}
	}
	if v, ok := provider.SearchVendorOf("deepseek"); !ok || v.Base != "https://api.deepseek.com" || v.KeyOptional {
		t.Errorf("vendor %+v", v)
	}
}

// TestVolcengineSearchAPI: Doubao Search Custom (Volcengine) answers
// from its docs' own example reply (volcengine.com/docs/87772/2272953),
// asked as its docs say, its errors read by their code (CyberCaius on
// Discord).
func TestVolcengineSearchAPI(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	reply, err := os.ReadFile("testdata/volcengine_web_search.json")
	if err != nil {
		t.Fatal(err)
	}
	// its docs' error example, its "// 该字段为错误码" note taken out
	errReply := func(code int, msg string) string {
		return `{
    "ResponseMetadata": {
        "RequestId": "202505231206578F7BD0BCD73AF8703C1E",
        "Action": "WebSearch",
        "Version": "2025-01-01",
        "Service": "volc_torchlight_api",
        "Region": "cn-beijing",
        "Error": {
            "CodeN": ` + strconv.Itoa(code) + `,
            "Code": "` + strconv.Itoa(code) + `",
            "Message": "` + msg + `"
        }
    },
    "Result": null
}`
	}
	var mu sync.Mutex
	var got []map[string]any
	var paths, auths []string
	answer := func(w http.ResponseWriter) { w.Write(reply) }
	f := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		json.Unmarshal(b, &body)
		mu.Lock()
		got = append(got, body)
		paths = append(paths, r.Method+" "+r.URL.Path+" "+r.Header.Get("Content-Type"))
		auths = append(auths, r.Header.Get("Authorization"))
		a := answer
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		a(w)
	}))
	t.Cleanup(f.Close)
	s := New()
	a := provider.SearchAPI{Vendor: "volcengine", Key: "volc-key-123456", URL: f.URL}
	long := strings.Repeat("搜", 150)
	for _, q := range []string{"北京最新游玩攻略", long} {
		pages, err := s.askSearchAPI(context.Background(), a, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(pages) != 2 || pages[0].URL != "https://c.m.163.com/news/a/L4U501RM0556LJ9Q.html" ||
			pages[0].Title != "2026北京5日游超全攻略｜行前预约、交通住宿、经典路线、避坑指南一站式拆解" ||
			!strings.HasPrefix(pages[0].Text, "2026北京5日游超全攻略｜行前预约、交通住宿、经典路线、避坑指南一站式拆解 北京旅游——行前准备") ||
			!strings.Contains(pages[0].Text, "高铁（首选）") || // its Summary, not its Snippet
			strings.Contains(pages[0].Text, "旅游定制小团 2026-08-22") || // nor its Content
			pages[0].Age != "2026-08-22T09:16:00+08:00" ||
			pages[1].URL != "https://www.cnblogs.com/chun-wan-lv/p/22651954" || pages[1].Age != "2026-08-24T11:37:00+08:00" {
			t.Fatalf("pages %+v", pages)
		}
		if len(pages[0].Text) > pageText+len("…") {
			t.Fatalf("text of %d bytes", len(pages[0].Text))
		}
	}
	if paths[0] != "POST /search_api/web_search application/json" || auths[0] != "Bearer volc-key-123456" {
		t.Fatalf("asked %q with %q", paths[0], auths[0])
	}
	b, _ := json.Marshal(got[0])
	if string(b) != `{"Count":6,"Filter":{"NeedUrl":true},"Query":"北京最新游玩攻略","SearchType":"web"}` {
		t.Fatalf("body %s", b)
	}
	if q, _ := got[1]["Query"].(string); q != strings.Repeat("搜", volcQueryMax) {
		t.Fatalf("a long query went as %d characters", len([]rune(q)))
	}

	// a page with no URL is left out, and a 1970 time is none
	mu.Lock()
	answer = func(w http.ResponseWriter) {
		io.WriteString(w, `{"ResponseMetadata":{"RequestId":"r"},"Result":{"ResultCount":2,"WebResults":[`+
			`{"Id":"a","SortId":1,"Title":"天气","SiteName":"火山如意","Url":"","Snippet":"晴"},`+
			`{"Id":"b","SortId":2,"Title":"<em>Go</em> 下载","Url":"https://go.dev/dl/","Snippet":"Download Go.","Summary":"","PublishTime":"1970-01-01T08:00:00+08:00"}]}}`)
	}
	mu.Unlock()
	pages, err := s.askSearchAPI(context.Background(), a, "go")
	if err != nil || len(pages) != 1 || pages[0].Title != "Go 下载" || pages[0].Text != "Download Go." || pages[0].Age != "" {
		t.Fatalf("pages %+v, %v", pages, err)
	}

	// an error comes in ResponseMetadata.Error, with a 200 or a status
	for _, c := range []struct {
		status, code, want int
		why                string
	}{
		{200, 10403, 403, "turned away"},
		{401, 10401, 403, "turned away"},
		{200, 10406, 402, "out of credit"},
		{200, 10412, 402, "out of credit"},
		{429, 700429, 429, "rate limited"},
		{200, 700429, 429, "rate limited"},
		{200, 10400, 0, ""},
		{500, 10500, 0, ""},
	} {
		mu.Lock()
		answer = func(w http.ResponseWriter) {
			w.WriteHeader(c.status)
			io.WriteString(w, errReply(c.code, "volc-key-123456 query or search type is empty"))
		}
		mu.Unlock()
		_, err := s.askSearchAPI(context.Background(), a, "q")
		if err == nil || !strings.Contains(err.Error(), strconv.Itoa(c.code)+" ") || strings.Contains(err.Error(), "volc-key-123456") {
			t.Fatalf("%d %d: %v", c.status, c.code, err)
		}
		var r searchRefused
		if refused := errors.As(err, &r); refused != (c.want != 0) || refused && r.status != c.want {
			t.Fatalf("%d %d: %#v", c.status, c.code, err)
		}
		if c.want != 0 {
			if _, why := searchRestFor("volcengine", r.status, "", time.Now()); why != c.why {
				t.Errorf("%d rests as %q", c.code, why)
			}
		}
	}
	if v, ok := provider.SearchVendorOf("volcengine"); !ok || v.Base != "https://open.feedcoopapi.com" || v.KeyOptional {
		t.Errorf("vendor %+v", v)
	}
}

// TestWSASearchAPI: Tencent Cloud's Yuanbao Search (元宝搜索, wsa) by its
// service API key answers from its docs' own example replies, asked as
// its docs say, its errors read by their code (plugins#509, chenjiangzi).
// wsa_searchpro.json is SearchPro's API page's example
// (cloud.tencent.com/document/api/1806/121811) byte for byte;
// wsa_searchpro_apikey.json the API-key page's
// (cloud.tencent.com/document/product/1806/130615), whose page shows each
// quote in Pages escaped twice (\\", not JSON), once here.
func TestWSASearchAPI(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	api, err := os.ReadFile("testdata/wsa_searchpro.json")
	if err != nil {
		t.Fatal(err)
	}
	keyed, err := os.ReadFile("testdata/wsa_searchpro_apikey.json")
	if err != nil {
		t.Fatal(err)
	}
	// its docs' error reply (cloud.tencent.com/document/api/1806/121818)
	errReply := func(code, msg string) string {
		return `{
        "Response": {
            "Error": {
                "Code": "` + code + `",
                "Message": "` + msg + `"
            },
            "RequestId": "ed93f3cb-f35e-473f-b9f3-0d451b8b79c6"
        }
    }`
	}
	var mu sync.Mutex
	var got []map[string]any
	var paths, auths []string
	reply := keyed
	answer := func(w http.ResponseWriter) { w.Write(reply) }
	f := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		json.Unmarshal(b, &body)
		mu.Lock()
		got = append(got, body)
		paths = append(paths, r.Method+" "+r.URL.Path+" "+r.Header.Get("Content-Type"))
		auths = append(auths, r.Header.Get("Authorization"))
		a := answer
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		a(w)
	}))
	t.Cleanup(f.Close)
	s := New()
	a := provider.SearchAPI{Vendor: "wsa", Key: "sk-209b0123456789e23d", URL: f.URL}

	pages, err := s.askSearchAPI(context.Background(), a, "三星堆的由来")
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 3 || pages[0].URL != "https://www.163.com/dy/article/J43Q2STU0553FV3Z.html" ||
		pages[0].Title != "三星堆名字的由来,你真的了解吗?三星堆的参观攻略,建议收藏青铜文物玉器古蜀国三星堆遗址_网易订阅" ||
		!strings.HasPrefix(pages[0].Text, "三星堆名字的由来 三星堆遗址位于四川省广汉市西北部") ||
		pages[0].Age != "2024-06-07 19:00:51" ||
		pages[1].URL != "https://news.qq.com/rain/a/20240903A048N700" ||
		// its <strong> marks taken out
		pages[2].Text != "最早在1929年,一位当地农民在遗址区域内偶然发现了一个玉器坑,这一发现引起了考古界的关注｡在四川省广汉市西北部,有三座稍稍隆起的土堆呈直线排列,这种地形布局与夜空中闪烁的三颗星辰相呼应,因此得名“三星堆”｡" {
		t.Fatalf("pages %+v", pages)
	}
	if paths[0] != "POST /SearchPro application/json" || auths[0] != "Bearer sk-209b0123456789e23d" {
		t.Fatalf("asked %q with %q", paths[0], auths[0])
	}
	// Query as given, and Cnt 10: fewer is turned away ("illegal Cnt")
	if len(got[0]) != 2 || got[0]["Query"] != "三星堆的由来" || got[0]["Cnt"] != float64(10) {
		t.Fatalf("body %v", got[0])
	}

	// the API page's example: a passage that is only its title gives
	// way to its content; a date as it gives it
	mu.Lock()
	reply = api
	mu.Unlock()
	pages, err = s.askSearchAPI(context.Background(), a, "人工智能发展现状")
	if err != nil || len(pages) != 1 || pages[0].URL != "https://www.cac.gov.cn/2025-02/26/c_1742272852299173.htm" ||
		pages[0].Title != "【新思想引领新征程】人工智能创新加速我国产业转型升级_中央网络安全和信息化委员会办公室" ||
		pages[0].Text != "【新思想引领新征程】人工智能创新加速我国产业转型升级_中央网络安全和信息化委员会办公室..." ||
		pages[0].Age != "2025/02/26 09:37:00" {
		t.Fatalf("pages %+v, %v", pages, err)
	}

	// its errors come in Response.Error with a 200, read by their code
	for _, c := range []struct {
		code, msg string
		want      int
		why       string
	}{
		{"UnauthorizedOperation", "未授权操作。", 403, "turned away"},
		{"AuthFailure.SignatureFailure", "The provided credentials could not be validated. Please check your signature is correct.", 403, "turned away"},
		{"ResourceNotFound", "用户资源未开通。", 403, "turned away"},
		{"ResourceUnavailable", "用户资源不可用。", 402, "out of credit"},
		{"LimitExceeded", "超过配额限制。", 402, "out of credit"},
		{"RequestLimitExceeded", "请求的次数超过了频率限制。", 429, "rate limited"},
		{"RequestLimitExceeded.UinLimitExceeded", "主账号限频。", 429, "rate limited"},
		{"InvalidParameter", "illegal Cnt sk-209b0123456789e23d", 0, ""},
		{"InternalError", "内部错误。", 0, ""},
	} {
		mu.Lock()
		answer = func(w http.ResponseWriter) { io.WriteString(w, errReply(c.code, c.msg)) }
		mu.Unlock()
		_, err := s.askSearchAPI(context.Background(), a, "q")
		if err == nil || !strings.Contains(err.Error(), c.code+" ") || strings.Contains(err.Error(), "sk-209b0123456789e23d") {
			t.Fatalf("%s: %v", c.code, err)
		}
		var r searchRefused
		if refused := errors.As(err, &r); refused != (c.want != 0) || refused && r.status != c.want {
			t.Fatalf("%s: %#v", c.code, err)
		}
		if c.want != 0 {
			if _, why := searchRestFor("wsa", r.status, "", time.Now()); why != c.why {
				t.Errorf("%s rests as %q", c.code, why)
			}
		}
	}
	if v, ok := provider.SearchVendorOf("wsa"); !ok || v.Base != "https://api.wsa.cloud.tencent.com" || v.KeyOptional ||
		v.KeysURL != "https://console.cloud.tencent.com/wsapi/index" {
		t.Errorf("vendor %+v", v)
	}
}
