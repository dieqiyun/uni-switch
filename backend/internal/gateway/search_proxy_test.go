package gateway

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/netproxy"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// connectProxy is an HTTP proxy that notes the host:port of each CONNECT
// and tunnels it to where dial sends that name.
type connectProxy struct {
	ln   net.Listener
	dial func(addr string) (net.Conn, error)
	mu   sync.Mutex
	got  []string
}

func newConnectProxy(t *testing.T, dial func(addr string) (net.Conn, error)) *connectProxy {
	t.Helper()
	var ln net.Listener
	for range 20 { // a free port, never one of uni-switch's own (3425-3437)
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		if p := l.Addr().(*net.TCPAddr).Port; p >= 3425 && p <= 3437 {
			l.Close()
			continue
		}
		ln = l
		break
	}
	if ln == nil {
		t.Fatal("no free port")
	}
	p := &connectProxy{ln: ln, dial: dial}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go p.serve(c)
		}
	}()
	return p
}

func (p *connectProxy) URL() string { return "http://" + p.ln.Addr().String() }

func (p *connectProxy) serve(c net.Conn) {
	defer c.Close()
	br := bufio.NewReader(c)
	req, err := http.ReadRequest(br)
	if err != nil || req.Method != http.MethodConnect {
		io.WriteString(c, "HTTP/1.1 405 Method Not Allowed\r\nContent-Length: 0\r\n\r\n")
		return
	}
	p.mu.Lock()
	p.got = append(p.got, req.Host)
	p.mu.Unlock()
	up, err := p.dial(req.Host)
	if err != nil {
		io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	defer up.Close()
	io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n")
	done := make(chan struct{}, 2)
	go func() { io.Copy(up, br); done <- struct{}{} }()
	go func() { io.Copy(c, up); done <- struct{}{} }()
	<-done
}

func (p *connectProxy) connects() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.got)
}

func (p *connectProxy) reset() {
	p.mu.Lock()
	p.got = nil
	p.mu.Unlock()
}

