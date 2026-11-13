package gateway

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// plugins#349: a pool of capped accounts that ends at its caps (CapStop)
// and reads usage again near them (CapCareful).

// poolRead stands in for when each account's usage was last read, and
// whether that read failed, by user; readUsageNow is stood in for by
// again, which is told the users asked and may change the readings. A
// user with no entry was never read.
type poolRead struct {
	mu     sync.Mutex
	at     map[string]time.Time
	failed map[string]bool
	asked  []string
	again  func(user string)
}

func standInReads(t *testing.T) *poolRead {
	t.Helper()
	r := &poolRead{at: map[string]time.Time{}, failed: map[string]bool{}}
	oldRead, oldNow := usageRead, readUsageNow
	t.Cleanup(func() { usageRead, readUsageNow = oldRead, oldNow })
	usageRead = func(_, user string) (time.Time, bool, bool) {
		r.mu.Lock()
		defer r.mu.Unlock()
		at, ok := r.at[user]
		return at, r.failed[user], ok
	}
	readUsageNow = func(_ context.Context, _ string, users ...string) bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.asked = append(r.asked, users...)
		for _, u := range users {
			if r.again != nil {
				r.again(u)
			}
		}
		return true
	}
	carefulTried.Lock()
	carefulTried.at = nil
	carefulTried.Unlock()
	return r
}

func (r *poolRead) set(user string, at time.Time, failed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.at[user], r.failed[user] = at, failed
}

func (r *poolRead) askedFor() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.asked)
}

// carefulPool: Codex's two accounts, me first in the order with a 90% cap
// and spare with none, their five hours used as given.
func carefulPool(t *testing.T, me, spare float64, careful bool) *resetBackend {
	t.Helper()
	codexSignedIn(t, "spare@example.com")
	b := newResetBackend(t)
	capUsage(t, map[string]float64{"me@example.com": me, "spare@example.com": spare})
	if err := provider.SetRouting("codex", provider.Ordered); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetAccountCap("codex", "me@example.com", 90); err != nil {
		t.Fatal(err)
	}
	if careful {
		if err := provider.SetCapCareful("codex", true); err != nil {
			t.Fatal(err)
		}
	}
	return b
}

// Off, the default: an account under its cap takes the request on its
// last reading, however old or failed that read, and nothing is read
// again before it.
func TestCarefulOffByDefault(t *testing.T) {
	b := carefulPool(t, 88, 10, false)
	r := standInReads(t)
	r.set("me@example.com", time.Now().Add(-time.Hour), true)
	if p, _ := provider.Find("codex"); p.CapCareful || p.CapStop {
		t.Fatalf("on by default: careful %v stop %v", p.CapCareful, p.CapStop)
	}
	code, body := resetPost(t, New())
	if tried, _ := b.seen(); code != 200 || !strings.Contains(body, "pong") || tried != "acct-1" {
		t.Fatalf("%d %s tried %s", code, body, tried)
	}
	if asked := r.askedFor(); len(asked) != 0 {
		t.Fatalf("read again with careful off: %v", asked)
	}
}

// On, an account whose usage read failed, was never read, or was read
// over five minutes ago is held, as one at its cap is, and the request
// goes to the next; a fresh good reading lets it through.
func TestCarefulHoldsWhatItCantTell(t *testing.T) {
	for _, c := range []struct {
		name string
		read func(*poolRead)
		want string
	}{
		{"failed", func(r *poolRead) { r.set("me@example.com", time.Now(), true) }, "acct-2"},
		{"never read", func(*poolRead) {}, "acct-2"},
		{"stale", func(r *poolRead) { r.set("me@example.com", time.Now().Add(-10*time.Minute), false) }, "acct-2"},
		{"fresh", func(r *poolRead) { r.set("me@example.com", time.Now(), false) }, "acct-1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := carefulPool(t, 50, 10, true)
			r := standInReads(t)
			c.read(r)
			srv := New()
			code, body := resetPost(t, srv)
			if tried, _ := b.seen(); code != 200 || !strings.Contains(body, "pong") || tried != c.want {
				t.Fatalf("%d %s tried %s, want %s", code, body, tried, c.want)
			}
			if c.want == "acct-1" {
				return
			}
			// read again first, the reading still not told: held
			if asked := r.askedFor(); !slices.Equal(asked, []string{"me@example.com"}) {
				t.Fatalf("read again for %v", asked)
			}
			var unread []string
			for _, rt := range srv.Trace(t.Context(), 0, 0).Routes {
				for _, w := range rt.Left {
					if w.Unread != "" {
						unread = append(unread, w.Who+": "+w.Unread)
					}
				}
			}
			if len(unread) != 1 || !strings.HasPrefix(unread[0], "me@example.com: its usage") {
				t.Fatalf("traced as unread: %v", unread)
			}
		})
	}
}

