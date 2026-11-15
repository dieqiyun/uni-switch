package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// toolsEffortChat is JJ-Lin's mock of a Chat-only relay (#2670): function
// tools with any reasoning_effort but okEffort are refused as Bedrock's
// are (#176), with "set reasoning_effort to 'none'"; none itself is refused
// naming the levels the model takes (gpt-6.1-sol there), unless noneOK
// (gpt-6-astra there). serves answers everything.
type toolsEffortChat struct {
	mu       sync.Mutex
	efforts  []string
	okEffort string
	noneOK   bool
	serves   bool
	model    string
}

func (c *toolsEffortChat) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var m map[string]any
	json.Unmarshal(body, &m)
	e, _ := m["reasoning_effort"].(string)
	c.mu.Lock()
	c.efforts = append(c.efforts, e)
	c.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, tools := m["tools"]
	switch {
	case c.serves:
	case e == "none" && !c.noneOK:
		w.WriteHeader(400)
		io.WriteString(w, `{"error":{"message":"Unsupported value: 'none' is not supported with the '`+c.model+`' model. Supported values are: 'low', 'medium', 'high', 'xhigh', and 'max'.","type":"invalid_request_error"}}`)
		return
	case tools && e != c.okEffort:
		w.WriteHeader(400)
		io.WriteString(w, `{"error":{"code":null,"message":"Function tools with reasoning_effort are not supported for `+c.model+` in /v1/chat/completions. To use function tools, use /v1/responses or set reasoning_effort to 'none'.","param":null,"type":"invalid_request_error"}}`)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	io.WriteString(w, sse(
		`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"ok from relay"}}]}`,
		`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
		`data: [DONE]`))
}

// asked returns the efforts asked since the last call, and forgets them.
func (c *toolsEffortChat) asked() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.efforts
	c.efforts = nil
	return out
}

// toolsEffortOn saves a Chat-only provider id serving model from up.
func toolsEffortOn(t *testing.T, id, model string, up http.Handler) {
	t.Helper()
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	if err := provider.Save(provider.Provider{ID: id, Name: strings.ToUpper(id), Key: "k", Chat: srv.URL + "/v1", Models: []string{model}}); err != nil {
		t.Fatal(err)
	}
}

func codexToolsAt(model, effort string) string {
	return strings.Replace(fmt.Sprintf(codexToolsBody, model), `"effort":"high"`, `"effort":"`+effort+`"`, 1)
}

// A relay with a Chat URL alone that refuses tools with reasoning (#176)
// and refuses none too (#2670, JJ-Lin): the first request asks none once,
// and isn't asked again at the model's lowest level, refused the same;
// later ones aren't asked none again. Each gets the tools refusal, which
// names the Responses URL the provider lacks, never the none refusal, and
// on one Server, three in a row: before, the first went
// [xhigh none low] and each after [high], the 400 the agent's.
func TestToolsEffortNoneRefused(t *testing.T) {
	fresh(t)
	up := &toolsEffortChat{okEffort: "never", model: "gpt-6.1-sol"}
	toolsEffortOn(t, "relay", "gpt-6.1-sol", up)
	s := New()
	for i, x := range []struct {
		effort string
		want   []string
	}{
		{"xhigh", []string{"xhigh", "none"}},
		{"high", []string{"high"}},
		{"high", []string{"high"}},
	} {
		code, body := postTo(t, s, "/v1/responses", codexToolsAt("relay/gpt-6.1-sol", x.effort))
		got := up.asked()
		if code != 400 || !strings.Contains(body, "Function tools with reasoning_effort") || strings.Contains(body, "'none' is not supported") ||
			!strings.Contains(body, "/v1/responses, which RELAY has no URL for") {
			t.Fatalf("request %d: %d %s", i+1, code, body)
		}
		if !slices.Equal(got, x.want) {
			t.Fatalf("request %d asked %v, want %v", i+1, got, x.want)
		}
	}

	// a chat client's, relayed: the same
	fresh(t)
	toolsEffortOn(t, "relay", "gpt-6.1-sol", up)
	s = New()
	up.asked()
	for i, want := range [][]string{{"high", "none"}, {"high"}} {
		code, body := postTo(t, s, "/v1/chat/completions", fmt.Sprintf(chatToolsBody, "relay/gpt-6.1-sol"))
		if got := up.asked(); code != 400 || !strings.Contains(body, "Function tools with reasoning_effort") || !slices.Equal(got, want) {
			t.Fatalf("chat request %d: %d %s, asked %v", i+1, code, body, got)
		}
	}
}

// Where the relay takes none (gpt-6-astra in #2670), every tools request
// is asked again with it and answered, as #176 had it, and a request with
// no tools goes as it came.
func TestToolsEffortNoneTaken(t *testing.T) {
	fresh(t)
	up := &toolsEffortChat{okEffort: "none", noneOK: true, model: "gpt-6-astra"}
	toolsEffortOn(t, "relay", "gpt-6-astra", up)
	s := New()
	for i, effort := range []string{"xhigh", "high", "high"} {
		code, body := postTo(t, s, "/v1/responses", codexToolsAt("relay/gpt-6-astra", effort))
		if got := up.asked(); code != 200 || !strings.Contains(body, "ok from relay") || !slices.Equal(got, []string{effort, "none"}) {
			t.Fatalf("request %d: %d %s, asked %v", i+1, code, body, got)
		}
	}

	plain := &toolsEffortChat{okEffort: "never", model: "gpt-6.1-sol"}
	toolsEffortOn(t, "plain", "gpt-6.1-sol", plain)
	for i := 0; i < 2; i++ {
		code, body := postTo(t, s, "/v1/chat/completions", `{"model":"plain/gpt-6.1-sol","stream":true,"reasoning_effort":"xhigh","messages":[{"role":"user","content":"hi"}]}`)
		if got := plain.asked(); code != 200 || !slices.Equal(got, []string{"xhigh"}) {
			t.Fatalf("plain %d: %d %s, asked %v", i+1, code, body, got)
		}
	}
}