// A web search uni-switch makes for a turn goes through the proxy its searcher
// follows, not the one the turn's provider is set to (#1522): Claude Code
// on opencode-go, set to "direct", with Codex searching and the proxy on
// Auto, had the search to chatgpt.com dialed directly, to the poisoned
// addresses DNS gave, until it gave up 2 minutes later. Here the turn's
// provider is reached directly, the searcher and the search APIs only
// through the global proxy: set in Settings, or followed from the
// environment as Auto does.
func TestSearchFollowsTheSearchersProxy(t *testing.T) {
	searchSandbox(t)
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy"} {
		t.Setenv(k, "")
	}
	// the turn's model can't search: it asks for one, then answers with
	// what it was told was found
	turn := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			io.WriteString(w, `{"data":[{"id":"flash"}]}`)
			return
		}
		var q struct {
			Messages []map[string]any `json:"messages"`
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &q)
		w.Header().Set("Content-Type", "text/event-stream")
		if last := q.Messages[len(q.Messages)-1]; last["role"] == "tool" {
			said, _ := last["content"].(string)
			io.WriteString(w, sse(`data: {"id":"d","choices":[{"index":0,"delta":{"role":"assistant","content":"told: `+strconv.Quote(said)[1:len(strconv.Quote(said))-1]+`"}}]}`,
				`data: {"id":"d","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
				`data: [DONE]`))
			return
		}
		io.WriteString(w, sse(`data: {"id":"c","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"latest go\"}"}}]}}]}`,
			`data: {"id":"c","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":30,"completion_tokens":7}}`,
			`data: [DONE]`))
	}))
	t.Cleanup(turn.Close)
	// the searcher, on an Anthropic API that runs web_search_20250305
	searcherSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/models") {
			io.WriteString(w, `{"data":[{"id":"m1"}]}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"m","type":"message","role":"assistant","model":"m1","content":[
			{"type":"web_search_tool_result","tool_use_id":"s","content":[{"type":"web_search_result","title":"Go","url":"https://go.dev/dl/"}]},
			{"type":"text","text":"Go 1.27.1 is out."}],"stop_reason":"end_turn"}`)
	}))
	t.Cleanup(searcherSrv.Close)
	// a search API, for when the providers don't search
	api := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"query":"x","results":[{"title":"Go downloads","url":"https://go.dev/dl/","content":"go1.27.1 from the API.","score":0.9}]}`)
	}))
	t.Cleanup(api.Close)
	// a Kimi Code plan's search service
	kimi := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"search_results":[{"site_name":"Go","title":"Go downloads","url":"https://go.dev/dl/","snippet":"go1.27.1 from Kimi.","date":"2026-09-30"}]}`)
	}))
	t.Cleanup(kimi.Close)
	where := map[string]string{
		"kimi.test":   kimi.Listener.Addr().String(),
		"turn.test":   turn.Listener.Addr().String(),
		"search.test": searcherSrv.Listener.Addr().String(),
		"api.test":    api.Listener.Addr().String(),
	}
	at := func(addr string) (string, bool) {
		host, _, _ := net.SplitHostPort(addr)
		a, ok := where[host]
		return a, ok
	}
	// through the proxy every name is reached
	proxy := newConnectProxy(t, func(addr string) (net.Conn, error) {
		if a, ok := at(addr); ok {
			addr = a
		}
		return net.DialTimeout("tcp", addr, 5*time.Second)
	})
	// directly, only the turn's provider is: the others are black holes,
	// as chatgpt.com's poisoned addresses were
	var d net.Dialer
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		if host, _, _ := net.SplitHostPort(addr); host == "turn.test" {
			a, _ := at(addr)
			return d.DialContext(ctx, network, a)
		}
		if _, ok := at(addr); ok {
			return nil, errors.New("dial tcp " + addr + ": connect: operation timed out (dialed directly)")
		}
		return d.DialContext(ctx, network, addr)
	}
	searchesOn(t, provider.Anthropic, "https://search.test")
	// the model lists, fetched once beforehand, reach every name
	defaultTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if a, ok := at(addr); ok {
				addr = a
			}
			return d.DialContext(ctx, network, addr)
		}}
	for _, p := range []provider.Provider{
		{ID: "turn", Name: "Turn", Key: "k", Chat: "https://turn.test/v1", Models: []string{"flash"}, Proxy: "direct"},
		{ID: "srch", Name: "Searcher", Key: "k", Anthropic: "https://search.test/anthropic", Models: []string{"m1"}},
	} {
		save(t, p)
	}
	http.DefaultClient.Transport = defaultTransport
	setSearcher := func(v string) {
		t.Helper()
		st := settings.Load()
		st.Searcher = v
		if err := settings.Save(st); err != nil {
			t.Fatal(err)
		}
	}
	setSearcher("srch/m1")

	s := New()
	s.client = &http.Client{Transport: netproxy.Dispatch(&http.Transport{
		Proxy: netproxy.Func, DialContext: dial, ForceAttemptHTTP2: true,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, ResponseHeaderTimeout: 10 * time.Second,
	})}
	ask := func(what, want string, through []string) {
		t.Helper()
		proxy.reset()
		s.client.CloseIdleConnections() // each case dials afresh
		body := `{"model":"turn/flash","max_tokens":3000,"stream":false,"messages":[{"role":"user","content":"search: latest go"}],"tools":[{"type":"web_search_20250305","name":"web_search"}]}`
		rec := httptest.NewRecorder()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body)).WithContext(ctx))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("%s: %d %s (CONNECTs %v)", what, rec.Code, rec.Body.String(), proxy.connects())
		}
		if got := proxy.connects(); !slices.Equal(got, through) {
			t.Fatalf("%s: CONNECTs %v, want %v", what, got, through)
		}
	}

	// the proxy set in Settings
	st := settings.Load()
	st.Proxy = proxy.URL()
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	ask("the searcher, Settings' proxy", "Go 1.27.1 is out.", []string{"search.test:443"})

	// Auto: the proxy followed from the environment
	st = settings.Load()
	st.Proxy = ""
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	// (ALL_PROXY: Go reads HTTPS_PROXY once a process, before this test)
	t.Setenv("ALL_PROXY", proxy.URL())
	ask("the searcher, Auto", "Go 1.27.1 is out.", []string{"search.test:443"})

	// a searcher with a proxy of its own goes through it, though the
	// global one is direct: a Kimi Code plan's search service too
	st = settings.Load()
	st.Proxy = "direct"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "kimi", Name: "Kimi Code", Preset: "kimi-code-cn", Key: "sk-kimi",
		Chat: "https://kimi.test/coding/v1", Models: []string{"kimi-for-coding"}, Proxy: proxy.URL()}); err != nil {
		t.Fatal(err)
	}
	setSearcher("kimi")
	ask("Kimi Code's search, its own proxy", "go1.27.1 from Kimi.", []string{"kimi.test:443"})
	st = settings.Load()
	st.Proxy = ""
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}

	// the search APIs, with the providers' search off, go the same way
	setSearcher("off")
	if err := provider.SetSearchAPI(provider.SearchAPI{Vendor: "tavily", Key: "tvly-k", URL: "https://api.test/tavily"}); err != nil {
		t.Fatal(err)
	}
	ask("the search API, Auto", "go1.27.1 from the API.", []string{"api.test:443"})
}
