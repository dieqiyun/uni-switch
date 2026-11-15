package gateway

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"

	"net/http"
	"net/http/httptest"

	"strings"
	"sync"
	"testing"
	"time"
)

// edgeVendor is #5182's Google edge: the connection the first request
// came on is refused for where it comes from, every request on it after
// too, whatever account it carries, while a request on any other
// connection is answered. conns lists each request's connection, in the
// order they came.
type edgeVendor struct {
	srv   *httptest.Server
	mu    sync.Mutex
	bad   string
	conns []string
	who   []string
	// refuse is the refusal the bad connection answers
	refuse string
}

func newEdgeVendor(t *testing.T, h2 bool, refuse string) *edgeVendor {
	v := &edgeVendor{refuse: refuse}
	v.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		switch {
		case strings.Contains(r.URL.Path, ":loadCodeAssist"):
			io.WriteString(w, `{"currentTier":{"id":"standard-tier"},"cloudaicompanionProject":"p1"}`)
			return
		case strings.Contains(r.URL.Path, ":fetchAvailableModels"):
			io.WriteString(w, `{"models":{"m":{}}}`)
			return
		}
		who := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		v.mu.Lock()
		if v.bad == "" {
			v.bad = r.RemoteAddr
		}
		v.conns = append(v.conns, r.RemoteAddr)
		v.who = append(v.who, who)
		bad := r.RemoteAddr == v.bad
		v.mu.Unlock()
		if bad {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, v.refuse)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"from `+who+`"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":3}}}`))
	}))
	v.srv.EnableHTTP2 = h2
	v.srv.StartTLS()
	t.Cleanup(v.srv.Close)
	return v
}

func (v *edgeVendor) asked() (conns, who []string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string(nil), v.conns...), append([]string(nil), v.who...)
}

// trustVendor has the gateway's transports, made after it, trust v's
// certificate.
func trustVendor(t *testing.T, v *httptest.Server) {
	pool := x509.NewCertPool()
	pool.AddCert(v.Certificate())
	was := upstreamTLS
	upstreamTLS = &tls.Config{RootCAs: pool, ServerName: "127.0.0.1"}
	t.Cleanup(func() { upstreamTLS = was })
}

// #5182: the gateway's own transport (upstreamTransport) doesn't send a
// request on the connection a vendor refused one on for where it comes
// from; a refusal of another kind keeps its connection, as any reply's.
// Over HTTP/2, as Google's front speaks, and HTTP/1.1.
func TestRegionRefusalDropsItsConnection(t *testing.T) {
	for _, h2 := range []bool{true, false} {
		for _, tc := range []struct {
			name, refuse string
			fresh        bool
		}{
			{"region", googleRegion400, true},
			{"other 400", `{"error":{"code":400,"message":"Request contains an invalid argument.","status":"INVALID_ARGUMENT"}}`, false},
		} {
			t.Run(fmt.Sprintf("h2=%v/%s", h2, tc.name), func(t *testing.T) {
				fresh(t)
				v := newEdgeVendor(t, h2, tc.refuse)
				trustVendor(t, v.srv)
				s := New()
				u := "https://" + v.srv.Listener.Addr().String() + "/v1internal:streamGenerateContent"
				ask := func() (int, string) {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(`{}`))
					req.Header.Set("Authorization", "Bearer u@example.com")
					res, err := s.client.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					defer res.Body.Close()
					b, _ := io.ReadAll(res.Body)
					if h2 && res.ProtoMajor != 2 || !h2 && res.ProtoMajor != 1 {
						t.Fatalf("spoken %s", res.Proto)
					}
					return res.StatusCode, string(b)
				}
				if code, body := ask(); code != 400 {
					t.Fatalf("first request: %d %s", code, body)
				}
				code, body := ask()
				conns, _ := v.asked()
				if len(conns) != 2 {
					t.Fatalf("asked on %v", conns)
				}
				if tc.fresh {
					if conns[1] == conns[0] {
						t.Fatalf("the request after the region refusal went on the refused connection %s: %d %s", conns[0], code, body)
					}
					if code != 200 {
						t.Fatalf("the new connection: %d %s", code, body)
					}
				} else if conns[1] != conns[0] {
					t.Fatalf("a plain 400 dropped its connection: %v", conns)
				}
			})
		}
	}
}

// regionNote names the model and the vendor for a region refusal, and
// nothing for another 400 or a success that quotes the words.
func TestRegionNote(t *testing.T) {
	if n := regionNote("gemini-3.8-flash", 400, []byte(googleRegion400)); !strings.Contains(n, "Google doesn't serve gemini-3.8-flash") {
		t.Fatalf("Google's: %q", n)
	}
	if n := regionNote("gpt-5", 403, []byte(`{"error":{"code":"unsupported_country_region_territory","message":"Country, region, or territory not supported"}}`)); !strings.HasPrefix(n, "the vendor doesn't serve gpt-5") {
		t.Fatalf("OpenAI's: %q", n)
	}
	for _, c := range []struct {
		status int
		body   string
	}{{400, `{"error":{"code":400,"message":"Request contains an invalid argument.","status":"INVALID_ARGUMENT"}}`}, {200, googleRegion400}} {
		if n := regionNote("m", c.status, []byte(c.body)); n != "" {
			t.Fatalf("%d %s: %q", c.status, c.body, n)
		}
	}
}
