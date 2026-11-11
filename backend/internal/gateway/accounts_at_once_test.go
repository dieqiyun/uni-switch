package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// 01huadalang on Discord: ZCode bans the address three of its accounts are
// used from at once, while two are let be. A provider's accounts at once
// (provider.AtOnce) keeps the first that many in the user's own order,
// whatever routing then weighs them by, and holds the rest back as
// reserves, after them; one resting gives its seat to the next.
func TestAtOnceKeepsFirstInUserOrder(t *testing.T) {
	fresh(t)
	two := 2
	for _, mode := range []string{"", provider.Ordered, provider.Rotate, provider.LeastUsed} {
		p := provider.Provider{ID: "once" + mode, Name: "Once", Chat: "https://example.invalid/v1", Key: "first", Keys: []provider.KeyAccount{{Key: "second"}, {Key: "third"}}, Routing: mode, MaxAccounts: &two}
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
		if err := provider.SetAccountOrder(p.ID, []string{provider.KeyID("third"), provider.KeyID("first"), provider.KeyID("second")}); err != nil {
			t.Fatal(err)
		}
		got, err := provider.Find(p.ID)
		if err != nil {
			t.Fatal(err)
		}
		split := func() (kept []string, reserve string) {
			t.Helper()
			cs, pl := (&Server{}).plan(*got, "model", provider.Chat)
			if len(cs) != 3 || len(pl.order) != 3 {
				t.Fatalf("mode=%q: %d candidates, order %+v", mode, len(cs), pl.order)
			}
			for i, w := range pl.order {
				id := w.ID[len(p.ID)+1:]
				switch {
				case w.Reserve == 0 && cs[i].reserve == 0:
					if reserve != "" {
						t.Fatalf("mode=%q: %s kept after the reserve %s", mode, id, reserve)
					}
					kept = append(kept, id)
				case w.Reserve == 2 && cs[i].reserve == 2 && w.Aside:
					reserve = id
				default:
					t.Fatalf("mode=%q: %s reserve %d/%d aside %v", mode, id, w.Reserve, cs[i].reserve, w.Aside)
				}
			}
			slices.Sort(kept)
			return kept, reserve
		}
		want := []string{provider.KeyID("first"), provider.KeyID("third")}
		slices.Sort(want)
		if kept, reserve := split(); !slices.Equal(kept, want) || reserve != provider.KeyID("second") {
			t.Fatalf("mode=%q: kept %v, reserve %s; want the first two as dragged, third and first", mode, kept, reserve)
		}
		// the first in the list resting: the next takes its seat
		restingUntil.Lock()
		k := p.ID + "#" + provider.KeyID("third")
		restingUntil.m[k] = time.Now().Add(time.Hour)
		restingUntil.note[k] = Rest{Why: failOther, Status: 500, Key: k}
		restingUntil.Unlock()
		want = []string{provider.KeyID("first"), provider.KeyID("second")}
		slices.Sort(want)
		if kept, reserve := split(); !slices.Equal(kept, want) || reserve != provider.KeyID("third") {
			t.Fatalf("mode=%q, third resting: kept %v, reserve %s", mode, kept, reserve)
		}
		restingUntil.Lock()
		delete(restingUntil.m, k)
		restingUntil.Unlock()
	}
	// with no limit, every one is routed over
	p := provider.Provider{ID: "every", Name: "Every", Chat: "https://example.invalid/v1", Key: "first", Keys: []provider.KeyAccount{{Key: "second"}, {Key: "third"}}}
	if _, pl := (&Server{}).plan(p, "model", provider.Chat); len(pl.order) != 3 || slices.ContainsFunc(pl.order, func(w Weighed) bool { return w.Reserve > 0 || w.Aside }) {
		t.Fatalf("no limit: %+v", pl.order)
	}
}

// atOnceKeys records which key each request came with, and lets a slowVendor
// answer it.
type atOnceKeys struct {
	mu   sync.Mutex
	keys []string
	next http.Handler
}

func (b *atOnceKeys) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	b.keys = append(b.keys, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	b.mu.Unlock()
	b.next.ServeHTTP(w, r)
}

func (b *atOnceKeys) seen() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.keys)
}

