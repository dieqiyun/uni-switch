package provider

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeMiniMaxCheckin is MiniMax Code's check-in as one account sees it,
// behind the plugin's fetch, answering as Hu9956 captured it (#811): the
// week's panel, today day 2, and a claim (claim_result 1, 2 once in).
type fakeMiniMaxCheckin struct {
	mu      sync.Mutex
	today   int  // today's status on the panel
	code    int  // base_resp.status_code of every answer
	expired bool // the plugin marks the sign-in expired
	asked   []string
	bad     []string // what was sent wrong
}

func (f *fakeMiniMaxCheckin) panel() string {
	days := make([]string, 7)
	for i := range days {
		st, today := 1, i == 1
		if i == 0 {
			st = 3
		}
		if today {
			st = f.today
		}
		days[i] = fmt.Sprintf(`{"day_no":%d,"points":800,"status":%d,"is_today":%t,"bonus_points":400}`, i+1, st, today)
	}
	return `{"scene":2,"days":[` + strings.Join(days, ",") + `]}`
}

func (f *fakeMiniMaxCheckin) serve(t *testing.T) { f.serveSite(t, MiniMaxCodeID) }

// serveSite serves the check-in of the plugin's provider site.
func (f *fakeMiniMaxCheckin) serveSite(t *testing.T, site string) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		b, _ := io.ReadAll(r.Body)
		f.asked = append(f.asked, r.Method+" "+strings.TrimPrefix(r.URL.Path, "/minimax-cloud/api/v1/signin"))
		if r.URL.Query().Get("timezone_id") != "Asia/Shanghai" {
			f.bad = append(f.bad, "timezone_id "+r.URL.RawQuery)
		}
		sum := md5.Sum([]byte(r.Header.Get("x-timestamp") + miniMaxSignSecret + string(b)))
		if r.Header.Get("x-timestamp") == "" || r.Header.Get("x-signature") != hex.EncodeToString(sum[:]) {
			f.bad = append(f.bad, "signature")
		}
		if f.expired {
			w.Header().Set("X-Uni-Switch-Sign-In", "expired")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		base := fmt.Sprintf(`{"status_code":%d,"status_msg":"%s"}`, f.code, map[bool]string{true: "ok", false: "invalid timezone_id"}[f.code == 0])
		switch r.Method + " " + r.URL.Path {
		case "GET /minimax-cloud/api/v1/signin/status":
			if len(b) != 0 {
				f.bad = append(f.bad, "a GET with a body")
			}
			fmt.Fprintf(w, `{"data":%s,"base_resp":%s}`, f.panel(), base)
		case "POST /minimax-cloud/api/v1/signin/claim":
			if string(b) != "{}" {
				f.bad = append(f.bad, "claim body "+string(b))
			}
			result := 2
			if f.today == mmDayClaimable {
				result, f.today = 1, mmDayClaimed
			}
			fmt.Fprintf(w, `{"base_resp":%s,"data":{"claim_id":"2106776476924810147","claim_result":%d,"day_no":2,"points":800,"expire_at_ms":1793721600000,"panel":%s}}`, base, result, f.panel())
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	old := miniMaxCheckinURLs[site]
	miniMaxCheckinURLs[site] = srv.URL + "/minimax-cloud/api/v1/signin"
	t.Cleanup(func() { miniMaxCheckinURLs[site] = old })
}

func (f *fakeMiniMaxCheckin) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.asked
	f.asked = nil
	return out
}

