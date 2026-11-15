package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// listedIDs is /v1/models' ids, in order, as the request r is answered.
func listedIDs(t *testing.T, r *http.Request) []string {
	t.Helper()
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, r)
	var l struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &l); err != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var ids []string
	for _, m := range l.Data {
		ids = append(ids, m.ID)
	}
	return ids
}

// love1sbug on X, moving from CLIProxyAPI: the old clients ask for
// gpt-5.6-sol and pick from /v1/models, and uni-switch listed only
// codex/gpt-5.6-sol, whatever Names in agents' lists said. With Model ids
// without the provider on, the list has gpt-5.6-sol too, after the
// provider/model ids it had, and a request for it on each API reaches the
// Codex account as gpt-5.6-sol. Off, as by default, the list is as it was.
func TestBareModelIDsReporterCase(t *testing.T) {
	codexSignedIn(t)
	var mu sync.Mutex
	var asked []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		asked = append(asked, modelOf(b))
		mu.Unlock()
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"r1","model":"gpt-5.6-sol"}}`,
			`data: {"type":"response.output_text.delta","delta":"pong"}`,
			`data: {"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":7,"output_tokens":1}}}`))
	}))
	t.Cleanup(up.Close)
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })
	if err := provider.Save(provider.Provider{ID: "codex", Models: []string{"gpt-5.6-sol"}}); err != nil {
		t.Fatal(err)
	}

	list := func() []string { return listedIDs(t, httptest.NewRequest("GET", "/v1/models", nil)) }
	before := list()
	if !slices.Contains(before, "codex/gpt-5.6-sol") || slices.Contains(before, "gpt-5.6-sol") {
		t.Fatalf("off, as by default: %v", before)
	}
	if err := provider.SetBareModelIDs(true); err != nil {
		t.Fatal(err)
	}
	after := list()
	if !slices.Equal(after[:len(before)], before) || !slices.Contains(after[len(before):], "gpt-5.6-sol") {
		t.Fatalf("on: %v, want %v and then gpt-5.6-sol", after, before)
	}
	// the text list a client takes its models from has it too
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models?format=text", nil))
	if !slices.Contains(strings.Split(rec.Body.String(), "\n"), "gpt-5.6-sol") {
		t.Fatalf("text list %q", rec.Body.String())
	}
	// and one model looked up by it
	rec = httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models/gpt-5.6-sol", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"id":"gpt-5.6-sol"`) {
		t.Fatalf("GET /v1/models/gpt-5.6-sol: %d %s", rec.Code, rec.Body.String())
	}

	for _, c := range []struct{ path, body string }{
		{"/v1/chat/completions", `{"model":"gpt-5.6-sol","messages":[{"role":"user","content":"hi"}]}`},
		{"/v1/responses", `{"model":"gpt-5.6-sol","input":"hi"}`},
		{"/v1/messages", `{"model":"gpt-5.6-sol","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`},
	} {
		asked = nil
		req := httptest.NewRequest("POST", c.path, strings.NewReader(c.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "python-requests/2.31") // a client uni-switch doesn't know, as CLIProxyAPI's are
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, req)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "pong") || rec.Header().Get("X-Uni-Switch-Provider") != "codex" {
			t.Fatalf("%s: %d by %q %s", c.path, rec.Code, rec.Header().Get("X-Uni-Switch-Provider"), rec.Body.String())
		}
		if !slices.Equal(asked, []string{"gpt-5.6-sol"}) {
			t.Fatalf("%s asked the backend for %v", c.path, asked)
		}
	}

	if err := provider.SetBareModelIDs(false); err != nil {
		t.Fatal(err)
	}
	if got := list(); !slices.Equal(got, before) {
		t.Fatalf("off again: %v, want %v", got, before)
	}
}

// Two providers serving one bare id list it once, and a request for it
// goes where uni-switch sends a bare id: the group it found for it, first
// member first, or with found groups off the first provider in the
// Providers order; the listed row is where that is. A gateway key is
// listed a bare id only where its request would be served, another uni-switch
// none (its provider would list each model twice), and an id that names a
// provider of the user's ("vendor/m9") isn't listed bare.

// held to b/*: glm-5.3 bare reaches a first, which the key may not use
