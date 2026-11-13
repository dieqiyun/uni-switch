package gateway

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// asSentRequest is a Responses Lite request with what a relay checking
// Codex's own shape looks at (#1758): the additional_tools item, the
// functions namespace, Codex's stream_options and its turn metadata.
var asSentRequest = strings.Replace(liteRequest, `"stream":true,`,
	`"stream":true,"stream_options":{"reasoning_summary_delivery":"inline"},"client_metadata":{"x-codex-turn-metadata":"{\"turn_id\":\"t1\"}"},`, 1)

// A relay set to take Codex's requests as sent (codex=as-sent) gets the
// request as Codex sent it, its model name alone swapped, and Codex's turn
// metadata header (#1758: lifting additional_tools into tools was answered
// 400 "Codex 客户端请求特征无效", 10014). Off, the default, the request is
// still fitted as before (#350).
func TestCodexAsSent(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"type":"response.created","response":{"id":"r1","model":"m1"}}`,
		`data: {"type":"response.completed","response":{"id":"r1","output":[],"usage":{"input_tokens":5,"output_tokens":2}}}`)}
	setup(t, provider.Responses, f)
	ask := func() {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(asSentRequest))
		req.Header.Set("User-Agent", "codex_cli_rs/0.162.0-alpha.17.2 (Mac OS 26.2.0; arm64)")
		req.Header.Set("originator", "codex_cli_rs")
		req.Header.Set("X-Codex-Turn-Metadata", `{"turn_id":"t1"}`)
		req.Header.Set("X-OpenAI-Internal-Codex-Responses-Lite", "true")
		New().Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(asSentRequest), &sent); err != nil {
		t.Fatal(err)
	}
	sent["model"] = "m1"

	ask()
	if strings.Contains(string(f.got), `"additional_tools"`) || strings.Contains(string(f.got), "reasoning_summary_delivery") {
		t.Fatalf("by default the request is fitted as before: %s", f.got)
	}

	p, err := provider.Find("fake")
	if err != nil {
		t.Fatal(err)
	}
	p.CodexAsSent = true
	if err := provider.Save(*p); err != nil {
		t.Fatal(err)
	}
	ask()
	var got map[string]any
	if err := json.Unmarshal(f.got, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, sent) {
		t.Fatalf("as sent, the relay got\n%s\nwant Codex's request with model m1", f.got)
	}
	for _, h := range []string{"X-Codex-Turn-Metadata", "X-Openai-Internal-Codex-Responses-Lite"} {
		if f.head.Get(h) == "" {
			t.Errorf("%s not sent on: %v", h, f.head)
		}
	}
}

// Codex offers web_search on every turn, and a request offering it to a
// model that doesn't search by itself was translated for uni-switch's search
// before anything else: the developer messages merged into instructions,
// web_search left out, max_output_tokens added. A relay set codex=as-sent
// got that shape all the same (maralica on Discord). It now gets Codex's
// turn byte for byte, web_search in it, its model name alone swapped; a
// relay left at the default is still translated and searched for.
func TestCodexAsSentWebSearch(t *testing.T) {
	codex, err := os.ReadFile("testdata/workdir/codex-0.162.0.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(codex, []byte(`{"type":"web_search"`)) || !bytes.Contains(codex, []byte(`"role":"developer"`)) {
		t.Fatal("the fixture is no longer a Codex turn offering web_search")
	}
	// spawn_agent's description is widened with the catalog's models on
	// every Codex turn, as sent or not (spawnAgentModels), so the turn
	// goes without the multi-agent tools: each other tool is Codex's bytes
	tools, _, _ := topMember(codex, "tools")
	var each []json.RawMessage
	if err := json.Unmarshal(tools, &each); err != nil {
		t.Fatal(err)
	}
	each = slices.DeleteFunc(each, func(m json.RawMessage) bool { return bytes.Contains(m, []byte(`"spawn_agent"`)) })
	kept, err := json.Marshal(each)
	if err != nil {
		t.Fatal(err)
	}
	codex = withMembers(codex, map[string][]byte{"tools": kept})
	asked := bytes.Replace(codex, []byte(`"model":"gpt-5"`), []byte(`"model":"fake/m1"`), 1)
	want := bytes.Replace(codex, []byte(`"model":"gpt-5"`), []byte(`"model":"m1"`), 1)
	f := &fake{t: t, reply: sse(
		`data: {"type":"response.created","response":{"id":"r1","model":"m1"}}`,
		`data: {"type":"response.completed","response":{"id":"r1","output":[],"usage":{"input_tokens":5,"output_tokens":2}}}`)}
	setup(t, provider.Responses, f)
	ask := func() {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(asked))
		req.Header.Set("User-Agent", "codex_cli_rs/0.162.0 (Mac OS 26.2.0; arm64)")
		req.Header.Set("originator", "codex_cli_rs")
		New().Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
	}

	ask()
	if bytes.Contains(f.got, []byte(`{"type":"web_search"`)) || !bytes.Contains(f.got, []byte(`"max_output_tokens"`)) {
		t.Fatalf("by default the turn is translated for uni-switch's search: %.400s", f.got)
	}

	p, err := provider.Find("fake")
	if err != nil {
		t.Fatal(err)
	}
	p.CodexAsSent = true
	if err := provider.Save(*p); err != nil {
		t.Fatal(err)
	}
	ask()
	if !bytes.Equal(f.got, want) {
		i := 0
		for i < len(f.got) && i < len(want) && f.got[i] == want[i] {
			i++
		}
		t.Fatalf("as sent, the relay's body differs from Codex's at byte %d of %d (want %d):\ngot  …%.200s\nwant …%.200s", i, len(f.got), len(want), f.got[max(0, i-40):], want[max(0, i-40):])
	}
}
