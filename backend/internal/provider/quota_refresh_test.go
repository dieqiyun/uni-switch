package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agentenv"
)

// A card's refresh button reads that card alone (Hu9956, #840): the key
// it is for is asked again and the cache has its new figure, while the
// provider's other key and another provider's card aren't asked and keep
// what they read.
func TestRefreshOneCard(t *testing.T) {
	isolate(t)
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	t.Setenv("PATH", h)
	for _, v := range agentenv.Vars {
		t.Setenv(v, "")
	}
	keyBalanceCache.data = nil
	t.Cleanup(func() { keyBalanceCache.data = nil })

	var mu sync.Mutex
	asked := map[string]int{}
	left := map[string]string{"Bearer sk-one": "1000000", "Bearer sk-two": "250000", "Bearer sk-other": "500000"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		k := r.Header.Get("Authorization")
		asked[k]++
		w.Write([]byte(`{"data":{"total_available":` + left[k] + `}}`))
	}))
	defer srv.Close()
	for _, p := range []Provider{
		{ID: "relay", Name: "Relay", Chat: srv.URL + "/v1", Key: "sk-one", KeyName: "main", Keys: []KeyAccount{{Name: "spare", Key: "sk-two"}}},
		{ID: "other", Name: "Other", Chat: srv.URL + "/v1", Key: "sk-other"},
	} {
		p.BalanceURL, p.BalancePath = srv.URL+"/api/usage/token", "$data.total_available / 500000"
		if err := Save(p); err != nil {
			t.Fatal(err)
		}
	}
	cards := func() string {
		var lines []string
		for _, q := range KeyBalances(context.Background()) {
			lines = append(lines, q.Provider+"|"+q.User+"|"+q.Balance)
		}
		return strings.Join(lines, ",")
	}
	if got, want := cards(), "relay|main|$2.00,relay|spare|$0.50,other||$1.00"; got != want {
		t.Fatalf("balances %s, want %s", got, want)
	}
	mu.Lock()
	for k := range left {
		left[k] = "100000"
	}
	clear(asked)
	mu.Unlock()

	// the menu bar, showing the same cards, hears of the read at once,
	// not at its next tick minutes later (#2357)
	var told atomic.Int32
	OnSubscriptionUsage(func() { told.Add(1) })
	t.Cleanup(func() { OnSubscriptionUsage(nil) })
	RefreshUsage(context.Background(), "relay", "Spare")
	if got, want := cards(), "relay|main|$2.00,relay|spare|$0.20,other||$1.00"; got != want {
		t.Fatalf("after spare's refresh %s, want %s", got, want)
	}
	if told.Load() == 0 {
		t.Fatal("a card read again on its own didn't tell the menu bar")
	}
	if len(asked) != 1 || asked["Bearer sk-two"] != 1 {
		t.Fatalf("asked %v, want sk-two alone", asked)
	}
	// a card of one account: every key of the provider's is its
	clear(asked)
	RefreshUsage(context.Background(), "other", "")
	if got, want := cards(), "relay|main|$2.00,relay|spare|$0.20,other||$0.20"; got != want {
		t.Fatalf("after other's refresh %s, want %s", got, want)
	}
	if len(asked) != 1 || asked["Bearer sk-other"] != 1 {
		t.Fatalf("asked %v, want sk-other alone", asked)
	}
}
