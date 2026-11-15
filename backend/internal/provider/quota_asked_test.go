package provider

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/netproxy"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// dialsOut has uni-switch's own requests go as netproxy.Install has them, and
// tells every host dialed that isn't this machine's, never dialing it.
func dialsOut(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var out []string
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = netproxy.Func
	dial := (&net.Dialer{Timeout: 5 * time.Second}).DialContext
	base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if host, _, _ := net.SplitHostPort(addr); host != "127.0.0.1" && host != "::1" && host != "localhost" {
			mu.Lock()
			out = append(out, addr)
			mu.Unlock()
			return nil, &net.OpError{Op: "dial", Net: network, Err: net.UnknownNetworkError("test: no network")}
		}
		return dial(ctx, network, addr)
	}
	oldT, oldC := http.DefaultTransport, http.DefaultClient.Transport
	http.DefaultTransport, http.DefaultClient.Transport = base, netproxy.Dispatch(base)
	t.Cleanup(func() {
		http.DefaultTransport, http.DefaultClient.Transport = oldT, oldC
		base.CloseIdleConnections()
	})
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), out...)
	}
}

func setQuotaReads(t *testing.T, v string) {
	t.Helper()
	s := settings.Load()
	s.QuotaReads = v
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
}

// #1518 (RooobinYe): with Settings → Usage → Allowance reads at "When I
// ask", nothing uni-switch does by itself asks a vendor for an allowance —
// the switch to an account with room (CodexUsedUp), routing
// (Allowances), the Usage page's and tray's timed reads, the menu bar
// (Quotas) — and each shows the reading kept from the last time, as of
// then. The user asking reads, and so does an account the vendor turned
// away for its quota (StaleAllowance).
func TestAllowancesReadOnlyWhenAsked(t *testing.T) {
	signIn(t) // Codex on me@example.com, and a Copilot account
	rememberLogins(true)
	var asked atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/wham/usage" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		asked.Add(1)
		w.Write([]byte(`{"plan_type":"pro","rate_limit":{"allowed":true,"limit_reached":false,
			"primary_window":{"used_percent":37,"limit_window_seconds":18000,"reset_after_seconds":3600}}}`))
	}))
	defer fake.Close()
	old := CodexBase
	CodexBase = fake.URL + "/backend-api/codex"
	t.Cleanup(func() { CodexBase = old })
	restart := func() {
		loginUsageCache.Lock()
		loginUsageCache.m, loginUsageCache.pending = nil, nil
		loginUsageCache.Unlock()
		subscriptionUsageCache.Lock()
		subscriptionUsageCache.data, subscriptionUsageCache.at, subscriptionUsageCache.asked = nil, time.Time{}, false
		subscriptionUsageCache.Unlock()
		planQuotaCache.Lock()
		planQuotaCache.data, planQuotaCache.at = nil, time.Time{}
		planQuotaCache.Unlock()
		keyBalanceCache.Lock()
		keyBalanceCache.data, keyBalanceCache.at = nil, time.Time{}
		keyBalanceCache.Unlock()
		usedCache.Lock()
		usedCache.m, usedCache.at, usedCache.loading = map[string]map[string]Allowance{}, map[string]time.Time{}, map[string]chan struct{}{}
		usedCache.stale, usedCache.renewed, usedCache.seen = nil, nil, nil
		usedCache.Unlock()
		staleReads.Lock()
		staleReads.m = nil
		staleReads.Unlock()
		ForgetKeptCardsForTest()
	}
	restart()
	t.Cleanup(restart)
	bg := context.Background()

	// read once, as uni-switch does by itself, before the setting
	if CodexUsedUp(bg) {
		t.Fatal("used up at 37%")
	}
	if n := asked.Load(); n != 1 {
		t.Fatalf("ChatGPT asked %d times, want 1", n)
	}

	// then allowances read only when asked, and uni-switch started again (at
	// login, before the proxy app)
	setQuotaReads(t, "asked")
	t.Cleanup(func() { setQuotaReads(t, "") })
	restart()
	out := dialsOut(t)
	CodexUsedUp(bg)
	q := LoginUsage(bg, "codex")["me@example.com"]
	if len(q.Windows) == 0 || q.Windows[0].Used != 37 || q.AsOf == nil {
		t.Fatalf("not asked, the account shows %+v, want the kept 37%% as of then", q)
	}
	if !AwaitAllowance(bg, "codex", "me@example.com") {
		t.Fatal("routing has no allowance for the account, not even the kept one")
	}
	cards := Quotas(bg) // the Usage page's timer, the tray, the menu bar
	if c := cardOf(cards, "codex", "me@example.com"); c == nil || len(c.Windows) == 0 || c.Windows[0].Used != 37 || c.AsOf == nil {
		t.Fatalf("not asked, the card is %+v, want the kept reading as of then", c)
	}
	Quotas(bg)
	if n := asked.Load(); n != 1 {
		t.Fatalf("nobody asked, and ChatGPT was asked %d more times", n-1)
	}
	if d := out(); len(d) > 0 {
		t.Fatalf("nobody asked, and uni-switch dialed %v", d)
	}

	// the vendor turns the account away for its quota: read, asked or not
	StaleAllowance("codex", "me@example.com")
	AwaitAllowance(bg, "codex", "me@example.com")
	if n := asked.Load(); n != 2 {
		t.Fatalf("an account the vendor turned away was read %d times, want once", n-1)
	}
	LoginUsage(bg, "codex")
	if n := asked.Load(); n != 2 {
		t.Fatal("read again after the one read a refusal asks for")
	}

	// the user asks: read
	AskUsage()
	q = LoginUsage(Asked(bg), "codex")["me@example.com"]
	if n := asked.Load(); n != 3 || q.AsOf != nil {
		t.Fatalf("asked, ChatGPT was asked %d times in all (want 3), the account %+v", n, q)
	}
}

