package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/agentenv"
	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// @NetRozee on X: a custom endpoint given a thousand keys froze uni-switch.
// What a page or a request asks of a provider's keys is counted here, not
// timed: each is asked once for all of them, never once a key.

func manyKeys(n int) Provider {
	p := Provider{ID: "relay", Name: "Relay", Chat: "https://relay.example/v1", Key: "sk-first"}
	for i := range n {
		p.Keys = append(p.Keys, KeyAccount{Key: fmt.Sprintf("sk-key-%04d", i)})
	}
	return p
}

// RestKeys is RestKey, found once: the id alone while one key is on, the
// id and the key's while several are, the first key again or keys off
// not counting.
func TestRestKeysIsRestKey(t *testing.T) {
	for _, p := range []Provider{
		{ID: "a", Key: "k"},
		{ID: "a", Key: "k", Keys: []KeyAccount{{Key: "k"}, {Key: "j", Off: true}, {Key: ""}}},
		{ID: "a", Key: "k", Keys: []KeyAccount{{Key: "k"}, {Key: "j"}}},
		manyKeys(3),
	} {
		rest := p.RestKeys()
		for _, k := range p.KeyList() {
			if got, want := rest(k), p.ID+map[bool]string{true: "#" + k.ID}[len(p.KeysOn()) > 1]; got != want {
				t.Errorf("%+v: key %s rests by %q, want %q", p.Keys, k.ID, got, want)
			}
		}
	}
}

// KeysOn still drops a key listed twice, and the first key listed again,
// keeping the order (#1772).
func TestKeysOnDropsRepeats(t *testing.T) {
	p := Provider{ID: "a", Key: "k", Keys: []KeyAccount{{Key: "j"}, {Key: "k"}, {Key: "i", Off: true}, {Key: "j"}, {Key: "h"}, {Key: ""}}}
	var got []string
	for _, k := range p.KeysOn() {
		got = append(got, k.Key)
	}
	if fmt.Sprint(got) != "[k j h]" {
		t.Errorf("keys on %v, want [k j h]", got)
	}
}

// Whether each of a thousand keys serves a model reads the provider's
// lists once, and answers as Serves did: a key no list names serves
// everything, a listed one what its lists hold.
func TestServingReadsListsOnce(t *testing.T) {
	isolateFetchHome(t)
	p := manyKeys(1000)
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	var odd, all []string
	for i, k := range p.KeysOn() {
		if i < 900 { // the last hundred are on no list yet
			all = append(all, keyID(k.Key))
			if i%2 == 1 {
				odd = append(odd, keyID(k.Key))
			}
		}
	}
	if err := catalog.SaveLive(p.ID, p.Chat, []catalog.Model{{ID: "both", Keys: all}, {ID: "half", Keys: odd}}); err != nil {
		t.Fatal(err)
	}
	q, err := Find(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	before := ListsRead()
	serves := q.Serving("half")
	if got := ListsRead() - before; got != 1 {
		t.Errorf("Serving read the lists %d times, want once", got)
	}
	n := 0
	for i, k := range q.KeysOn() {
		want := i >= 900 || i%2 == 1
		if serves(k) != want {
			t.Fatalf("key %d serves half: %v, want %v", i, !want, want)
		}
		if q.Serves(k, "half") != want {
			t.Fatalf("Serves key %d half: %v, want %v", i, !want, want)
		}
		n++
	}
	if n != 1001 {
		t.Fatalf("%d keys on", n)
	}
}

// heldAtOnce is how many requests the server was asked at once at most
// while call ran: each is held until a ninth comes, or a second passes
// with eight held.
func heldAtOnce(t *testing.T, answer func(w http.ResponseWriter, r *http.Request), call func()) int {
	t.Helper()
	var mu sync.Mutex
	in, most := 0, 0
	gate := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		in++
		most = max(most, in)
		mu.Unlock()
		<-gate
		answer(w, r)
		mu.Lock()
		in--
		mu.Unlock()
	}))
	defer srv.Close()
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = rewrite{srv}
	defer func() { http.DefaultClient.Transport = old }()
	done := make(chan struct{})
	go func() { defer close(done); call() }()
	var eight time.Time
	for open := true; open; {
		mu.Lock()
		m := most
		mu.Unlock()
		switch {
		case m > keyReadsAtOnce:
			open = false
		case m == keyReadsAtOnce && eight.IsZero():
			eight = time.Now()
		case m == keyReadsAtOnce && time.Since(eight) > time.Second:
			open = false
		}
		select {
		case <-done:
			open = false
		case <-time.After(5 * time.Millisecond):
		}
	}
	close(gate)
	<-done
	t.Logf("%d asked at once at most", most)
	return most
}

// A provider's keys have their balances, plans and remote limits read
// eight at a time: every key at once was a thousand requests to one
// relay together.
func TestKeyReadsEightAtOnce(t *testing.T) {
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
	forget := func() {
		lastQuotas.Lock()
		lastQuotas.m, lastQuotas.loaded = nil, false
		lastQuotas.Unlock()
		ForgetBalances()
		forgetPlanQuotas()
	}
	forget()
	t.Cleanup(forget)
	p := Provider{ID: "commandcode", Name: "commandCode", Preset: "commandcode", Key: "cmd-first",
		Chat: "https://api.commandcode.ai/provider/v1", Anthropic: "https://api.commandcode.ai/provider"}
	for i := range 30 {
		p.Keys = append(p.Keys, KeyAccount{Key: fmt.Sprintf("cmd-%02d", i)})
	}
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	credits := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alpha/billing/credits" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"credits":{"monthlyCredits":1,"purchasedCredits":0,"freeCredits":0}}`))
	}
	var cards int
	most := heldAtOnce(t, credits, func() { cards = len(KeyBalances(context.Background())) })
	if most > keyReadsAtOnce || cards != 31 {
		t.Errorf("balances: %d cards, %d asked at once, want 31 cards and at most %d", cards, most, keyReadsAtOnce)
	}
	most = heldAtOnce(t, credits, func() { cards = len(PlanQuotas(context.Background())) })
	if most > keyReadsAtOnce {
		t.Errorf("plans: %d asked at once, want at most %d", most, keyReadsAtOnce)
	}
}

// One read given up on goes on without a slot; another provider's keys
// have slots of their own.
func TestKeySlots(t *testing.T) {
	var s keySlots
	var free []func()
	for range keyReadsAtOnce {
		free = append(free, s.take(context.Background(), "a"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.take(ctx, "a")() // returns at once, with nothing to free
	if n := len(s.m["a"]); n != keyReadsAtOnce {
		t.Fatalf("%d of a's slots taken, want %d", n, keyReadsAtOnce)
	}
	b := s.take(context.Background(), "b") // never waits on a's
	b()
	free[0]()
	s.take(context.Background(), "a")()
	if n := len(s.m["a"]); n != keyReadsAtOnce-1 {
		t.Fatalf("%d of a's slots taken, want %d", n, keyReadsAtOnce-1)
	}
}