// MiniMax Code's daily check-in (#811): the panel is read, today's claimed
// while it is claimable, once a Beijing day, signed as MiniMax Code's page
// signs it; one in already isn't claimed again; a refusal or an expired
// sign-in is a failure with its reason, tried again.
func TestMiniMaxCheckin(t *testing.T) {
	f := &fakeMiniMaxCheckin{today: mmDayClaimable}
	f.serve(t)
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	c := newMiniMaxCheckiner(func() []miniMaxAccount {
		return []miniMaxAccount{{User: "hu", On: true, uid: "42", card: MiniMaxCodeID, via: http.DefaultClient.Do}}
	})
	c.path = filepath.Join(t.TempDir(), "minimax-checkin.json")
	c.now = func() time.Time { return now }

	rs := c.checkinNow(t.Context(), false)
	if len(rs) != 1 || rs[0].Outcome != CheckinClaimed || rs[0].Credit != 800 || rs[0].Streak != 2 || rs[0].By != "minimax" || !rs[0].Asked {
		t.Fatalf("claimable: %+v", rs)
	}
	if got := strings.Join(f.take(), ", "); got != "GET /status, POST /claim" {
		t.Fatalf("asked %s", got)
	}
	// the day's answer holds: nothing asked again
	if rs := c.checkinNow(t.Context(), false); rs[0].Outcome != CheckinClaimed || rs[0].Asked || len(f.take()) != 0 {
		t.Fatalf("asked again the same day: %+v", rs)
	}

	// the next day, in already (pressed in MiniMax Code): no claim
	now = now.Add(24 * time.Hour)
	f.today = mmDayClaimed
	if rs := c.checkinNow(t.Context(), false); rs[0].Outcome != CheckinDone || rs[0].Credit != 800 {
		t.Fatalf("in already: %+v", rs)
	}
	if got := strings.Join(f.take(), ", "); got != "GET /status" {
		t.Fatalf("in already, asked %s", got)
	}

	// a day closed to the account
	now = now.Add(24 * time.Hour)
	f.today = mmDayDisabled
	if rs := c.checkinNow(t.Context(), false); rs[0].Outcome != CheckinIneligible {
		t.Fatalf("disabled: %+v", rs)
	}

	// MiniMax's refusal, with its reason
	now = now.Add(24 * time.Hour)
	f.today, f.code = mmDayClaimable, 1406010011
	if rs := c.checkinNow(t.Context(), false); rs[0].Outcome != CheckinFailed || !strings.Contains(rs[0].Msg, "invalid timezone_id") {
		t.Fatalf("refused: %+v", rs)
	}

	// an expired sign-in says so
	now = now.Add(24 * time.Hour)
	f.code, f.expired = 0, true
	if rs := c.checkinNow(t.Context(), false); rs[0].Outcome != CheckinFailed || !strings.Contains(rs[0].Msg, "sign in again") {
		t.Fatalf("expired: %+v", rs)
	}
	if len(f.bad) != 0 {
		t.Fatalf("sent wrong: %v", f.bad)
	}
}

// A claim that says today's is in already (claim_result 2) is a check-in
// done, not a failure: MiniMax's claim is idempotent.
func TestMiniMaxClaimAlready(t *testing.T) {
	f := &fakeMiniMaxCheckin{today: mmDayClaimable}
	f.serve(t)
	a := miniMaxAccount{User: "hu", On: true, uid: "42", via: func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			f.mu.Lock()
			f.today = mmDayClaimed // pressed in MiniMax Code in between
			f.mu.Unlock()
		}
		return http.DefaultClient.Do(r)
	}}
	if r := miniMaxCheckin(t.Context(), a); r.Outcome != CheckinDone || r.Credit != 800 || r.Streak != 2 {
		t.Fatalf("already: %+v", r)
	}
}

// MiniMax Code's international site has the same check-in (ARNO on
// Discord): an account signed in there is checked in at its own site, not
// the China one, and kept apart from a China account of the same uid.
func TestMiniMaxGlobalCheckin(t *testing.T) {
	if !strings.HasPrefix(miniMaxCheckinURLs[MiniMaxCodeGlobalID], "https://agent.minimax.io/") {
		t.Fatalf("the international site's check-in is %q", miniMaxCheckinURLs[MiniMaxCodeGlobalID])
	}
	cn := &fakeMiniMaxCheckin{today: mmDayClaimed}
	cn.serve(t)
	io := &fakeMiniMaxCheckin{today: mmDayClaimable}
	io.serveSite(t, MiniMaxCodeGlobalID)
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	c := newMiniMaxCheckiner(func() []miniMaxAccount {
		return []miniMaxAccount{
			{User: "arno", On: true, site: MiniMaxCodeGlobalID, uid: "42", card: MiniMaxCodeGlobalID, via: http.DefaultClient.Do},
			{User: "hu", On: true, site: MiniMaxCodeID, uid: "42", card: MiniMaxCodeID, via: http.DefaultClient.Do},
		}
	})
	c.path = filepath.Join(t.TempDir(), "minimax-checkin.json")
	c.now = func() time.Time { return now }

	rs := c.checkinNow(t.Context(), false)
	got := map[string]string{}
	for _, r := range rs {
		got[r.User] = r.Outcome
	}
	if len(rs) != 2 || got["arno"] != CheckinClaimed || got["hu"] != CheckinDone {
		t.Fatalf("checked in: %+v", rs)
	}
	if a := strings.Join(io.take(), ", "); a != "GET /status, POST /claim" {
		t.Fatalf("the international site was asked %s", a)
	}
	if a := strings.Join(cn.take(), ", "); a != "GET /status" {
		t.Fatalf("the China site was asked %s", a)
	}
	if len(io.bad)+len(cn.bad) != 0 {
		t.Fatalf("sent wrong: %v %v", io.bad, cn.bad)
	}
	k := miniMaxCheckinKey(miniMaxAccount{site: MiniMaxCodeGlobalID, uid: "42"})
	if k == miniMaxCheckinKey(miniMaxAccount{uid: "42"}) || !strings.HasPrefix(k, MiniMaxCodeGlobalID+"|") {
		t.Fatalf("key %q", k)
	}
}
