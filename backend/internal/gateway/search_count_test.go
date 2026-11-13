package gateway

import (
	"context"

	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Every built-in search API, and a Kimi Code plan's search service, is
// asked for the count the search names, in its own field.
func TestSearchCountReachesEveryAPI(t *testing.T) {
	fresh(t)
	var mu sync.Mutex
	var last string
	f := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		last = r.URL.RawQuery + " " + string(b)
		mu.Unlock()
		io.WriteString(w, `{}`)
	}))
	t.Cleanup(f.Close)
	s := New()
	ctx := withSearchCount(context.Background(), 12)
	for _, c := range []struct{ vendor, want string }{
		{"tavily", `"max_results":12`},
		{"brave", "count=12"},
		{"exa", `"numResults":12`},
		{"firecrawl", `"limit":12`},
		{"deepseek", `"count":12`},
		{"volcengine", `"Count":12`},
		{"wsa", `"Cnt":20`},
		{"you", `"count":12`},
	} {
		s.askSearchAPI(ctx, provider.SearchAPI{Vendor: c.vendor, Key: "k", URL: f.URL}, "latest go")
		mu.Lock()
		if !strings.Contains(last, c.want) {
			t.Errorf("%s was asked %q, not %s", c.vendor, last, c.want)
		}
		mu.Unlock()
	}
	s.askSearchAPI(withSearchCount(context.Background(), 3), provider.SearchAPI{Vendor: "wsa", Key: "k", URL: f.URL}, "q")
	mu.Lock()
	if !strings.Contains(last, `"Cnt":10`) {
		t.Errorf("Yuanbao Search for 3 was asked %q, not its fewest, 10", last)
	}
	mu.Unlock()

	kimi := newKimiPlan(t)
	plan := provider.Provider{ID: "kimi", Name: "Kimi Code", Preset: "kimi-code-cn", Key: "sk-kimi",
		Chat: kimi.URL + "/coding/v1", Models: []string{"kimi-for-coding"}}
	for _, n := range []int{0, 12} {
		ctx := context.Background()
		want := "| 6,false,30"
		if n > 0 {
			ctx, want = withSearchCount(ctx, n), fmt.Sprint("| ", n, ",false,30")
		}
		s.kimiSearch(ctx, plan, "latest go")
		kimi.mu.Lock()
		got := kimi.searched[len(kimi.searched)-1]
		kimi.mu.Unlock()
		if !strings.HasSuffix(got, want) {
			t.Errorf("Kimi Code's search asked %q, want limit %d", got, n)
		}
	}
}
