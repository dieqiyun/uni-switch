package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// ownOutputUpstream answers every API uni-switch may ask on, whole, and keeps
// each body it was sent. A Chat body naming max_tokens past refuseOver is
// turned away as a vendor whose limit is lower than its list says does.
type ownOutputUpstream struct {
	mu         sync.Mutex
	got        []map[string]any
	refuseOver int
}

func (u *ownOutputUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var q map[string]any
	json.Unmarshal(b, &q)
	u.mu.Lock()
	u.got = append(u.got, q)
	refuse := u.refuseOver
	u.mu.Unlock()
	stream := q["stream"] == true
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	switch {
	case strings.HasSuffix(r.URL.Path, "/responses") && stream:
		io.WriteString(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"r\",\"status\":\"in_progress\",\"output\":[]}}\n\n"+
			"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":0,\"delta\":\"ok\"}\n\n"+
			"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"o\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\",\"annotations\":[]}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	case strings.HasSuffix(r.URL.Path, "/responses"):
		io.WriteString(w, `{"id":"r","object":"response","status":"completed","model":"m","output":[{"type":"message","id":"o","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok","annotations":[]}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
	default:
		if n, _ := q["max_tokens"].(float64); refuse > 0 && int(n) > refuse {
			w.WriteHeader(400)
			io.WriteString(w, `{"error":{"message":"Invalid max_tokens value, the valid range of max_tokens is [1, 32768]","type":"invalid_request_error"}}`)
			return
		}
		if stream {
			io.WriteString(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":null}]}\n\n"+
				"data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n"+
				"data: [DONE]\n\n")
			return
		}
		io.WriteString(w, `{"id":"c","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}
}

func (u *ownOutputUpstream) take() []map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	got := u.got
	u.got = nil
	return got
}

// Codex names no max_output_tokens, and DeepSeek's API reads none as 8K
// without thinking, 64K with it, where its models give 384K: Codex's turns
// on DeepSeek stopped there, "Incomplete response returned, reason:
// max_output_tokens" (xuanwo on X). A request naming no reply limit is
// sent the model's own there, relayed or translated, on every API a client
// may speak; a limit the client named goes as it is, and the same request
// to another vendor's Responses or Chat, relayed, goes as it came.
func TestShortDefaultAsksTheModelsOutput(t *testing.T) {
	fresh(t)
	u := &ownOutputUpstream{}
	up := httptest.NewServer(u)
	t.Cleanup(up.Close)
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	if err := os.WriteFile(catalog.CachePath(), []byte(`{"deepseek":{"id":"deepseek","models":{
		"deepseek-flash":{"id":"deepseek-flash","limit":{"context":1048576,"output":393216}}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	for _, p := range []provider.Provider{
		{ID: "deepseek", Name: "DeepSeek", Key: "k", Catalog: "deepseek", Models: []string{"deepseek-flash"},
			Chat: "http://api.deepseek.com/v1", Responses: "http://api.deepseek.com/v1"},
		// the same model on a relay that speaks Responses and Chat: its
		// default isn't DeepSeek's
		{ID: "relay", Name: "Relay", Key: "k", Catalog: "deepseek", Models: []string{"deepseek-flash"},
			Chat: "http://relay.example.com/v1", Responses: "http://relay.example.com/v1"},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	s := New()
	addr := strings.TrimPrefix(up.URL, "http://")
	s.client = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}}
	const chat = `{"model":"%s","messages":[{"role":"user","content":"hi"}]}`
	for _, c := range []struct {
		name, path, body, field string
		want                    int // 0: the field isn't sent
	}{
		{"Codex's turn, relayed", "/v1/responses", `{"model":"deepseek/deepseek-flash","input":"hi"}`, "max_output_tokens", 393216},
		{"Codex's turn, its own limit", "/v1/responses", `{"model":"deepseek/deepseek-flash","input":"hi","max_output_tokens":1000}`, "max_output_tokens", 1000},
		{"a Chat client, relayed", "/v1/chat/completions", strings.Replace(chat, "%s", "deepseek/deepseek-flash", 1), "max_tokens", 393216},
		{"a Chat client, its own limit", "/v1/chat/completions", `{"model":"deepseek/deepseek-flash","max_completion_tokens":2000,"messages":[{"role":"user","content":"hi"}]}`, "max_tokens", 2000},
		{"a Gemini client, translated", "/v1beta/models/deepseek/deepseek-flash:generateContent", `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`, "", 393216},
		// another host's Responses and Chat, relayed: as they came
		{"a relay's Responses", "/v1/responses", `{"model":"relay/deepseek-flash","input":"hi"}`, "max_output_tokens", 0},
		{"a relay's Chat", "/v1/chat/completions", strings.Replace(chat, "%s", "relay/deepseek-flash", 1), "max_tokens", 0},
	} {
		if code, raw := postJSON(t, s, c.path, c.body); code != 200 {
			t.Fatalf("%s: %d %s", c.name, code, raw)
		}
		got := u.take()
		if len(got) != 1 {
			t.Fatalf("%s: %d requests upstream", c.name, len(got))
		}
		field := c.field
		if field == "" {
			// translated: whichever API uni-switch spoke
			for _, k := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens"} {
				if _, ok := got[0][k]; ok {
					field = k
				}
			}
		}
		n, sent := got[0][field].(float64)
		if c.want == 0 && sent || c.want != 0 && int(n) != c.want {
			t.Errorf("%s: %s %v (sent %v), want %d: %v", c.name, field, n, sent, c.want, got[0])
		}
	}
}

// Translated onto another vendor's Chat or Responses, a request naming no
// reply limit is sent its model's own, as on Anthropic's (#1792): the
// client's API reads none as the model's whole, which the vendor's needn't.
// A vendor that turns the figure away (a list saying more than it takes) is
// asked again naming none, as the client did, and from then on.
func TestTranslatedAsksTheModelsOutput(t *testing.T) {
	fresh(t)
	u := &ownOutputUpstream{}
	up := httptest.NewServer(u)
	t.Cleanup(up.Close)
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	if err := os.WriteFile(catalog.CachePath(), []byte(`{"kimi":{"id":"kimi","models":{
		"k-big":{"id":"k-big","limit":{"context":262144,"output":65536}}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	// a vendor serving Chat alone: Codex's turn is translated
	if err := provider.Save(provider.Provider{ID: "v", Name: "V", Key: "k", Catalog: "kimi", Models: []string{"k-big"}, Chat: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	s := New()
	codex := `{"model":"v/k-big","input":"hi"}`
	ask := func() []map[string]any {
		t.Helper()
		if code, raw := postJSON(t, s, "/v1/responses", codex); code != 200 {
			t.Fatalf("%d %s", code, raw)
		}
		return u.take()
	}
	got := ask()
	if len(got) != 1 || got[0]["max_tokens"] != float64(65536) {
		t.Fatalf("translated onto Chat: %v, want max_tokens 65536", got)
	}
	if code, _ := postJSON(t, s, "/v1/responses", `{"model":"v/k-big","input":"hi","max_output_tokens":700}`); code != 200 {
		t.Fatal(code)
	}
	if got := u.take(); len(got) != 1 || got[0]["max_tokens"] != float64(700) {
		t.Fatalf("the client's own limit: %v, want max_tokens 700", got)
	}

	u.mu.Lock()
	u.refuseOver = 32768
	u.mu.Unlock()
	got = ask()
	if len(got) != 2 || got[0]["max_tokens"] != float64(65536) {
		t.Fatalf("refused: %v, want 65536 then none", got)
	}
	if _, ok := got[1]["max_tokens"]; ok {
		t.Fatalf("asked again with a limit: %v", got[1])
	}
	got = ask()
	if len(got) != 1 {
		t.Fatalf("after a refusal: %d requests, want 1 naming none", len(got))
	}
	if _, ok := got[0]["max_tokens"]; ok {
		t.Fatalf("filled in again after the vendor turned it away: %v", got[0])
	}
}
