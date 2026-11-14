package gateway

import (
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A plain 403 (or 401) that names no quota is about the key or the
// request, not the window: an account whose cached usage read 98% rests
// on the backoff ladder, not until its window resets. Before, it sat out
// the whole week on the reading alone (plugins#243, zygs1083-dotcom: one
// Factory 403 benched a working account into its reset). A 403 that does
// say the quota is spent still rests until the window, and so does any
// other failure at 98%, as before.
func TestPlain403DoesNotRestUntilTheWindow(t *testing.T) {
	forgetRouting()
	old := allowances
	defer func() { allowances = old }()
	now := time.Now()
	week := now.Add(39 * time.Hour)
	allowances = func(string) map[string]provider.Allowance {
		return map[string]provider.Allowance{
			"me@x.com": {{Used: 98, Resets: week, Span: 7 * 24 * time.Hour}},
		}
	}
	acct := candidate{p: provider.Provider{ID: "factory", Account: &provider.Account{Agent: "factory", User: "me@x.com"}}, model: "claude-opus-4-7", rest: "factory#me@x.com"}
	if !acct.full(now).Equal(week) {
		t.Fatal("the account at 98% doesn't read as full; the test proves nothing")
	}
	s := &Server{}
	near := func(d, want time.Duration) bool { return d > want-time.Minute && d <= want }

	for _, status := range []int{http.StatusForbidden, http.StatusUnauthorized} {
		forgetRouting()
		r := s.restAfter(acct, status, http.Header{}, []byte(`{"error":{"message":"Forbidden"}}`))
		if r.By == "window" || r.By != "backoff" || time.Until(r.Until) > longestRetry {
			t.Fatalf("a plain %d at 98%% rests %v by %s (%s)", status, time.Until(r.Until).Round(time.Minute), r.By, r.Why)
		}
	}

	// a 403 that says the quota is spent is out until the window
	forgetRouting()
	r := s.restAfter(acct, http.StatusForbidden, http.Header{}, []byte(`{"error":{"message":"You've hit your usage limit"}}`))
	if r.Why != failQuota || r.By != "window" || !near(time.Until(r.Until), 39*time.Hour) {
		t.Fatalf("a 403 saying used up rests %v by %s (%s)", time.Until(r.Until).Round(time.Minute), r.By, r.Why)
	}
	// and another failure at 98% still trusts the reading, as before
	forgetRouting()
	r = s.restAfter(acct, http.StatusBadGateway, http.Header{}, []byte("bad gateway"))
	if r.By != "window" || !near(time.Until(r.Until), 39*time.Hour) {
		t.Fatalf("a 502 at 98%% rests %v by %s", time.Until(r.Until).Round(time.Minute), r.By)
	}
}

// One request refused with a plain 403 by two accounts of a provider is
// the request refused, not each account (plugins#243, zygs1083-dotcom:
// Factory's 403 for a Claude Code wrapper it refused walked every account
// into backoff, so the next requests found them all resting). Neither
// rests, and an account after them answers. One account's plain 403
// alone still rests it, as a key refused does.
func TestPlain403OnTwoMatesRestsNeither(t *testing.T) {
	forbid := func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"error":{"message":"Forbidden"}}`)
	}
	for _, x := range []struct {
		name    string
		spares  []string
		also    []string // accounts after acct-1 that refuse too
		resting []string // who rests after
		code    int
	}{
		{"alone", []string{"b@example.com"}, nil, []string{"codex@me@example.com"}, 200},
		{"two alike, a third answers", []string{"b@example.com", "c@example.com"}, []string{"acct-2"}, nil, 200},
		{"two alike, nobody left", []string{"b@example.com"}, []string{"acct-2"}, nil, 403},
	} {
		t.Run(x.name, func(t *testing.T) {
			codexSignedIn(t, x.spares...)
			if err := provider.SetRouting("codex", provider.Ordered); err != nil {
				t.Fatal(err)
			}
			var tried []string
			chatgptRefusing(t, &tried, forbid, x.also...)
			s := New()
			code, body := codexAskOn(s, `{"model":"gpt-5.5","stream":true,"input":"ping"}`)
			if code != x.code {
				t.Fatalf("status %d: %s; tried %v", code, body, tried)
			}
			r := lastRoute(s)
			for _, who := range append([]string{"codex@me@example.com"}, func() (w []string) {
				for _, s := range x.spares {
					w = append(w, "codex@"+s)
				}
				return
			}()...) {
				_, ok := restOf(who)
				if want := slices.Contains(x.resting, who); ok != want {
					t.Fatalf("%s resting %v, want %v; tried %v; tries %+v", who, ok, want, tried, r.Tries)
				}
				if !slices.Contains(x.resting, who) {
					routed.Lock()
					n := routed.failures[who]
					routed.Unlock()
					if n != 0 {
						t.Fatalf("%s still counts %d failures", who, n)
					}
				}
			}
			if want := map[int]string{200: "acct-1,acct-2", 403: "acct-1,acct-2"}[code]; x.also != nil && !strings.HasPrefix(strings.Join(tried, ","), want) {
				t.Fatalf("tried %v", tried)
			}
			if x.also != nil && (r.Tries[0].Rest != nil || r.Tries[1].Rest != nil) {
				t.Fatalf("tries still say they rest: %+v", r.Tries)
			}
		})
	}
}
