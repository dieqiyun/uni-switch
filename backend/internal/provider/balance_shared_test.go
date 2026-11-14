package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/agentenv"
)

// mimi on Discord: a sub2api site's keys on the wallet each told its
// $8.39, and the Usage page showed it once a key. Keys whose balance is
// the account's, and moved with the others', are told as one account's;
// one with the same amount that didn't move with them (another account),
// a key with a quota of its own and a key's own query (new-api's
// /api/usage/token) stay each its own.
func TestKeysOfOneAccountShareItsBalance(t *testing.T) {
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

	// the replies as sub2api's /v1/usage gives them (gateway_handler.go's
	// usageUnrestricted and usageQuotaLimited), and new-api's
	// /api/usage/token
	wallet := `{"mode":"unrestricted","isValid":true,"planName":"钱包余额","remaining":8.39,"unit":"USD","balance":8.39}`
	own := `{"mode":"quota_limited","isValid":true,"status":"active","quota":{"limit":20,"used":11.61,"remaining":8.39,"unit":"USD"},"remaining":8.39,"unit":"USD"}`
	token := `{"code":true,"message":"ok","data":{"object":"token_usage","total_granted":5000000,"total_used":805000,"total_available":4195000,"unlimited_quota":false}}`
	var mu sync.Mutex
	var asked []string
	team := wallet // what the third key reads
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		mu.Lock()
		asked = append(asked, key)
		teamReads := team
		mu.Unlock()
		switch key {
		case "sk-team":
			w.Write([]byte(teamReads))
		case "sk-pro", "sk-plus", "sk-other":
			w.Write([]byte(wallet))
		case "sk-quota":
			w.Write([]byte(own))
		case "sk-t1", "sk-t2":
			w.Write([]byte(token))
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()
	if err := Save(Provider{ID: "site", Name: "Site", Chat: srv.URL + "/v1", Key: "sk-pro", KeyName: "1-A031-Pro",
		Keys: []KeyAccount{{Name: "2-S009-Plus", Key: "sk-plus"}, {Name: "3-S010-Team-2", Key: "sk-team"},
			{Name: "other", Key: "sk-other"}, {Name: "quota", Key: "sk-quota"}},
		BalanceURL: srv.URL + "/v1/usage"}); err != nil {
		t.Fatal(err)
	}
	if err := Save(Provider{ID: "panel", Name: "Panel", Chat: srv.URL + "/v1", Key: "sk-t1", KeyName: "t1",
		Keys: []KeyAccount{{Name: "t2", Key: "sk-t2"}}, BalanceURL: srv.URL + "/api/usage/token"}); err != nil {
		t.Fatal(err)
	}
	shared := func() string {
		var lines []string
		for _, q := range KeyBalances(context.Background()) {
			lines = append(lines, q.Provider+"|"+q.User+"|"+q.Balance+"|"+q.SharedBalance)
		}
		return strings.Join(lines, "\n")
	}

	// the first reading: every one $8.39, none seen to move yet, so
	// none is taken for another's account
	first := "site|1-A031-Pro|$8.39|\nsite|2-S009-Plus|$8.39|\nsite|3-S010-Team-2|$8.39|\nsite|other|$8.39|\nsite|quota|$8.39|\npanel|t1|$8.39|\npanel|t2|$8.39|"
	if got := shared(); got != first {
		t.Fatalf("first reading:\n%s\nwant\n%s", got, first)
	}

	// an hour before, the three keys of the account and the quota'd key
	// and new-api's two read $8.80, the other account $8.39 already: the
	// next reading has the three moved together
	before := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	balanceHistMu.Lock()
	bh := balanceHist{}
	for _, k := range []string{"site|1-A031-Pro", "site|2-S009-Plus", "site|3-S010-Team-2", "site|quota", "panel|t1", "panel|t2"} {
		p, u, _ := strings.Cut(k, "|")
		bh[quotaHistKey(p, u)] = []BalancePoint{{At: before, Amount: 8.80}}
	}
	bh[quotaHistKey("site", "other")] = []BalancePoint{{At: before.Add(-time.Hour), Amount: 8.39}, {At: before, Amount: 8.39}}
	writeBalanceHist(bh)
	balanceHistMu.Unlock()
	ForgetBalances()
	want := "site|1-A031-Pro|$8.39|1-A031-Pro\nsite|2-S009-Plus|$8.39|1-A031-Pro\nsite|3-S010-Team-2|$8.39|1-A031-Pro\nsite|other|$8.39|\nsite|quota|$8.39|\npanel|t1|$8.39|\npanel|t2|$8.39|"
	if got := shared(); got != want {
		t.Fatalf("after they moved together:\n%s\nwant\n%s", got, want)
	}
	// as uni-switch quota --json and /v1/uni-switch/quotas tell them
	for _, q := range quotaReport(nil, nil, KeyBalances(context.Background()), time.Now()) {
		if q.User == "2-S009-Plus" && q.SharedBalance != "1-A031-Pro" {
			t.Fatalf("the report's card: %+v", q)
		}
	}

	// the card's refresh reads every key it stands for, so it stays one
	mu.Lock()
	asked = nil
	mu.Unlock()
	ctx := context.WithValue(Asked(context.Background()), cardRefreshKey{}, cardRefresh{"site", "1-A031-Pro"})
	got := KeyBalances(ctx)
	mu.Lock()
	keys := strings.Join(asked, ",")
	mu.Unlock()
	for _, k := range []string{"sk-pro", "sk-plus", "sk-team"} {
		if !strings.Contains(keys, k) {
			t.Fatalf("the shared card's refresh asked %s, not %s", keys, k)
		}
	}
	if strings.Contains(keys, "sk-other") || strings.Contains(keys, "sk-quota") {
		t.Fatalf("the shared card's refresh asked %s", keys)
	}
	for _, q := range got {
		if q.User == "1-A031-Pro" && q.SharedBalance != "1-A031-Pro" {
			t.Fatalf("the refreshed card: %+v", q)
		}
	}
	if got := shared(); got != want {
		t.Fatalf("after the refresh:\n%s\nwant\n%s", got, want)
	}

	// the keys apart again: one read another amount, so it is no longer
	// the others' account
	mu.Lock()
	team = `{"mode":"unrestricted","isValid":true,"planName":"钱包余额","remaining":5.00,"unit":"USD","balance":5.00}`
	mu.Unlock()
	ForgetBalances()
	apart := "site|1-A031-Pro|$8.39|1-A031-Pro\nsite|2-S009-Plus|$8.39|1-A031-Pro\nsite|3-S010-Team-2|$5.00|\nsite|other|$8.39|\nsite|quota|$8.39|\npanel|t1|$8.39|\npanel|t2|$8.39|"
	if got := shared(); got != apart {
		t.Fatalf("one key read another amount:\n%s\nwant\n%s", got, apart)
	}
}

// Two balances moved together when their latest change was the same, from
// and to, at the same time; not when either hasn't changed, or changed
// from another amount, or long apart.
func TestBalancesMovedTogether(t *testing.T) {
	t0 := time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
	pts := func(at time.Time, amounts ...float64) []BalancePoint {
		var out []BalancePoint
		for i, a := range amounts {
			out = append(out, BalancePoint{At: at.Add(time.Duration(i) * time.Hour), Amount: a})
		}
		return out
	}
	for _, c := range []struct {
		name string
		a, b []BalancePoint
		want bool
	}{
		{"the same moves", pts(t0, 9, 8.8, 8.39), pts(t0.Add(3*time.Second), 9, 8.8, 8.39), true},
		{"a run of the last amount", pts(t0, 8.8, 8.39, 8.39), pts(t0, 8.8, 8.39), true},
		{"a key added later", pts(t0, 9, 8.8, 8.39), pts(t0.Add(time.Hour), 8.8, 8.39), true},
		{"never moved", pts(t0, 8.39, 8.39), pts(t0, 8.39, 8.39), false},
		{"one never moved", pts(t0, 8.8, 8.39), pts(t0, 8.39, 8.39), false},
		{"from another amount", pts(t0, 8.8, 8.39), pts(t0, 8.5, 8.39), false},
		{"hours apart", pts(t0, 8.8, 8.39), pts(t0.Add(2*time.Hour), 8.8, 8.39), false},
		{"one point each", pts(t0, 8.39), pts(t0, 8.39), false},
	} {
		if got := movedTogether(c.a, c.b); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
