package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// youAsked is one request a fake You.com was asked.
type youAsked struct {
	method, url, key, agent, body string
}

// youStub answers every request as You.com did (testdata/you), with no
// network: free_search.json is a real answer of its free search, the
// keyed Search API's answer having the same shape (its API reference);
// keyed_402.json is the keyed API's real answer to a key with no credit.
type youStub struct {
	mu    sync.Mutex
	asked []youAsked
	code  int
	file  string
}

func (y *youStub) RoundTrip(r *http.Request) (*http.Response, error) {
	var body string
	if r.Body != nil {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
	}
	y.mu.Lock()
	y.asked = append(y.asked, youAsked{r.Method, r.URL.String(), r.Header.Get("X-API-Key"), r.Header.Get("User-Agent"), body})
	y.mu.Unlock()
	b, err := os.ReadFile("testdata/you/" + y.file)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: y.code, Status: http.StatusText(y.code), Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(string(b))), Request: r}, nil
}

func youServer(t *testing.T, code int, file string) (*Server, *youStub) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	y := &youStub{code: code, file: file}
	s := New()
	s.client = &http.Client{Transport: y}
	return s, y
}

// #1626: You.com is asked with no key at its free search, and with a key
// at its Search API, its key in X-API-Key; both read into the pages found.
func TestYouSearch(t *testing.T) {
	for _, c := range []struct {
		name   string
		api    provider.SearchAPI
		method string
		url    string
		key    string
	}{
		{"free", provider.SearchAPI{Vendor: "you"}, "GET", "https://api.you.com/v1/agents/search?count=6&query=go+context", ""},
		{"keyed", provider.SearchAPI{Vendor: "you", Key: "ydc-k"}, "POST", "https://ydc-index.io/v1/search", "ydc-k"},
		{"own address", provider.SearchAPI{Vendor: "you", URL: "https://you.example.com/"}, "GET", "https://you.example.com/v1/agents/search?count=6&query=go+context", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, y := youServer(t, 200, "free_search.json")
			pages, err := s.askSearchAPI(context.Background(), c.api, "go context")
			if err != nil {
				t.Fatal(err)
			}
			if len(y.asked) != 1 {
				t.Fatalf("asked %+v", y.asked)
			}
			a := y.asked[0]
			if a.method != c.method || a.url != c.url || a.key != c.key || !strings.HasPrefix(a.agent, SearchAgent+" ") {
				t.Fatalf("asked %+v", a)
			}
			if c.method == "POST" && a.body != `{"count":6,"query":"go context"}` {
				t.Fatalf("body %s", a.body)
			}
			if len(pages) != 3 || pages[0].URL != "https://golangbot.com/context-timeout-cancellation/" ||
				pages[0].Title != "Golang Context - Cancellation, Timeout and Propagation | golangbot.com" ||
				!strings.HasPrefix(pages[0].Text, "Context is a package in the standard library") ||
				pages[0].Age != "2026-07-08T10:28:15" || pages[2].URL == "" || pages[2].Text == "" {
				t.Fatalf("pages %+v", pages)
			}
		})
	}
}

// A You.com key with no credit left is a refusal of the key: the next
// key, or the next search API, is asked.
func TestYouKeyOutOfCredit(t *testing.T) {
	s, _ := youServer(t, 402, "keyed_402.json")
	_, err := s.askSearchAPI(context.Background(), provider.SearchAPI{Vendor: "you", Key: "ydc-k"}, "q")
	var r searchRefused
	if !errors.As(err, &r) || r.status != 402 || !strings.Contains(err.Error(), "prepaid credit balance has been depleted") {
		t.Fatalf("%v", err)
	}
}

// You.com, like SearXNG, can be added with no key; the others still can't.
func TestYouNeedsNoKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := provider.SetSearchAPI(provider.SearchAPI{Vendor: "you"}); err != nil {
		t.Fatal(err)
	}
	if as := provider.SearchAPIs(); len(as) != 1 || as[0].Vendor != "you" || !as[0].Ready() {
		t.Fatalf("%+v", as)
	}
	if err := provider.SetSearchAPI(provider.SearchAPI{Vendor: "tavily"}); err == nil {
		t.Fatal("Tavily added with no key")
	}
}
