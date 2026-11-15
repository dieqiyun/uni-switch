package gateway

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/routes"
)

// drained answers each key, or refuses it with its status and body.
type drained struct {
	sync.Mutex
	tried []string
	fail  map[string][2]string // key → status, body
}

func (d *drained) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.ReadAll(r.Body)
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	d.Lock()
	d.tried = append(d.tried, key)
	f, failing := d.fail[key]
	d.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if failing {
		code := map[string]int{"429": 429, "402": 402}[f[0]]
		w.WriteHeader(code)
		io.WriteString(w, f[1])
		return
	}
	io.WriteString(w, `{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"from `+key+`"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`)
}

func (d *drained) set(key, status, body string) {
	d.Lock()
	defer d.Unlock()
	if status == "" {
		delete(d.fail, key)
		return
	}
	d.fail[key] = [2]string{status, body}
}

func (d *drained) last() string {
	d.Lock()
	defer d.Unlock()
	return d.tried[len(d.tried)-1]
}

// wake ends every rest, as each one's time running out would.
func wake() {
	restingUntil.Lock()
	restingUntil.m = map[string]time.Time{}
	restingUntil.note = map[string]Rest{}
	restingUntil.Unlock()
}

const outOfQuota = `{"error":{"message":"You exceeded your current quota, please check your plan","type":"insufficient_quota"}}`

// dzxx0496 on Discord: an account's free quota used up, the next takes
// over, and when the first renews requests go back to it. Routed Until used
// up, they stay on the one that took over; In order still goes back to the
// first, as it always has.
func TestDrainKeepsTheOneThatTookOver(t *testing.T) {
	for _, c := range []struct {
		routing, want string
	}{
		{provider.Drain, "k2"},
		{provider.Ordered, "k1"},
	} {
		t.Run(c.routing, func(t *testing.T) {
			fresh(t)
			up := &drained{fail: map[string][2]string{}}
			serveOn(t, "dr", "k1", []string{"m1"}, up, "k2", "k3")
			if err := provider.SetRouting("dr", c.routing); err != nil {
				t.Fatal(err)
			}
			if err := provider.SetAffinity("dr", provider.AffinityOff); err != nil {
				t.Fatal(err)
			}
			routes.Forget("dr")
			s := New()
			ask := func(n int) string {
				t.Helper()
				code, out := postAs(t, s, "", `{"model":"dr/m1","messages":[{"role":"user","content":"ask `+strings.Repeat("x", n)+`"}]}`)
				if code != 200 {
					t.Fatalf("%d %s", code, out)
				}
				return up.last()
			}
			if k := ask(1); k != "k1" {
				t.Fatalf("first request went to %s, want k1", k)
			}
			// k1's quota used up: k2 takes over
			up.set("k1", "429", outOfQuota)
			if k := ask(2); k != "k2" {
				t.Fatalf("k1 out of quota: went to %s, want k2 (tried %v)", k, up.tried)
			}
			// k1 renews
			up.set("k1", "", "")
			wake()
			for i := 3; i < 6; i++ {
				if k := ask(i); k != c.want {
					t.Fatalf("after k1 renewed, request %d went to %s, want %s (tried %v)", i, k, c.want, up.tried)
				}
			}
		})
	}
}

// A rate limit is not the quota used up: the next answers while it rests,
// and the requests go back to the one it was on after.
func TestDrainComesBackAfterARateLimit(t *testing.T) {
	fresh(t)
	up := &drained{fail: map[string][2]string{}}
	serveOn(t, "dl", "k1", []string{"m1"}, up, "k2")
	if err := provider.SetRouting("dl", provider.Drain); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetAffinity("dl", provider.AffinityOff); err != nil {
		t.Fatal(err)
	}
	routes.Forget("dl")
	s := New()
	ask := func(n int) string {
		t.Helper()
		code, out := postAs(t, s, "", `{"model":"dl/m1","messages":[{"role":"user","content":"ask `+strings.Repeat("y", n)+`"}]}`)
		if code != 200 {
			t.Fatalf("%d %s", code, out)
		}
		return up.last()
	}
	up.set("k1", "429", `{"error":{"message":"slow down"}}`)
	if k := ask(1); k != "k2" {
		t.Fatalf("k1 rate limited: went to %s, want k2", k)
	}
	up.set("k1", "", "")
	wake()
	if k := ask(2); k != "k1" {
		t.Fatalf("k1's rate limit over: went to %s, want k1 again (tried %v)", k, up.tried)
	}
}
