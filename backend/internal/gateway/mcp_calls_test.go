package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/budget"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// loggedRows is every row of usage.jsonl, the MCP ones included, which
// usage.Load leaves out (they ride the probe lane), and the file's text.
func loggedRows(t *testing.T) ([]usage.Record, string) {
	t.Helper()
	b, err := os.ReadFile(usage.Path())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var out []usage.Record
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var r usage.Record
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			out = append(out, r)
		}
	}
	return out, string(b)
}

func mcpRows(t *testing.T) []usage.Record {
	t.Helper()
	all, _ := loggedRows(t)
	var out []usage.Record
	for _, r := range all {
		if r.Source == usage.SourceMCP {
			out = append(out, r)
		}
	}
	return out
}

// A tool call to the gateway's image MCP server is a row of the request
// log (ferock on Discord): the tool, its server, the provider and model
// the drawing went to, how long it took, and nothing of its arguments.
// The drawing's own request is a model row beside it, and only that one
// counts.
func TestMCPToolCallIsARequestRow(t *testing.T) {
	s, _ := easeled(t)
	t.Setenv("UNI_SWITCH_PUBLIC_URL", "")
	t.Cleanup(s.imageStore.running.Wait)
	h := localGuard(s.Handler())
	const prompt = "a uni-switch on a secret-plum-branch"
	m := readImageReply(t, imagePost(h, "127.0.0.1:5000", imageToolCall(GenerateImageTool, map[string]any{"prompt": prompt}), "User-Agent", "claude-cli/2.1.0 (external, cli)"))
	if m.Result.IsError {
		t.Fatalf("drawn: %+v", m)
	}
	rows := mcpRows(t)
	if len(rows) != 1 {
		t.Fatalf("MCP rows %+v", rows)
	}
	r := rows[0]
	if r.Requested != GenerateImageTool || r.Host != imageServerName || r.Kind != usage.KindMCP || r.Endpoint != ImageMCPPath ||
		r.Provider != "art" || r.Model == "" || r.Error != "" || r.Operation != "execute_tool" || r.Time.IsZero() {
		t.Fatalf("row %+v", r)
	}
	if !r.IsProbe() {
		t.Fatal("an MCP row is counted")
	}
	all, text := loggedRows(t)
	if strings.Contains(text, "secret-plum-branch") {
		t.Fatalf("the prompt was kept: %s", text)
	}
	// the drawing is a model row of its own, the one Load lists
	if len(all) < 2 || len(usage.Load(r.Time.Add(-1))) == 0 {
		t.Fatalf("no model row beside it: %+v", all)
	}
	for _, x := range usage.Load(r.Time.Add(-1)) {
		if x.Source == usage.SourceMCP {
			t.Fatalf("Load lists the MCP row %+v", x)
		}
	}
}

// A call that fails says so, and a call to a tool the server has not is
// a row too, failed.
func TestMCPToolCallFailureIsKept(t *testing.T) {
	s, _ := easeled(t)
	t.Setenv("UNI_SWITCH_PUBLIC_URL", "")
	t.Cleanup(s.imageStore.running.Wait)
	h := localGuard(s.Handler())
	readImageReply(t, imagePost(h, "127.0.0.1:5000", imageToolCall(GenerationResultTool, map[string]any{"id": "img_nothing"})))
	w := imagePost(h, "127.0.0.1:5000", imageToolCall("draw_me", map[string]any{"prompt": "x"}))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	rows := mcpRows(t)
	if len(rows) != 2 || rows[0].Requested != GenerationResultTool || !strings.Contains(rows[0].Error, "no image is being made") ||
		rows[1].Requested != "draw_me" || rows[1].Error == "" {
		t.Fatalf("rows %+v", rows)
	}
}

// generation_result is of the model that started the job it waits on.
func TestMCPGenerationResultNamesTheJobsModel(t *testing.T) {
	fresh(t)
	t.Setenv("UNI_SWITCH_PUBLIC_URL", "")
	gate := make(chan struct{})
	e := &easel{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-gate
		e.ServeHTTP(w, r)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "art", Name: "Art", Chat: up.URL + "/v1", Key: "key", Models: []string{"gpt-image-1"}}); err != nil {
		t.Fatal(err)
	}
	s := New()
	opened := false
	open := func() {
		if !opened {
			opened = true
			close(gate)
		}
	}
	t.Cleanup(s.imageStore.running.Wait)
	t.Cleanup(open)
	h := localGuard(s.Handler())
	was := imageMCPWithin
	t.Cleanup(func() { imageMCPWithin = was })
	imageMCPWithin = 10 * time.Millisecond
	m := readImageReply(t, imagePost(h, "127.0.0.1:5000", imageToolCall(GenerateImageTool, map[string]any{"prompt": "a uni-switch"})))
	text := m.Result.Content[0].Text
	i := strings.Index(text, `"img_`)
	if m.Result.IsError || i < 0 {
		t.Fatalf("still drawing: %+v", m)
	}
	id := strings.Trim(strings.Fields(text[i:])[0], `".,`)
	open()
	imageMCPWithin = time.Minute
	m = readImageReply(t, imagePost(h, "127.0.0.1:5000", imageToolCall(GenerationResultTool, map[string]any{"id": id})))
	if m.Result.IsError {
		t.Fatalf("waited on: %+v", m)
	}
	rows := mcpRows(t)
	if len(rows) != 2 || rows[1].Requested != GenerationResultTool || rows[1].Provider != "art" || rows[1].Model != "gpt-image-1" {
		t.Fatalf("rows %+v", rows)
	}
}