// Automatic, as before: uni-switch reads by itself.
func TestAllowancesReadByThemselvesByDefault(t *testing.T) {
	signIn(t)
	rememberLogins(true)
	var asked atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		w.Write([]byte(`{"plan_type":"pro","rate_limit":{"allowed":true,"primary_window":{"used_percent":5,"limit_window_seconds":18000,"reset_after_seconds":3600}}}`))
	}))
	defer fake.Close()
	old := CodexBase
	CodexBase = fake.URL + "/backend-api/codex"
	t.Cleanup(func() { CodexBase = old })
	forget := func() {
		loginUsageCache.Lock()
		loginUsageCache.m, loginUsageCache.pending = nil, nil
		loginUsageCache.Unlock()
		ForgetKeptCardsForTest()
	}
	forget()
	t.Cleanup(forget)
	LoginUsage(context.Background(), "codex")
	if asked.Load() != 1 {
		t.Fatal("uni-switch no longer reads an allowance by itself while set to")
	}
}

// The same for a key's windows and balance: routing (KeyAllowance) and
// the cards' timed reads (KeyBalances, PlanQuotas) ask nothing while
// allowances are read only when asked; a key that said it is out
// (StaleKeyAllowance) is read, and so is one the user asks for.
func TestKeyReadsOnlyWhenAsked(t *testing.T) {
	keyLimitsHome(t)
	srv, asked := sub2apiServer(t, func() ([]byte, int) { return sub2apiUsage(t, true), 200 })
	p := sub2apiKey(t, srv)
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	setQuotaReads(t, "asked")
	t.Cleanup(func() { setQuotaReads(t, "") })
	dialsOut(t) // uni-switch's own transport, which holds what nobody asked for
	bg := context.Background()
	KeyBalances(bg)
	PlanQuotas(bg)
	KeyAllowance(p)
	time.Sleep(100 * time.Millisecond)
	if n := asked.Load(); n != 0 {
		t.Fatalf("nobody asked, and the key was read %d times", n)
	}
	StaleKeyAllowance(p)
	KeyAllowance(p)
	for i := 0; i < 100 && asked.Load() == 0; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if n := asked.Load(); n != 1 {
		t.Fatalf("a key that said it is out was read %d times, want once", n)
	}
	ForgetBalances()
	KeyBalances(Asked(bg))
	if n := asked.Load(); n < 2 {
		t.Fatal("the user asked, and the key's balance wasn't read")
	}
}