// On, an account within five points of its cap on a reading over 15
// seconds old is read again before the request: a reading back under the
// cap sends it there, one at the cap moves on. Far from its cap, a
// minute-old reading is used as it is.
func TestCarefulReadsAgainNearTheCap(t *testing.T) {
	t.Run("far from the cap", func(t *testing.T) {
		b := carefulPool(t, 50, 10, true)
		r := standInReads(t)
		r.set("me@example.com", time.Now().Add(-time.Minute), false)
		if code, _ := resetPost(t, New()); code != 200 {
			t.Fatal(code)
		}
		if tried, _ := b.seen(); tried != "acct-1" || len(r.askedFor()) != 0 {
			t.Fatalf("tried %s, read again for %v", tried, r.askedFor())
		}
	})
	t.Run("read again, room left", func(t *testing.T) {
		b := carefulPool(t, 87, 10, true)
		r := standInReads(t)
		r.set("me@example.com", time.Now().Add(-time.Minute), false)
		r.again = func(u string) { r.at[u] = time.Now() }
		if code, _ := resetPost(t, New()); code != 200 {
			t.Fatal(code)
		}
		if tried, _ := b.seen(); tried != "acct-1" || !slices.Equal(r.askedFor(), []string{"me@example.com"}) {
			t.Fatalf("tried %s, read again for %v", tried, r.askedFor())
		}
	})
	t.Run("read again, at the cap", func(t *testing.T) {
		b := carefulPool(t, 87, 10, true)
		r := standInReads(t)
		r.set("me@example.com", time.Now().Add(-time.Minute), false)
		r.again = func(u string) {
			r.at[u] = time.Now()
			capUsage(t, map[string]float64{"me@example.com": 91, "spare@example.com": 10})
		}
		if code, _ := resetPost(t, New()); code != 200 {
			t.Fatal(code)
		}
		if tried, _ := b.seen(); tried != "acct-2" || !slices.Equal(r.askedFor(), []string{"me@example.com"}) {
			t.Fatalf("tried %s, read again for %v", tried, r.askedFor())
		}
	})
	t.Run("the read fails", func(t *testing.T) {
		b := carefulPool(t, 87, 10, true)
		r := standInReads(t)
		r.set("me@example.com", time.Now().Add(-time.Minute), false)
		r.again = func(u string) { r.failed[u] = true }
		if code, _ := resetPost(t, New()); code != 200 {
			t.Fatal(code)
		}
		if tried, _ := b.seen(); tried != "acct-2" {
			t.Fatalf("tried %s", tried)
		}
	})
}

// On, with every account capped and none of their usage readable, the
// client is answered 429 and nothing reaches the vendor; the message says
// why and how to turn it off.
func TestCarefulEveryAccountUnread(t *testing.T) {
	b := carefulPool(t, 50, 10, true)
	if err := provider.SetAccountCap("codex", "spare@example.com", 90); err != nil {
		t.Fatal(err)
	}
	r := standInReads(t)
	r.set("me@example.com", time.Now(), true)
	r.set("spare@example.com", time.Now(), true)
	srv := New()
	code, body := resetPost(t, srv)
	if tried, _ := b.seen(); code != 429 || tried != "" {
		t.Fatalf("%d tried %q: %s", code, tried, body)
	}
	for _, want := range []string{"rate_limit_error", "couldn't be read", "--careful off"} {
		if !strings.Contains(body, want) {
			t.Errorf("no %q in %s", want, body)
		}
	}
}

// Every account at its cap, with a fallback set: it takes the request,
// as before; with Stop at the caps on, the client is answered 429 and
// neither the vendor nor the fallback is sent anything.
func TestCapStopSkipsTheFallbacks(t *testing.T) {
	b := carefulPool(t, 95, 92, false)
	if err := provider.SetAccountCap("codex", "spare@example.com", 90); err != nil {
		t.Fatal(err)
	}
	fb := &keyed{}
	serveOn(t, "spare", "kf", []string{"m2"}, fb)
	p, _ := provider.Find("codex")
	p.Fallback = []string{"spare/m2"}
	if err := provider.Save(*p); err != nil {
		t.Fatal(err)
	}
	post := func() (int, string) {
		return postAs(t, New(), "", `{"model":"codex/gpt-5.5","messages":[{"role":"user","content":"hi"}]}`)
	}
	code, body := post()
	if tried, _ := b.seen(); code != 200 || !strings.Contains(body, "from kf") || tried != "" {
		t.Fatalf("stop off: %d %s, codex tried %q", code, body, tried)
	}
	if err := provider.SetCapStop("codex", true); err != nil {
		t.Fatal(err)
	}
	fb.tried = nil
	code, body = post()
	if tried, _ := b.seen(); code != 429 || tried != "" || len(fb.tried) != 0 {
		t.Fatalf("stop on: %d %s, codex tried %q, fallback %v", code, body, tried, fb.tried)
	}
	for _, want := range []string{"rate_limit_error", "usage cap reached", "past its 90% cap"} {
		if !strings.Contains(body, want) {
			t.Errorf("no %q in %s", want, body)
		}
	}
	// an account with room still takes it (the first request's stick to
	// the fallback let go, as it would be once the session moves on)
	capUsage(t, map[string]float64{"me@example.com": 50, "spare@example.com": 92})
	sticks.Lock()
	sticks.m = map[string]stick{}
	sticks.Unlock()
	code, body = post()
	if tried, _ := b.seen(); code != 200 || tried != "acct-1" || len(fb.tried) != 0 {
		t.Fatalf("room left: %d %s, codex tried %q, fallback %v", code, body, tried, fb.tried)
	}
}