// A hub call's row names the Library server the tool is of and the agent
// the hub is mounted for; its arguments are not kept.
func TestMCPHubCallIsARequestRow(t *testing.T) {
	fresh(t)
	f := &fakeHub{on: true, tools: map[string][]HubTool{
		"claude": {{Server: "github", Tool: "create_issue", Spec: hubSpec("create_issue")}, {Server: "fs", Tool: "read", Spec: hubSpec("read")}},
	}}
	f.call = func(_ context.Context, server, tool string) (json.RawMessage, error) {
		if server == "fs" {
			return nil, errors.New("fs is down")
		}
		return json.RawMessage(`{"content":[{"type":"text","text":"done"}]}`), nil
	}
	withHub(t, f)
	h := localGuard(New().Handler())
	readMCP(t, hubPost(h, "claude", here, toolsCall("github__create_issue", `{"q":"secret-plum-title"}`)))
	readMCP(t, hubPost(h, "claude", here, toolsCall("fs__read", `{"q":"x"}`)))
	readMCP(t, hubPost(h, "claude", here, toolsList)) // not a call: no row
	rows := mcpRows(t)
	if len(rows) != 2 {
		t.Fatalf("rows %+v", rows)
	}
	if r := rows[0]; r.Requested != "github__create_issue" || r.Host != "github" || r.Agent != "claude" || r.Error != "" || r.Provider != "" {
		t.Fatalf("hub row %+v", r)
	}
	if r := rows[1]; r.Host != "fs" || !strings.Contains(r.Error, "fs is down") {
		t.Fatalf("failed hub row %+v", r)
	}
	if _, text := loggedRows(t); strings.Contains(text, "secret-plum-title") {
		t.Fatalf("arguments kept: %s", text)
	}
}

// The web search server's call is a row of uni-switch-web-search.
func TestMCPSearchCallIsARequestRow(t *testing.T) {
	fresh(t)
	apis := newSearchAPIs(t)
	if err := provider.SetSearchAPI(apis.api("tavily", "tvly-k")); err != nil {
		t.Fatal(err)
	}
	h := localGuard(New().Handler())
	readMCP(t, mcpPost(h, here, mcpSearchCall))
	rows := mcpRows(t)
	if len(rows) != 1 || rows[0].Requested != "web_search" || rows[0].Host != "uni-switch-web-search" || rows[0].Error != "" {
		t.Fatalf("rows %+v", rows)
	}
	if _, text := loggedRows(t); strings.Contains(text, "latest go") {
		t.Fatalf("the query was kept: %s", text)
	}
}

// The uni-switch Image server an agent runs reports each call it answered
// (MCPCalledMethod): a row of its tool and model, at "stdio"; a tool not
// uni-switch Image's is turned away.
func TestMCPCalledFromStdioIsARow(t *testing.T) {
	s, _ := easeled(t)
	h := localGuard(s.Handler())
	body := func(c MCPCalled) string {
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 9, "method": MCPCalledMethod, "params": c})
		return string(b)
	}
	m := readMCP(t, imagePost(h, "127.0.0.1:5000", body(MCPCalled{Tool: GenerateImageTool, Millis: 1500, Model: "art/gpt-image-1"})))
	if m.Error != nil {
		t.Fatalf("reported: %+v", m)
	}
	readMCP(t, imagePost(h, "127.0.0.1:5000", body(MCPCalled{Tool: GenerateVideoTool, Millis: 20, Error: "no model makes videos"})))
	if m := readMCP(t, imagePost(h, "127.0.0.1:5000", body(MCPCalled{Tool: "rm_rf"}))); m.Error == nil || m.Error.Code != -32602 {
		t.Fatalf("another tool: %+v", m)
	}
	rows := mcpRows(t)
	if len(rows) != 2 {
		t.Fatalf("rows %+v", rows)
	}
	if r := rows[0]; r.Requested != GenerateImageTool || r.Host != imageServerName || r.Endpoint != "stdio" || r.Provider != "art" || r.Model != "gpt-image-1" || r.Millis != 1500 {
		t.Fatalf("row %+v", r)
	}
	if r := rows[1]; r.Provider != "" || r.Error != "no model makes videos" {
		t.Fatalf("failed row %+v", r)
	}
}

// A key's MCP tool call adds nothing to what its limit counts, as read
// back from usage.jsonl: the model requests it made are its calls.
func TestMCPCallCountsInNoKeyLimit(t *testing.T) {
	fresh(t)
	f := &fakeHub{on: true, tools: map[string][]HubTool{"claude": {{Server: "fs", Tool: "read", Spec: hubSpec("read")}}}}
	withHub(t, f)
	keys, secrets := newCaller(t, "Capped")
	lim := &access.Limit{Period: "day", Tokens: 700}
	setLimit(t, keys[0].ID, lim)
	h := localGuard(New().Handler())
	st := budget.Of(access.Key{ID: keys[0].ID, Limit: lim}, time.Now())
	if st == nil || st.Calls != 0 {
		t.Fatalf("before: %+v", st)
	}
	m := readMCP(t, hubPost(h, "claude", here, toolsCall("fs__read", `{"q":"x"}`), "Authorization", "Bearer "+secrets[0]))
	if m.Result.IsError || len(f.ran()) != 1 {
		t.Fatalf("call: %+v %v", m, f.ran())
	}
	if rows := mcpRows(t); len(rows) != 1 || rows[0].CallerKeyID != keys[0].ID {
		t.Fatalf("rows %+v", rows)
	}
	if st := budget.Of(access.Key{ID: keys[0].ID, Limit: lim}, time.Now()); st == nil || st.Calls != 0 {
		t.Fatalf("an MCP call counted: %+v", st)
	}
}
