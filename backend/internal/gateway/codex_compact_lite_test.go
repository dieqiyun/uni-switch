package gateway

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// WuSix on Discord, codex/gpt-6-astra: "compaction: codex/gpt-6-astra
// failed to summarise this conversation 3 times in a row (last: the model
// wrote no summary)". codex/gpt-6-astra's catalog entry is Codex's own
// (codexcat.ownEntry), which puts gpt-6 on Responses Lite, so Codex sends
// its tools as an additional_tools input item and none in tools. uni-switch's
// summary request emptied tools and left that item in, so the model was
// still offered every tool Codex has, and an agentic model in the middle
// of its work called one rather than writing the summary: a function_call
// and no message, which is no summary.
//
// The backend here answers as such a model would: a call to the first tool
// it is offered, wherever it is offered, and the summary only when it is
// offered none.
func TestCodexLiteCompactionOffersNoTools(t *testing.T) {
	codexSignedIn(t)
	var mu sync.Mutex
	var got [][]byte
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, b)
		mu.Unlock()
		var q struct {
			Input []struct {
				Type  string           `json:"type"`
				Tools []map[string]any `json:"tools"`
			} `json:"input"`
			Tools []map[string]any `json:"tools"`
		}
		json.Unmarshal(b, &q)
		offered := len(q.Tools) > 0
		for _, it := range q.Input {
			offered = offered || it.Type == "additional_tools" && len(it.Tools) > 0
		}
		item := `{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"SUMMARY of the work"}]}`
		if offered {
			item = `{"type":"function_call","call_id":"c2","name":"shell","namespace":"functions","arguments":"{\"cmd\":\"go test ./...\"}"}`
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-6-astra"}}`,
			`data: {"type":"response.output_item.done","output_index":0,"item":`+item+`}`,
			`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":9,"output_tokens":2}}}`))
	}))
	t.Cleanup(up.Close)
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })

	// liteRequest's conversation, on codex/gpt-6-astra, with Codex's
	// compaction_trigger after it
	body := strings.Replace(liteRequest, `"model":"fake/m1"`, `"model":"codex/gpt-6-astra","reasoning":{"effort":"xhigh","context":"all_turns"}`, 1)
	body = strings.Replace(body, "\n]}", `,{"type":"compaction_trigger"}]}`, 1)
	if !strings.Contains(body, "compaction_trigger") || !strings.Contains(body, "codex/gpt-6-astra") {
		t.Fatalf("fixture: %s", body)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer chatgpt-token")
	req.Header.Set("chatgpt-account-id", "acct-1")
	req.Header.Set("X-OpenAI-Internal-Codex-Responses-Lite", "true")
	New().Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("the backend was asked %d times, not once", len(got))
	}
	sent := string(got[0])
	if strings.Contains(sent, `"additional_tools"`) || strings.Contains(sent, `"apply_patch"`) || strings.Contains(sent, `"mcp__probe"`) {
		t.Errorf("the summary request offers the conversation's tools: %s", sent)
	}
	if !strings.Contains(sent, "CONTEXT CHECKPOINT COMPACTION") || !strings.Contains(sent, `"call_id":"c1"`) || !strings.Contains(sent, "list the files") {
		t.Errorf("the summary request lost the conversation or the prompt: %s", sent)
	}
	var item map[string]any
	for _, e := range events(rec.Body.String()) {
		if e["type"] == "response.output_item.done" {
			item, _ = e["item"].(map[string]any)
		}
	}
	enc, _ := item["encrypted_content"].(string)
	sum, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(enc, uniSwitchCompaction))
	if item["type"] != "compaction" || string(sum) != "SUMMARY of the work" {
		t.Errorf("compaction: %s", rec.Body.String())
	}
}

// A summary reply without a summary says what it held instead, so the
// next report says whether the model called a tool, only reasoned, or
// answered nothing.
func TestCompactNoSummarySaysWhatCame(t *testing.T) {
	for _, c := range []struct {
		reply, want string
	}{
		{`{"id":"r","status":"completed","output":[{"type":"reasoning"},{"type":"function_call","name":"shell"},{"type":"function_call","name":"shell"}]}`, " (it answered with reasoning, function_call shell)"},
		{`{"id":"r","status":"completed","output":[]}`, " (it answered with nothing)"},
	} {
		res, err := compactReply([]byte(c.reply))
		if err != nil {
			t.Fatal(err)
		}
		if got := answeredWith(res.Output); got != c.want {
			t.Errorf("%s: %q, want %q", c.reply, got, c.want)
		}
	}
}
