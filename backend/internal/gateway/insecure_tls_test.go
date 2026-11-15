package gateway

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// lanVendor is a server speaking each API uni-switch asks, behind a
// certificate nothing trusts, as a vLLM or Ollama on the LAN with a
// self-signed cert is (#2383); asked are the paths it was asked.
func lanVendor(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var asked []string
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/models"):
			io.WriteString(w, `{"data":[{"id":"m1"}]}`)
		case strings.HasSuffix(p, "/count_tokens"):
			io.WriteString(w, `{"input_tokens":3}`)
		case strings.HasSuffix(p, "/messages"):
			io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"m1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":1}}`)
		case strings.HasSuffix(p, "/responses"):
			io.WriteString(w, `{"id":"resp_1","object":"response","status":"completed","model":"m1","output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}`)
		case strings.Contains(p, ":streamGenerateContent"):
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":1}}`+"\n\n")
		case strings.Contains(p, ":generateContent"):
			io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":1}}`)
		default:
			io.WriteString(w, `{"id":"c1","object":"chat.completion","model":"m1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`)
		}
	}))
	s.Config.ErrorLog = log.New(io.Discard, "", 0) // the refused handshakes
	s.StartTLS()
	t.Cleanup(s.Close)
	return s, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), asked...) }
}

// Each API the gateway serves reaches a provider of the user's behind a
// self-signed certificate only once it is set not to check it; until
// then, and for another provider of the user's on the same host, the
// handshake is refused and the vendor is never asked.
func TestInsecureTLSEveryRoute(t *testing.T) {
	routes := []struct{ name, id, path, body string }{
		{"chat", "lan-chat", "/v1/chat/completions", `{"model":"lan-chat/m1","messages":[{"role":"user","content":"hi"}]}`},
		{"responses", "lan-resp", "/v1/responses", `{"model":"lan-resp/m1","input":"hi"}`},
		{"anthropic", "lan-anth", "/v1/messages", `{"model":"lan-anth/m1","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`},
		{"count_tokens", "lan-anth", "/v1/messages/count_tokens", `{"model":"lan-anth/m1","messages":[{"role":"user","content":"hi"}]}`},
		{"gemini", "lan-gem", "/v1beta/models/lan-gem/m1:generateContent", `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`},
	}
	for _, unchecked := range []bool{false, true} {
		fresh(t)
		srv, asked := lanVendor(t)
		for _, p := range []provider.Provider{
			{ID: "lan-chat", Name: "LAN Chat", Key: "k", Models: []string{"m1"}, Chat: srv.URL + "/v1", InsecureTLS: unchecked},
			{ID: "lan-resp", Name: "LAN Resp", Key: "k", Models: []string{"m1"}, Responses: srv.URL + "/v1", InsecureTLS: unchecked},
			{ID: "lan-anth", Name: "LAN Anth", Key: "k", Models: []string{"m1"}, Anthropic: srv.URL, InsecureTLS: unchecked},
			{ID: "lan-gem", Name: "LAN Gem", Key: "k", Models: []string{"m1"}, Gemini: srv.URL + "/v1beta", InsecureTLS: unchecked},
			// the same host, checked
			{ID: "lan-checked", Name: "LAN Checked", Key: "k", Models: []string{"m1"}, Chat: srv.URL + "/v1"},
		} {
			if err := provider.Save(p); err != nil {
				t.Fatal(err)
			}
		}
		for _, r := range routes {
			before := len(asked())
			code, out := post(t, r.path, r.body)
			reached := len(asked()) > before
			if unchecked && (!reached || code != 200) {
				t.Errorf("%s with InsecureTLS: %d %s, asked %v", r.name, code, out, asked()[before:])
			}
			if !unchecked && (reached || code == 200) {
				t.Errorf("%s without InsecureTLS: reached the vendor (%d %s)", r.name, code, out)
			}
		}
		before := len(asked())
		if code, out := post(t, "/v1/chat/completions", `{"model":"lan-checked/m1","messages":[{"role":"user","content":"hi"}]}`); code == 200 || len(asked()) > before {
			t.Errorf("a provider on the same host without InsecureTLS reached the vendor: %d %s", code, out)
		}
		noRests()
	}
}
