package gateway

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The ChatGPT backend is spoken to over HTTP/1.1, as Codex and Pi speak
// to it, while every other vendor offering HTTP/2 still gets it: on one
// HTTP/2 connection every Codex stream was reset now and then mid-reply
// (jasonz3g on X, "stream ID 21; INTERNAL_ERROR; received from peer").
func TestCodexBackendOverHTTP1(t *testing.T) {
	fresh(t)
	proto := func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, r.Proto) }
	chatgpt := httptest.NewUnstartedServer(http.HandlerFunc(proto))
	chatgpt.EnableHTTP2 = true
	chatgpt.StartTLS()
	t.Cleanup(chatgpt.Close)
	other := httptest.NewUnstartedServer(http.HandlerFunc(proto))
	other.EnableHTTP2 = true
	other.StartTLS()
	t.Cleanup(other.Close)
	pool := x509.NewCertPool()
	pool.AddCert(chatgpt.Certificate())
	pool.AddCert(other.Certificate())
	was, wasTLS := provider.CodexBase, upstreamTLS
	provider.CodexBase = chatgpt.URL + "/backend-api/codex"
	upstreamTLS = &tls.Config{RootCAs: pool}
	t.Cleanup(func() { provider.CodexBase, upstreamTLS = was, wasTLS })
	s := New()
	got := func(u string) string {
		t.Helper()
		res, err := s.client.Post(u, "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return string(b)
	}
	if p := got(provider.CodexBase + "/responses"); p != "HTTP/1.1" {
		t.Errorf("the ChatGPT backend was asked over %s, not HTTP/1.1", p)
	}
	if p := got(other.URL + "/v1/responses"); p != "HTTP/2.0" {
		t.Errorf("another vendor was asked over %s, not HTTP/2.0", p)
	}
}