// A full lane never hands a request to a reserve: with one request at once
// on each key and two keys at once, a third request waits for a slot of
// the two rather than going to the third key (laneMate gave way to it, so
// In order still spread over every account under load), and once both
// queues are full too the next is turned away, not sent to the third.
func TestAtOnceFullLaneWaitsForTheKept(t *testing.T) {
	fresh(t)
	v := &slowVendor{release: map[string]chan struct{}{}, quit: make(chan struct{})}
	rec := &atOnceKeys{next: v}
	up := httptest.NewServer(rec)
	t.Cleanup(up.Close)
	t.Cleanup(func() { close(v.quit) })
	one, two := 1, 2
	if err := provider.Save(provider.Provider{ID: "zc", Name: "ZC", Key: "k1", Keys: []provider.KeyAccount{{Key: "k2"}, {Key: "k3"}}, Models: []string{"m"}, Chat: up.URL + "/v1",
		Routing: provider.Ordered, MaxConcurrency: &one, MaxAccounts: &two, QueueLimit: 1}); err != nil {
		t.Fatal(err)
	}
	g := newLaneRig(t)
	waiting := func() int {
		n := 0
		for _, l := range g.s.Lanes() {
			n += l.Waiting
		}
		return n
	}
	g.send(context.Background(), "/v1/chat/completions", "zc/m", "a")
	within(t, "a at the vendor", func() bool { return len(v.seen()) == 1 })
	g.send(context.Background(), "/v1/chat/completions", "zc/m", "b")
	within(t, "b at the vendor", func() bool { return len(v.seen()) == 2 })
	g.send(context.Background(), "/v1/chat/completions", "zc/m", "c")
	within(t, "c waiting for a slot", func() bool { return waiting() == 1 })
	if got := rec.seen(); slices.Contains(got, "k3") {
		t.Fatalf("keys asked %v: the reserve k3 was sent a request for want of a slot", got)
	}
	// k1's queue full, d waits in k2's
	g.send(context.Background(), "/v1/chat/completions", "zc/m", "d")
	within(t, "d waiting for a slot", func() bool { return waiting() == 2 })
	// both full: e is turned away, never sent to the reserve
	g.send(context.Background(), "/v1/chat/completions", "zc/m", "e")
	if r := g.reply(); r.tag != "e" || r.status != http.StatusTooManyRequests {
		t.Fatalf("e = %+v, want turned away with a 429", r)
	}
	if got := rec.seen(); slices.Contains(got, "k3") {
		t.Fatalf("keys asked %v: the reserve k3 was sent a request turned away by a full queue", got)
	}
	for _, tag := range []string{"a", "b", "c", "d"} {
		close(v.gate(tag))
	}
	for range 4 {
		if r := g.reply(); r.err != nil || !strings.Contains(r.body, "[DONE]") {
			t.Fatalf("reply %+v", r)
		}
	}
	if v.most != 2 {
		t.Fatalf("most out at once = %d, want 2", v.most)
	}
	if got := rec.seen(); slices.Contains(got, "k3") || len(got) != 4 {
		t.Fatalf("keys asked %v, want k1 and k2 only", got)
	}
	within(t, "every slot given back", func() bool { return len(g.s.Lanes()) == 0 })
}

// atOnceVendor answers each request by its key: 429 for those in fail,
// else a reply naming the key.
type atOnceVendor struct {
	mu    sync.Mutex
	fail  map[string]bool
	asked []string
}

func (v *atOnceVendor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body)
	k := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	v.mu.Lock()
	v.asked = append(v.asked, k)
	fail := v.fail[k]
	v.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if fail {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"message":"rate limited","type":"rate_limit"}}`)
		return
	}
	fmt.Fprintf(w, `{"id":"x","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"from %s"},"finish_reason":"stop"}]}`, k)
}

// A reserve takes the request once every key kept has failed, and keeps
// no conversation: once the kept are back, the conversation's next turn
// goes back to the first of them, even under Stays: Session, which keeps
// a conversation on whoever answered it.
func TestAtOnceReserveTakesOverAndGivesBack(t *testing.T) {
	fresh(t)
	v := &atOnceVendor{fail: map[string]bool{"k1": true, "k2": true}}
	up := httptest.NewServer(v)
	t.Cleanup(up.Close)
	two := 2
	if err := provider.Save(provider.Provider{ID: "zc", Name: "ZC", Key: "k1", Keys: []provider.KeyAccount{{Key: "k2"}, {Key: "k3"}}, Models: []string{"m"}, Chat: up.URL + "/v1",
		Routing: provider.Ordered, Affinity: provider.AffinitySession, MaxAccounts: &two}); err != nil {
		t.Fatal(err)
	}
	s := New()
	msgs := `{"role":"user","content":"hi"}`
	code, body := postAs(t, s, "s1", `{"model":"zc/m","messages":[`+msgs+`]}`)
	if code != 200 || !strings.Contains(body, "from k3") {
		t.Fatalf("with k1 and k2 refusing: %d %s", code, body)
	}
	// the kept two are back, their rests over
	v.mu.Lock()
	v.fail = nil
	v.mu.Unlock()
	restingUntil.Lock()
	restingUntil.m = map[string]time.Time{}
	restingUntil.Unlock()
	msgs += `,{"role":"assistant","content":"from k3"},{"role":"user","content":"more"}`
	code, body = postAs(t, s, "s1", `{"model":"zc/m","messages":[`+msgs+`]}`)
	if code != 200 || !strings.Contains(body, "from k1") {
		t.Fatalf("next turn, the kept back: %d %s; want k1, not kept on the reserve k3", code, body)
	}
	if r := s.trace.routes[len(s.trace.routes)-1]; r.Affinity == nil || r.Affinity.Why != "reserve" {
		t.Fatalf("affinity %+v, want reserve", r.Affinity)
	}
}
