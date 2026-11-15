package provider

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/netproxy"
)

// #1938 (kensenzhao): two Claude accounts, each with its own proxy. uni-switch
// switches Claude Code to max, and a Claude Code started on pro writes
// pro's profile after. Claude Code's sign-in is max's, so its /usage goes
// through max's proxy and is max's reading, not through pro's and shown
// as pro's.
func TestClaudeUsageGoesThroughTheSignedInAccountsProxy(t *testing.T) {
	prof, _ := claudeThree(t)
	if err := Save(Provider{ID: "claude", AccountProxies: map[string]string{"max@x.com": "http://127.0.0.1:17902", "pro@x.com": "http://127.0.0.1:7897"}}); err != nil {
		t.Fatal(err)
	}
	if err := SwitchLogin("claude", "max@x.com"); err != nil {
		t.Fatal(err)
	}
	prof("pro@x.com", "pro@x.com's Organization")
	forgetAccountCaches()
	for _, l := range Logins("claude") {
		if l.Active != (l.User == "max@x.com") {
			t.Errorf("%s active %v; Claude Code is signed in to max@x.com", l.User, l.Active)
		}
	}
	var text atomic.Value
	text.Store("Current session: 17% used\nCurrent week (all models): 38% used")
	fakeClaudeUsage(t, &text, nil)
	// no reading of another test's is kept, nor one of this test's left
	forget := func() {
		lastQuotas.Lock()
		lastQuotas.m, lastQuotas.loaded = nil, false
		lastQuotas.Unlock()
		loginUsageCache.Lock()
		loginUsageCache.m = nil
		loginUsageCache.Unlock()
	}
	forget()
	t.Cleanup(forget)
	var mu sync.Mutex
	var via []string
	UsageClaudeVia(func(ctx context.Context, dir string) (string, error) {
		mu.Lock()
		via = append(via, dir+"|"+netproxy.Choice(ctx))
		mu.Unlock()
		return text.Load().(string), nil
	})
	AskClaudeUsage()
	read := ""
	for _, q := range QuotaReport(Asked(context.Background()), time.Now()) {
		if q.Provider == "claude" && len(q.Windows) > 0 {
			read += q.User
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(via) != 1 || via[0] != "|http://127.0.0.1:17902" {
		t.Errorf("/usage ran %v, want once in Claude Code's own directory through max's proxy", via)
	}
	if read != "max@x.com" {
		t.Errorf("the reading is on %q's card, want max@x.com's", read)
	}
}
