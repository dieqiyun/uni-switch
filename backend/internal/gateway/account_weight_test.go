package gateway

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A subscription routed by weight spreads its requests over its accounts
// by the weight set beside each (rubbzha on X: a Max account and a Pro
// one): me@example.com weighing 3 takes three of every four requests and
// spare@example.com, with none set, the fourth.
func TestAccountWeightSharesRequests(t *testing.T) {
	codexSignedIn(t, "spare@example.com")
	b := newResetBackend(t)
	if err := provider.SetRouting("codex", provider.Weighted); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetAffinity("codex", provider.AffinityOff); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetAccountWeight("codex", "Me@Example.com", 3); err != nil {
		t.Fatal(err)
	}
	srv := New()
	for range 8 {
		if code, body := resetPost(t, srv); code != 200 || !strings.Contains(body, "pong") {
			t.Fatalf("%d %s", code, body)
		}
	}
	tried, _ := b.seen()
	n := map[string]int{}
	for _, a := range strings.Split(tried, ",") {
		n[a]++
	}
	if n["acct-1"] != 6 || n["acct-2"] != 2 {
		t.Fatalf("tried %s: want acct-1 6 times, acct-2 twice", tried)
	}
}
