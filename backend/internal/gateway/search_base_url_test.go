package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A search API set at a base URL of the user's own (#1580: an endpoint
// that answers as Tavily, Exa… do) is asked there, at the vendor's path
// under it, with the key carried the vendor's way; the vendor's own
// address is never asked.
func TestSearchAPIAtItsOwnBaseURL(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	type got struct{ method, path, auth string }
	var mu sync.Mutex
	var asked []got
	own := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, got{r.Method, r.URL.Path, r.Header.Get("Authorization") + r.Header.Get("x-api-key") + r.Header.Get("X-Subscription-Token")})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/res/v1/web/search") {
			io.WriteString(w, `{"web":{"results":[{"title":"Go downloads","url":"https://go.dev/dl/","description":"Go 1.27.1"}]}}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/v2/search") {
			io.WriteString(w, `{"success":true,"data":{"web":[{"title":"Go downloads","url":"https://go.dev/dl/","description":"Go 1.27.1"}]}}`)
			return
		}
		io.WriteString(w, `{"results":[{"title":"Go downloads","url":"https://go.dev/dl/","content":"Go 1.27.1"}]}`)
	}))
	t.Cleanup(own.Close)
	s := New()
	for _, c := range []struct {
		vendor, key string
		want        got
	}{
		{"tavily", "tvly-k", got{"POST", "/relay/tavily/search", "Bearer tvly-k"}},
		{"exa", "exa-k", got{"POST", "/relay/exa/search", "exa-k"}},
		{"brave", "brave-k", got{"GET", "/relay/brave/res/v1/web/search", "brave-k"}},
		{"firecrawl", "fc-k", got{"POST", "/relay/firecrawl/v2/search", "Bearer fc-k"}},
	} {
		// as Settings saves it: the base URL typed with a slash at its end
		if err := provider.SetSearchAPI(provider.SearchAPI{Vendor: c.vendor, Key: c.key, URL: own.URL + "/relay/" + c.vendor + "/"}); err != nil {
			t.Fatal(err)
		}
		var a provider.SearchAPI
		for _, x := range provider.SearchAPIs() {
			if x.Vendor == c.vendor {
				a = x
			}
		}
		mu.Lock()
		asked = nil
		mu.Unlock()
		pages, err := s.askSearchAPIKeys(context.Background(), a, "latest go")
		if err != nil || len(pages) != 1 || pages[0].URL != "https://go.dev/dl/" {
			t.Errorf("%s: %v %+v", c.vendor, err, pages)
		}
		mu.Lock()
		if len(asked) != 1 || asked[0] != c.want {
			t.Errorf("%s asked %+v, want %+v", c.vendor, asked, c.want)
		}
		mu.Unlock()
	}
}

// The key goes only to the base URL set: an endpoint there that sends the
// search on to another host doesn't take the key with it (Go keeps Brave's
// and Exa's own key headers across such a redirect), and the error says
// where it was sent.
func TestSearchKeyStaysAtItsBaseURL(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var mu sync.Mutex
	var elsewhere []string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		elsewhere = append(elsewhere, r.Header.Get("Authorization")+r.Header.Get("x-api-key")+r.Header.Get("X-Subscription-Token"))
		mu.Unlock()
		io.WriteString(w, `{"results":[{"title":"Go downloads","url":"https://go.dev/dl/","content":"Go 1.27.1"}],"web":{"results":[{"title":"Go","url":"https://go.dev/dl/"}]}}`)
	}))
	t.Cleanup(other.Close)
	// the same address by another name is another host
	away := strings.Replace(other.URL, "127.0.0.1", "localhost", 1)
	own := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, away+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(own.Close)
	s := New()
	for _, vendor := range []string{"exa", "brave", "tavily"} {
		a := provider.SearchAPI{Vendor: vendor, Key: vendor + "-secret", URL: own.URL}
		_, err := s.askSearchAPIKeys(context.Background(), a, "q")
		if err == nil || !strings.Contains(err.Error(), away) {
			t.Errorf("%s: %v", vendor, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	for _, k := range elsewhere {
		if k != "" {
			t.Errorf("a key reached another host: %q", k)
		}
	}
	if len(elsewhere) != 0 {
		t.Errorf("the search was sent on to another host %d times", len(elsewhere))
	}
}

// A base URL given with the path uni-switch puts after it (…/search for
// Tavily) is asked at that path once, never /search/search (plugins#509,
// chenjiangzi): for every vendor, whether Settings saved it so or
// providers.json held it so from before; and Settings keeps it as its base.
func TestSearchAPIBaseURLWithItsPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var mu sync.Mutex
	var asked []string
	own := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{}`)
	}))
	t.Cleanup(own.Close)
	s := New()
	for _, v := range provider.SearchVendors {
		if len(v.Paths) == 0 {
			t.Fatalf("%s says no path", v.ID)
		}
		path := v.Paths[0] // the one asked with a key
		at := own.URL + "/relay/" + v.ID
		for _, given := range []string{at + path, at + path + "/", at + strings.ToLower(path)} {
			for _, saved := range []bool{true, false} {
				a := provider.SearchAPI{Vendor: v.ID, Key: "k-" + v.ID, URL: given}
				if saved {
					if err := provider.SetSearchAPI(a); err != nil {
						t.Fatal(err)
					}
					i := slices.IndexFunc(provider.StoredSearchAPIs(), func(x provider.SearchAPI) bool { return x.Vendor == v.ID })
					if got := provider.StoredSearchAPIs()[i].URL; got != at {
						t.Errorf("%s: %q saved as %q, want %q", v.ID, given, got, at)
					}
				}
				mu.Lock()
				asked = nil
				mu.Unlock()
				s.askSearchAPI(context.Background(), a, "latest go")
				mu.Lock()
				if len(asked) != 1 || asked[0] != "/relay/"+v.ID+path {
					t.Errorf("%s at %q (saved %v) asked %q, want %q", v.ID, given, saved, asked, "/relay/"+v.ID+path)
				}
				mu.Unlock()
			}
		}
	}
	// a base that only ends like a path is left alone
	a := provider.SearchAPI{Vendor: "tavily", Key: "tvly-k", URL: own.URL + "/my-search"}
	if a.Base() != own.URL+"/my-search" {
		t.Errorf("base %q", a.Base())
	}
}
