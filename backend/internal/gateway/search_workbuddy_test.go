package gateway

import (
	"encoding/json"
	"io"
	"net/http"

	"sync"
)

// wbSearchReply is what WorkBuddy AI's /agenttool/v1/search answered a
// real account on 2026-10-12, as it came (snippets cut short).
const wbSearchReply = `{"query":"Go 1.26 release notes","type":"text2text","provider":"0","results":[` +
	`{"title":"Go 1.26 Release Notes - The Go Programming Language","url":"https://go.dev/doc/go1.26","snippet":"The latest Go release, version 1.26, arrives in February 2026, six months after Go 1.25. Most of its changes are in the implementation …"},` +
	`{"title":"Go 1.26 来了！new语法爽翻天，绿茶GC默认开启 ...","url":"https://www.jdon.com/90433-go-1-26-release-notes-full-breakdown.html","snippet":"2026年2月12日 · Go 1.26 于2026年2月发布，带来语言级新特性、全新“绿茶”垃圾回收器、go fix 现代化重构、goroutine 泄漏检测实验 …"}` +
	`],"total_results":2,"response_time_ms":2428}`

// wbSearchSite is a WorkBuddy site as a Claude Code WebSearch turn on it
// goes: its config lists hy3; hy3 calls uni-switch's web_search once, then
// answers from what it was given; its search service answers as the real
// one did, or with refusal while that is set.
type wbSearchSite struct {
	mu       sync.Mutex
	searched []map[string]any
	heads    []http.Header
	results  []string // the tool results hy3 was given
	refusal  string
}

func (s *wbSearchSite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.URL.Path {
	case "/v3/config":
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"code":0,"msg":"OK","data":{"agents":[{"name":"cli","models":["hy3"]}],"models":[{"id":"hy3","name":"Hy3","tags":[]}]}}`)
	case wbSearchPath:
		var q map[string]any
		json.Unmarshal(b, &q)
		s.searched, s.heads = append(s.searched, q), append(s.heads, r.Header.Clone())
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if s.refusal != "" {
			io.WriteString(w, s.refusal)
			return
		}
		io.WriteString(w, wbSearchReply)
	case "/v2/chat/completions", "/v1/chat/completions":
		var q struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
			Messages []map[string]any `json:"messages"`
		}
		json.Unmarshal(b, &q)
		w.Header().Set("Content-Type", "text/event-stream")
		if last := q.Messages[len(q.Messages)-1]; last["role"] == "tool" {
			c, _ := last["content"].(string)
			s.results = append(s.results, c)
			io.WriteString(w, sse(`data: {"id":"d","choices":[{"index":0,"delta":{"role":"assistant","content":"Go 1.26 is out."}}]}`,
				`data: {"id":"d","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
				`data: [DONE]`))
			return
		}
		for _, t := range q.Tools {
			if t.Function.Name == "web_search" {
				io.WriteString(w, sse(`data: {"id":"c","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"Go 1.26 release notes\"}"}}]}}]}`,
					`data: {"id":"c","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":30,"completion_tokens":7}}`,
					`data: [DONE]`))
				return
			}
		}
		// no search tool: hy3 answers from nothing, as Claude Code's
		// WebSearch then shows no results
		io.WriteString(w, sse(`data: {"id":"e","choices":[{"index":0,"delta":{"role":"assistant","content":"I can't search."}}]}`,
			`data: {"id":"e","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
			`data: [DONE]`))
	default:
		http.NotFound(w, r)
	}
}
