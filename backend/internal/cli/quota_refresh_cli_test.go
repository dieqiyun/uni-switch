package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/agentenv"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// uni-switch quota refresh <provider> reads that card again, as its refresh
// button on the Usage page does, and shows it alone: the other providers'
// vendors aren't asked, and a second run within 30 seconds asks nobody and
// shows what was read (#2104).
func TestQuotaRefreshReadsOneCard(t *testing.T) {
	groupsHome(t)
	for _, k := range agentenv.Vars {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	bin := t.TempDir()
	for _, name := range []string{"security", "secret-tool", "claude", "codex", "cursor-agent", "devin", "grok", "kiro-cli"} {
		testenv.Program(t, filepath.Join(bin, name), "#!/bin/sh\nexit 1\n")
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	var mu sync.Mutex
	asked := map[string]int{}
	balance := map[string]string{"a": "7", "b": "9"}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/")
		mu.Lock()
		asked[id]++
		v := balance[id]
		mu.Unlock()
		fmt.Fprintf(w, `{"balance":%q}`, v)
	}))
	t.Cleanup(up.Close)
	for _, id := range []string{"a", "b"} {
		p, err := provider.Find(id)
		if err != nil {
			t.Fatal(err)
		}
		p.BalanceURL, p.BalancePath = up.URL+"/"+id, "balance"
		if err := provider.Save(*p); err != nil {
			t.Fatal(err)
		}
	}
	provider.ForgetBalances()
	count := func(id string) int {
		mu.Lock()
		defer mu.Unlock()
		return asked[id]
	}

	out, err := stdoutOf(t, func() error { return quotaCmd([]string{"quota", "refresh", "a", "--json"}) })
	if err != nil {
		t.Fatal(err)
	}
	var qs []map[string]any
	if err := json.Unmarshal([]byte(out), &qs); err != nil {
		t.Fatal(err, out)
	}
	if len(qs) != 1 || qs[0]["provider"] != "a" || qs[0]["balance"] != "7.00" {
		t.Fatalf("refresh a showed %s", out)
	}
	if count("a") != 1 || count("b") != 0 {
		t.Fatalf("asked a %d times, b %d times; want 1 and 0", count("a"), count("b"))
	}

	// within 30 seconds: not asked again, the reading shown as it was
	mu.Lock()
	balance["a"] = "6"
	mu.Unlock()
	out, err = stdoutOf(t, func() error { return quotaCmd([]string{"quota", "refresh", "a"}) })
	if err != nil {
		t.Fatal(err)
	}
	if count("a") != 1 || !strings.Contains(out, "7.00") || strings.Contains(out, "b ") {
		t.Fatalf("a second refresh at once asked a %d times and showed\n%s", count("a"), out)
	}

	// once the 30 seconds are over, asked again
	stamps := readRefreshStamps()
	for k := range stamps {
		stamps[k] = stamps[k].Add(-quotaRefreshGap)
	}
	b, _ := json.Marshal(stamps)
	if err := os.WriteFile(refreshStampsPath(), b, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err = stdoutOf(t, func() error { return quotaCmd([]string{"quota", "refresh", "a"}) })
	if err != nil {
		t.Fatal(err)
	}
	if count("a") != 2 || !strings.Contains(out, "6.00") {
		t.Fatalf("after 30s asked a %d times and showed\n%s", count("a"), out)
	}

	// a provider with no card says so
	if _, err := stdoutOf(t, func() error { return quotaCmd([]string{"quota", "refresh", "nosuch"}) }); err == nil {
		t.Fatal("refresh of no provider succeeded")
	}
}

// What makes an account's card count as asked within the gap: a whole
// provider's refresh covers each account, one account's covers neither the
// provider's whole nor another account.
func TestQuotaRefreshGapPerCard(t *testing.T) {
	groupsHome(t)
	now := time.Now()
	noteRefreshed("codex", "a@x.com", now)
	if _, ok := refreshedWithin("codex", "", now); ok {
		t.Error("one account's refresh held the provider's")
	}
	if _, ok := refreshedWithin("codex", "b@x.com", now); ok {
		t.Error("one account's refresh held another's")
	}
	if _, ok := refreshedWithin("codex", "A@x.com", now.Add(10*time.Second)); !ok {
		t.Error("the account's own refresh didn't hold it")
	}
	if _, ok := refreshedWithin("codex", "a@x.com", now.Add(quotaRefreshGap)); ok {
		t.Error("held past the gap")
	}
	noteRefreshed("claude", "", now)
	if _, ok := refreshedWithin("claude", "c@x.com", now); !ok {
		t.Error("the provider's refresh didn't hold its account")
	}
}
