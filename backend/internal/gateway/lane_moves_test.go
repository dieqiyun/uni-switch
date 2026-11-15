package gateway

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// 01huadalang on Discord (11-01): three ZCode accounts at three requests
// at once each, the first two resting after a 405. Every request went to
// the third, and those over its three waited in its queue; once the first
// two were back, the waiting ones waited on behind the third's long turns,
// under Smart, In turn and with Stays off alike, until its requests were
// cut by hand. A request waiting in a full lane goes to the first other
// key or account with room that isn't resting (waitMate) as soon as there
// is one: one back from its rest, lifted by hand (Try now) or run out.
// While they rest, the full lane gives way to neither (laneMate): its lane
// is free only because it rests. With two accounts at once (ZCode's
// plugin's maxAccounts), the third, which took a seat while the first two
// rested, is a reserve again once they are back, and those waiting for it
// go to the two.
func TestWaitingRequestGoesToAccountBackFromRest(t *testing.T) {
	for _, mode := range []string{"", provider.Rotate} {
		for _, aff := range []string{"", provider.AffinityOff} {
			for _, lift := range []string{"lifted", "ran out"} {
				t.Run(fmt.Sprintf("routing=%q/stays=%q/%s", mode, aff, lift), func(t *testing.T) {
					waitingGoesBack(t, mode, aff, lift == "lifted")
				})
			}
		}
	}
}

func waitingGoesBack(t *testing.T, mode, aff string, lift bool) {
	fresh(t)
	v, _ := slowUp(t)
	rec := &atOnceKeys{next: v}
	up := httptest.NewServer(rec)
	t.Cleanup(up.Close)
	three, two := 3, 2
	if err := provider.Save(provider.Provider{ID: "zc", Name: "ZC", Key: "k1", Keys: []provider.KeyAccount{{Key: "k2"}, {Key: "k3"}}, Models: []string{"m"}, Chat: up.URL + "/v1",
		Routing: mode, MaxConcurrency: &three, MaxAccounts: &two}); err != nil {
		t.Fatal(err)
	}
	if aff != "" {
		if err := provider.SetAffinity("zc", aff); err != nil {
			t.Fatal(err)
		}
	}
	key := func(k string) string { return "zc#" + provider.KeyID(k) }
	// the first two rest after ZCode's 405
	restFor := func(d time.Duration) {
		restingUntil.Lock()
		for _, k := range []string{"k1", "k2"} {
			restingUntil.m[key(k)] = time.Now().Add(d)
			restingUntil.note[key(k)] = Rest{Why: failOther, Status: 405, Key: key(k)}
		}
		restingUntil.Unlock()
	}
	restFor(time.Hour)

	g := newLaneRig(t)
	count := func(k string) int {
		n := 0
		for _, got := range rec.seen() {
			if got == k {
				n++
			}
		}
		return n
	}
	waiting := func() int {
		n := 0
		for _, l := range g.s.Lanes() {
			n += l.Waiting
		}
		return n
	}
	tags := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}
	for _, tag := range tags[:3] {
		g.send(context.Background(), "/v1/chat/completions", "zc/m", tag)
	}
	within(t, "three out at k3", func() bool { return count("k3") == 3 })
	for _, tag := range tags[3:] {
		g.send(context.Background(), "/v1/chat/completions", "zc/m", tag)
	}
	within(t, "six waiting for k3", func() bool { return g.s.Lanes()[key("k3")].Waiting == 6 })
	if n1, n2 := count("k1"), count("k2"); n1+n2 > 0 {
		t.Fatalf("k1 asked %d, k2 %d while resting: a full lane gave way to one resting", n1, n2)
	}
	if lift {
		g.s.Unrest(key("k1"))
		g.s.Unrest(key("k2"))
	} else {
		// the rests now end in a moment, as if set so: the waiting ones
		// look once more (as when they queued) and find them resting, so
		// what moves them is the end itself, well before laneRecheck
		restFor(300 * time.Millisecond)
		g.s.lanes.signal()
	}
	// back: the six waiting go to the two, three each, at once, while
	// the third's three are still out
	within(t, "the waiting ones at k1 and k2", func() bool { return count("k1") == 3 && count("k2") == 3 && waiting() == 0 })
	if n := count("k3"); n != 3 {
		t.Fatalf("k3 asked %d, want its three only", n)
	}
	// rec counts a request before it reaches v: all nine at v at once
	within(t, "nine at the vendor", func() bool { return len(v.seen()) == 9 })
	for _, tag := range tags {
		close(v.gate(tag))
	}
	for range tags {
		if r := g.reply(); r.err != nil || !strings.Contains(r.body, "[DONE]") {
			t.Fatalf("reply %+v", r)
		}
	}
	if v.most != 9 {
		t.Fatalf("most out at once = %d, want 9: three on each", v.most)
	}
	within(t, "every slot given back", func() bool { return len(g.s.Lanes()) == 0 })
}

// A request waiting for a key whose slots are all out goes to another key
// of its provider once one of that one's is given back, under every
// routing, rather than waiting for the one it first queued for. The lane
// it goes to has no queue of its own: who waits there goes first.
func TestWaitingRequestTakesSlotGivenBackElsewhere(t *testing.T) {
	for _, mode := range []string{"", provider.Ordered, provider.Rotate, provider.LeastUsed} {
		t.Run(fmt.Sprintf("routing=%q", mode), func(t *testing.T) {
			fresh(t)
			v, _ := slowUp(t)
			rec := &atOnceKeys{next: v}
			up := httptest.NewServer(rec)
			t.Cleanup(up.Close)
			one := 1
			if err := provider.Save(provider.Provider{ID: "two", Name: "Two", Key: "k1", Keys: []provider.KeyAccount{{Key: "k2"}}, Models: []string{"m"}, Chat: up.URL + "/v1",
				Routing: mode, MaxConcurrency: &one}); err != nil {
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
			g.send(context.Background(), "/v1/chat/completions", "two/m", "a")
			within(t, "a out", func() bool { return len(v.seen()) == 1 })
			g.send(context.Background(), "/v1/chat/completions", "two/m", "b")
			within(t, "b out", func() bool { return len(v.seen()) == 2 })
			g.send(context.Background(), "/v1/chat/completions", "two/m", "c")
			within(t, "c waiting", func() bool { return waiting() == 1 })
			// whichever key c waits for, the other one's request ends
			// first: c goes there
			keys := rec.seen()
			waitsFor := ""
			for who, l := range g.s.Lanes() {
				if l.Waiting == 1 {
					waitsFor = who
				}
			}
			end, other := "a", keys[0] // c waits for b's key: a's ends
			if waitsFor == "two#"+provider.KeyID(keys[0]) {
				end, other = "b", keys[1] // c waits for a's key: b's ends
			}
			close(v.gate(end))
			if r := g.reply(); r.tag != end {
				t.Fatalf("reply %+v, want %s", r, end)
			}
			within(t, "c sent to "+other, func() bool { return len(v.seen()) == 3 })
			if got := rec.seen(); got[2] != other {
				t.Fatalf("keys asked %v: c went to %s, want %s, the one whose slot was given back", got, got[2], other)
			}
			if n := waiting(); n != 0 {
				t.Fatalf("%d still waiting", n)
			}
			for _, tag := range []string{"a", "b", "c"} {
				if tag != end {
					close(v.gate(tag))
				}
			}
			for range 2 {
				if r := g.reply(); r.err != nil || !strings.Contains(r.body, "[DONE]") {
					t.Fatalf("reply %+v", r)
				}
			}
			within(t, "every slot given back", func() bool { return len(g.s.Lanes()) == 0 })
		})
	}
}
