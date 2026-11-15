package gateway

import (
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/routes"
)

// Weighted routing (#841): a provider's keys weighing 3 and 1 take three
// requests to one, held over every few requests rather than only on
// average, and a key resting is left out of the round, the others taking
// its share by their weights.
func TestWeightedKeys(t *testing.T) {
	p := provider.Provider{ID: "wk", Chat: "https://wk.example/v1", Key: "ka", KeyName: "team-a", KeyWeight: 3,
		Keys:    []provider.KeyAccount{{Name: "team-b", Key: "kb"}, {Name: "team-c", Key: "kc", Weight: 4}, {Name: "spare", Key: "kd", Off: true, Weight: 9}},
		Routing: provider.Weighted}
	a, b, c := "wk#"+provider.KeyID("ka"), "wk#"+provider.KeyID("kb"), "wk#"+provider.KeyID("kc")
	t.Cleanup(func() {
		routes.Forget(p.ID)
		restingUntil.Lock()
		delete(restingUntil.m, c)
		restingUntil.Unlock()
	})
	pick := func() string {
		cs := route(p, perKey(p, "m", provider.Chat), "m", provider.Chat)
		if len(cs) != 3 {
			t.Fatalf("%d candidates: %s", len(cs), restsOf(cs))
		}
		return cs[0].rest
	}
	// 3 : 1 : 4 over every round of 8, smoothly: never the same key more
	// than twice running when another weighs as much
	got := map[string]int{}
	var seq []string
	for range 8 * 50 {
		k := pick()
		got[k]++
		seq = append(seq, k)
		if len(seq)%8 == 0 {
			round := map[string]int{}
			for _, s := range seq[len(seq)-8:] {
				round[s]++
			}
			if round[a] != 3 || round[b] != 1 || round[c] != 4 {
				t.Fatalf("round %d: a %d b %d c %d, want 3 1 4", len(seq)/8, round[a], round[b], round[c])
			}
		}
	}
	if got[a] != 150 || got[b] != 50 || got[c] != 200 {
		t.Fatalf("over 400: a %d b %d c %d", got[a], got[b], got[c])
	}
	if run := strings.Count(strings.Join(seq[:8], " "), c+" "+c+" "+c); run > 0 {
		t.Fatalf("not smooth: %v", seq[:8])
	}

	// c resting: a and b share its requests 3 : 1, and c is tried last
	restingUntil.Lock()
	restingUntil.m[c] = time.Now().Add(time.Hour)
	restingUntil.Unlock()
	got = map[string]int{}
	for range 4 * 25 {
		cs := route(p, perKey(p, "m", provider.Chat), "m", provider.Chat)
		cs, _ = restLast(cs, planned{order: make([]Weighed, len(cs))})
		if cs[len(cs)-1].rest != c {
			t.Fatalf("the resting key isn't last: %s", restsOf(cs))
		}
		got[cs[0].rest]++
	}
	if got[a] != 75 || got[b] != 25 || got[c] != 0 {
		t.Fatalf("with c resting: a %d b %d c %d", got[a], got[b], got[c])
	}

	// a conversation kept on b counts against b: the next picks make it up
	restingUntil.Lock()
	delete(restingUntil.m, c)
	restingUntil.Unlock()
	routes.Forget(p.ID)
	got = map[string]int{}
	for i := range 8 * 10 {
		cs := route(p, perKey(p, "m", provider.Chat), "m", provider.Chat)
		k := cs[0]
		if i%8 == 0 { // every round, one request stays with b
			for _, x := range cs {
				if x.rest == b {
					weightedKept(k, x)
					k = x
				}
			}
		}
		got[k.rest]++
	}
	if got[a] != 30 || got[b] != 10 || got[c] != 40 {
		t.Fatalf("with b kept: a %d b %d c %d", got[a], got[b], got[c])
	}
}

// A key's weight is saved with it, the first key's too, and 1 is the
// default left out of the file.
func TestSetKeyWeight(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", home+"/.config")
	if err := provider.Save(provider.Provider{ID: "wk", Name: "WK", Chat: "https://wk.example/v1", Key: "ka", Keys: []provider.KeyAccount{{Key: "kb"}}, Routing: provider.Weighted}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetKeyWeight("wk", provider.KeyID("ka"), 3); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetKeyWeight("wk", provider.KeyID("kb"), 5); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetKeyWeight("wk", provider.KeyID("kb"), -1); err == nil {
		t.Fatal("a weight below 0 was taken")
	}
	p, err := provider.Find("wk")
	if err != nil {
		t.Fatal(err)
	}
	if p.Routing != provider.Weighted {
		t.Fatalf("routing %q", p.Routing)
	}
	ks := p.KeyList()
	if ks[0].Weight != 3 || ks[1].Weight != 5 {
		t.Fatalf("weights %d %d", ks[0].Weight, ks[1].Weight)
	}
	// the weight goes with its key when another goes first
	if err := provider.UseKey("wk", provider.KeyID("kb")); err != nil {
		t.Fatal(err)
	}
	p, _ = provider.Find("wk")
	if p.KeyWeight != 5 || p.Keys[0].Weight != 3 {
		t.Fatalf("after Make first: first %d, other %d", p.KeyWeight, p.Keys[0].Weight)
	}
	if err := provider.SetKeyWeight("wk", provider.KeyID("kb"), 1); err != nil {
		t.Fatal(err)
	}
	p, _ = provider.Find("wk")
	if p.KeyWeight != 0 {
		t.Fatalf("1 kept as %d", p.KeyWeight)
	}
}
