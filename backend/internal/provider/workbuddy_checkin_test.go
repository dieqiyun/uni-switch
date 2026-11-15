package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeWBCheckin is WorkBuddy's check-in API as one account sees it: the
// event's status, and a claim answered with claimCode (0 claims).
type fakeWBCheckin struct {
	mu        sync.Mutex
	active    bool
	checked   bool
	claimCode int
	down      bool // the network: every request fails
	broken    bool // every request is answered with a 500
	// refuse401 is the paths WorkBuddy's gateway answers with its bare
	// 401 page; "*" is every one (a token it refuses)
	refuse401 []string
	summaries int
	statuses  int
	claims    int
}

func (f *fakeWBCheckin) serve(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.down {
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			c.Close()
			return
		}
		if f.broken {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if slices.Contains(f.refuse401, "*") || slices.Contains(f.refuse401, r.URL.Path) {
			switch r.URL.Path {
			case "/v2/billing/meter/checkin-activity-status":
				f.statuses++
			case "/billing/meter/get-user-resource-summary":
				f.summaries++
			}
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, wbGateway401)
			return
		}
		if r.Header.Get("Authorization") != "Bearer wb-access" || r.Header.Get("X-User-Id") != "u1" ||
			!strings.HasPrefix(r.Header.Get("User-Agent"), "WorkBuddy/") {
			json.NewEncoder(w).Encode(map[string]any{"code": 10085, "msg": "请求不合法"})
			return
		}
		ok := func(data any) { json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "ok", "data": data}) }
		switch r.URL.Path {
		case "/v2/billing/meter/checkin-activity-status":
			f.statuses++
			ok(map[string]any{"active": f.active, "today_checked_in": f.checked, "streak_days": 3,
				"daily_credit": 100, "today_credit": map[bool]int{true: 100}[f.checked], "total_credits": 300})
		case "/billing/meter/get-user-resource-summary":
			f.summaries++
			ok(map[string]any{"Packages": []any{map[string]any{"CycleTotalCapacity": "1500", "CycleUsedCapacity": "0"}}})
		case "/v2/billing/meter/daily-checkin":
			f.claims++
			if f.claimCode != 0 {
				json.NewEncoder(w).Encode(map[string]any{"code": f.claimCode, "msg": "今日已签到"})
				return
			}
			f.checked = true
			ok(map[string]any{"credit": 100, "streak_days": 4, "is_streak_day": true})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// wbGateway401 is what copilot.tencent.com's gateway (APISIX) answers a
// token it refuses with, as captured on 2026-10-10.
const wbGateway401 = `<html>
<head><title>401 Authorization Required</title></head>
<body>
<center><h1>401 Authorization Required</h1></center>
<hr><center>openresty</center>
<p><em>Powered by <a href="https://apisix.apache.org/">APISIX</a>.</em></p></body>
</html>
`

func (f *fakeWBCheckin) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statuses, f.claims
}

// wbPluginSigned is the plugin's fetch for the account fakeWBCheckin
// knows: it signs each request with the account's sign-in.
func wbPluginSigned(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer wb-access")
	req.Header.Set("X-User-Id", "u1")
	return http.DefaultClient.Do(req)
}

// wbCheckinFixture is a checkiner for one WorkBuddy plugin account at srv,
// its clock at *now, its file in a temp folder.
func wbCheckinFixture(t *testing.T, srv *httptest.Server, now *time.Time) wbCheckiner {
	t.Helper()
	endpoint := srv.URL
	site := &wbSite{id: "workbuddy", name: "WorkBuddy", endpoint: &endpoint}
	a := wbAccount{Login: Login{User: "旅行者", On: true, Active: true}, site: site,
		creds: wbCreds{UID: "u1"}, via: wbPluginSigned}
	return wbCheckiner{
		path:     filepath.Join(t.TempDir(), "workbuddy-checkin.json"),
		now:      func() time.Time { return *now },
		accounts: func() []wbAccount { return []wbAccount{a} },
	}
}

// An account whose check-in is open is checked in once a Beijing day: the
// claim is kept, so a second look that day asks nothing, and the next day
// it is claimed again.
func TestWorkBuddyCheckinOncePerDay(t *testing.T) {
	f := &fakeWBCheckin{active: true}
	srv := f.serve(t)
	// 23:50 on 1 March in Beijing is 15:50 UTC
	now := time.Date(2026, 3, 1, 15, 50, 0, 0, time.UTC)
	c := wbCheckinFixture(t, srv, &now)
	ctx := context.Background()

	rs := c.checkinNow(ctx, false)
	if len(rs) != 1 || rs[0].Outcome != CheckinClaimed || !rs[0].Checked() || rs[0].Credit != 100 || rs[0].Streak != 4 ||
		rs[0].Day != "2026-03-01" || rs[0].User != "旅行者" || !rs[0].Asked {
		t.Fatalf("first: %+v", rs)
	}
	if s, cl := f.counts(); s != 1 || cl != 1 {
		t.Fatalf("asked status %d times, claimed %d", s, cl)
	}
	// kept by site and id, with no name and no token
	b, err := os.ReadFile(c.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"workbuddy|u1"`) || strings.Contains(string(b), "旅行者") || strings.Contains(string(b), "wb-access") {
		t.Fatalf("kept: %s", b)
	}

	// later that day: nothing asked, the claim read back
	now = now.Add(5 * time.Minute)
	rs = c.checkinNow(ctx, false)
	if len(rs) != 1 || rs[0].Outcome != CheckinClaimed || rs[0].Asked {
		t.Fatalf("again: %+v", rs)
	}
	if s, cl := f.counts(); s != 1 || cl != 1 {
		t.Fatalf("asked again the same day: status %d, claims %d", s, cl)
	}

	// past midnight in Beijing (16:05 UTC): a new day, claimed again
	now = now.Add(10 * time.Minute)
	f.mu.Lock()
	f.checked = false
	f.mu.Unlock()
	rs = c.checkinNow(ctx, false)
	if len(rs) != 1 || rs[0].Outcome != CheckinClaimed || rs[0].Day != "2026-03-02" || !rs[0].Asked {
		t.Fatalf("next day: %+v", rs)
	}
	if s, cl := f.counts(); s != 2 || cl != 2 {
		t.Fatalf("next day: status %d, claims %d", s, cl)
	}
}

// Checked in already (in WorkBuddy), or the event not running: nothing is
// claimed, and, answered from noon Beijing on (checkinSureHour), that holds
// for the day; an earlier one is asked again (#3326).
func TestWorkBuddyCheckinNothingToClaim(t *testing.T) {
	for _, tc := range []struct {
		name    string
		f       *fakeWBCheckin
		outcome string
	}{
		{"checked in", &fakeWBCheckin{active: true, checked: true}, CheckinDone},
		{"not active", &fakeWBCheckin{active: false}, CheckinInactive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := tc.f.serve(t)
			now := time.Date(2026, 3, 1, 4, 0, 0, 0, time.UTC) // 12:00 Beijing
			c := wbCheckinFixture(t, srv, &now)
			rs := c.checkinNow(context.Background(), false)
			if len(rs) != 1 || rs[0].Outcome != tc.outcome {
				t.Fatalf("%+v", rs)
			}
			if tc.outcome == CheckinDone && (!rs[0].Checked() || rs[0].Credit != 100 || rs[0].Streak != 3) {
				t.Fatalf("already in: %+v", rs[0])
			}
			now = now.Add(time.Hour)
			c.checkinNow(context.Background(), false)
			if s, cl := tc.f.counts(); s != 1 || cl != 0 {
				t.Fatalf("status %d, claims %d", s, cl)
			}
		})
	}
}

// A claim refused as already made (1001) or not eligible (1002) from noon
// Beijing on is that day's answer, not asked again by the loop.
func TestWorkBuddyCheckinRefused(t *testing.T) {
	for code, outcome := range map[int]string{wbCheckinAlready: CheckinDone, wbCheckinIneligible: CheckinIneligible, wbCheckinEnded: CheckinInactive} {
		f := &fakeWBCheckin{active: true, claimCode: code}
		srv := f.serve(t)
		now := time.Date(2026, 3, 1, 4, 0, 0, 0, time.UTC) // 12:00 Beijing
		c := wbCheckinFixture(t, srv, &now)
		rs := c.checkinNow(context.Background(), false)
		if len(rs) != 1 || rs[0].Outcome != outcome {
			t.Fatalf("%d: %+v", code, rs)
		}
		now = now.Add(2 * time.Hour)
		c.checkinNow(context.Background(), false)
		if s, cl := f.counts(); s != 1 || cl != 1 {
			t.Fatalf("%d asked again: status %d, claims %d", code, s, cl)
		}
	}
}

// An answer that isn't one of the event's (a 500, a refused token) is
// tried again later that day, not before wbCheckinRetry.
func TestWorkBuddyCheckinRetriesAfterFailure(t *testing.T) {
	f := &fakeWBCheckin{active: true, broken: true}
	srv := f.serve(t)
	now := time.Date(2026, 3, 1, 2, 0, 0, 0, time.UTC)
	c := wbCheckinFixture(t, srv, &now)
	if rs := c.checkinNow(context.Background(), false); len(rs) != 1 || rs[0].Outcome != CheckinFailed || rs[0].Checked() || rs[0].Offline {
		t.Fatalf("broken: %+v", rs)
	}
	f.mu.Lock()
	f.broken = false
	f.mu.Unlock()
	now = now.Add(10 * time.Minute)
	if rs := c.checkinNow(context.Background(), false); rs[0].Outcome != CheckinFailed || rs[0].Asked {
		t.Fatalf("tried again too soon: %+v", rs)
	}
	now = now.Add(wbCheckinRetry)
	if rs := c.checkinNow(context.Background(), false); rs[0].Outcome != CheckinClaimed || !rs[0].Asked {
		t.Fatalf("not tried again: %+v", rs)
	}
	if _, cl := f.counts(); cl != 1 {
		t.Fatalf("claims %d", cl)
	}
}

// A check-in that never reached WorkBuddy — the machine just woke and its
// network isn't back, "no such host" (#265) — is tried again a minute
// later, not half an hour, by the loop as well; while it still doesn't,
// the wait doubles up to wbCheckinRetry.
func TestWorkBuddyCheckinOfflineRetriesSoon(t *testing.T) {
	f := &fakeWBCheckin{active: true, down: true}
	srv := f.serve(t)
	now := time.Date(2026, 3, 1, 0, 37, 52, 0, time.UTC)
	c := wbCheckinFixture(t, srv, &now)
	ctx := context.Background()
	var l wbCheckinLoop
	// the loop's first look, the moment the machine wakes
	if rs := l.tick(ctx, c); len(rs) != 1 || rs[0].Outcome != CheckinFailed || !rs[0].Offline || !rs[0].Asked {
		t.Fatalf("woken: %+v", rs)
	}
	// a minute on, the loop's minute tick, the network back
	f.mu.Lock()
	f.down = false
	f.mu.Unlock()
	now = now.Add(time.Minute)
	if rs := l.tick(ctx, c); len(rs) != 1 || rs[0].Outcome != CheckinClaimed || !rs[0].Asked {
		t.Fatalf("a minute on: %+v", rs)
	}
	if _, cl := f.counts(); cl != 1 {
		t.Fatalf("claims %d", cl)
	}

	// down all along: tried at 0, 1, 3, 7, 15, 31, then every 30 minutes
	f = &fakeWBCheckin{active: true, down: true}
	srv = f.serve(t)
	now = time.Date(2026, 3, 1, 2, 0, 0, 0, time.UTC)
	c = wbCheckinFixture(t, srv, &now)
	l = wbCheckinLoop{}
	var tried []int
	for m := 0; m <= 91; m++ {
		for _, r := range l.tick(ctx, c) {
			if r.Asked {
				tried = append(tried, m)
			}
		}
		now = now.Add(time.Minute)
	}
	if want := []int{0, 1, 3, 7, 15, 31, 61, 91}; !slices.Equal(tried, want) {
		t.Fatalf("tried at %v, want %v", tried, want)
	}
}

// A moved account's check-in goes through the plugin's fetch, whose failure
// comes back as Bun's text, not a net.Error. The connection reset that
// plugins #149 showed is a check-in that never got an answer: tried again a
// minute later, not half an hour. An answer that failed still waits.
func TestWorkBuddyCheckinPluginResetRetriesSoon(t *testing.T) {
	f := &fakeWBCheckin{active: true}
	srv := f.serve(t)
	now := time.Date(2026, 3, 1, 2, 0, 0, 0, time.UTC)
	c := wbCheckinFixture(t, srv, &now)
	reset := true
	accts := c.accounts()
	accts[0].via = func(req *http.Request) (*http.Response, error) {
		if reset {
			// the reporter's words, as the plugin host passes them on
			return nil, errors.New("ECONNRESET: The socket connection was closed unexpectedly. For more information, pass `verbose: true` in the second argument to fetch()")
		}
		return wbPluginSigned(req)
	}
	c.accounts = func() []wbAccount { return accts }
	ctx := context.Background()
	var l wbCheckinLoop
	if rs := l.tick(ctx, c); len(rs) != 1 || rs[0].Outcome != CheckinFailed || !rs[0].Offline || !rs[0].Asked {
		t.Fatalf("reset: %+v", rs)
	}
	reset = false
	now = now.Add(time.Minute)
	if rs := l.tick(ctx, c); len(rs) != 1 || rs[0].Outcome != CheckinClaimed || !rs[0].Asked {
		t.Fatalf("a minute on: %+v", rs)
	}

	for _, msg := range []string{"plugin fetch: HTTP 500", "WorkBuddy refused: code 11140", "Unable to parse the reply"} {
		if r := wbCheckinFailed(errors.New(msg)); r.Offline {
			t.Errorf("%q read as never answered", msg)
		}
	}
}

// An account on the plugin is checked in through the plugin's own fetch,
// which signs it: uni-switch holds no token of it to renew or send.
func TestWorkBuddyCheckinThroughPlugin(t *testing.T) {
	var paths []string
	via := func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if req.Header.Get("Authorization") != "" {
			t.Errorf("uni-switch signed %s itself", req.URL.Path)
		}
		body := `{"code":0,"data":{"active":true,"today_checked_in":false,"streak_days":2}}`
		if strings.HasSuffix(req.URL.Path, "/daily-checkin") {
			body = `{"code":0,"data":{"credit":50,"streak_days":3}}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	}
	a := wbAccount{Login: Login{User: "me", On: true}, site: wbCN, creds: wbCreds{UID: "u1"}, via: via}
	r := wbCheckin(context.Background(), a)
	if r.Outcome != CheckinClaimed || r.Credit != 50 || r.Streak != 3 {
		t.Fatalf("check-in through the plugin: %+v", r)
	}
	if len(paths) != 2 || !strings.HasSuffix(paths[0], "/checkin-activity-status") || !strings.HasSuffix(paths[1], "/daily-checkin") {
		t.Fatalf("asked %v", paths)
	}
}

// The Usage page's WorkBuddy card says how each account's check-in went
// (#694): matched by its name, or the only account's on a card without
// one; another vendor's card, and an account not checked in, untouched,
// and the cards given (the usage cache) left as they were.
func TestWorkBuddyCheckinOnTheCard(t *testing.T) {
	accts := []wbAccount{
		{Login: Login{User: "Ann", On: true}, site: wbCN, creds: wbCreds{UID: "u1"}},
		{Login: Login{User: "Bob", On: true}, site: wbCN, creds: wbCreds{UID: "u2"}},
		{Login: Login{User: "Off", On: false}, site: wbCN, creds: wbCreds{UID: "u3"}},
	}
	st := map[string]WorkBuddyCheckin{
		"workbuddy|u1": {Day: "2026-10-03", Outcome: CheckinClaimed, Credit: 50, Streak: 3},
		"workbuddy|u3": {Day: "2026-10-03", Outcome: CheckinDone},
	}
	qs := []SubscriptionQuota{{Provider: "workbuddy", User: "ann"}, {Provider: "workbuddy", User: "Bob"}, {Provider: "workbuddy", User: "Off"}, {Provider: "codex", User: "Ann"}}
	got := withCheckins(qs, accts, st)
	if !got[0].Checkins || got[0].Checkin == nil || got[0].Checkin.Credit != 50 || got[0].Checkin.User != "Ann" {
		t.Fatalf("Ann: %+v", got[0])
	}
	if !got[1].Checkins || got[1].Checkin != nil {
		t.Fatalf("Bob, not checked in yet: %+v", got[1])
	}
	if got[2].Checkins || got[2].Checkin != nil || got[3].Checkins || got[3].Checkin != nil {
		t.Fatalf("an account off, or another vendor's: %+v %+v", got[2], got[3])
	}
	if qs[0].Checkins || qs[0].Checkin != nil {
		t.Fatal("the cards given were changed")
	}
	one := withCheckins([]SubscriptionQuota{{Provider: "workbuddy"}}, accts[:1], st)
	if !one[0].Checkins || one[0].Checkin == nil {
		t.Fatalf("one account, its card unnamed: %+v", one[0])
	}
	if two := withCheckins([]SubscriptionQuota{{Provider: "workbuddy"}}, accts[:2], st); two[0].Checkins {
		t.Fatalf("an unnamed card of two accounts: %+v", two[0])
	}
}

// WorkBuddy's gateway answering the check-in with its bare 401 (モモコ on
// Discord: 签到失败，uni-switch 稍后重试 · Unauthorized, all day, and signing in
// again didn't help) is told apart by reading the account's credits with
// the same headers: they read, so the sign-in is good and the check-in
// takes WorkBuddy's own app — an answer for the day, not asked again; they
// don't, so the sign-in is refused and the account is to be signed in
// again, said in those words, not "Unauthorized".
func TestWorkBuddyCheckinUnauthorized(t *testing.T) {
	for _, tc := range []struct {
		name    string
		refuse  []string
		outcome string
		msg     string
		claims  int
		again   bool
	}{
		{"status", []string{"/v2/billing/meter/checkin-activity-status"}, CheckinOwnApp, "WorkBuddy's own app", 0, false},
		{"claim", []string{"/v2/billing/meter/daily-checkin"}, CheckinOwnApp, "WorkBuddy's own app", 1, false},
		{"sign-in", []string{"*"}, CheckinFailed, "sign in to it again", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeWBCheckin{active: true, refuse401: tc.refuse}
			srv := f.serve(t)
			now := time.Date(2026, 10, 10, 2, 0, 0, 0, time.UTC)
			c := wbCheckinFixture(t, srv, &now)
			rs := c.checkinNow(context.Background(), false)
			if len(rs) != 1 || rs[0].Outcome != tc.outcome || !strings.Contains(rs[0].Msg, tc.msg) || rs[0].Checked() {
				t.Fatalf("got %+v", rs)
			}
			if f.summaries != 1 {
				t.Fatalf("credits read %d times", f.summaries)
			}
			now = now.Add(wbCheckinRetry + time.Minute)
			c.checkinNow(context.Background(), false)
			s, cl := f.counts()
			if again := s == 2; again != tc.again || cl > tc.claims {
				t.Fatalf("after the retry: status %d, claims %d", s, cl)
			}
		})
	}
}

// Through the plugin's fetch too: a moved account's check-in answered 401
// while its credits read is the WorkBuddy app's to do.
func TestWorkBuddyCheckinUnauthorizedThroughPlugin(t *testing.T) {
	var paths []string
	via := func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if strings.HasSuffix(req.URL.Path, "/checkin-activity-status") {
			return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader(wbGateway401)), Header: http.Header{"X-Uni-Switch-Sign-In": {"kept"}}}, nil
		}
		body := `{"code":0,"data":{"Packages":[]}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	}
	a := wbAccount{Login: Login{User: "me", On: true}, site: wbCN, creds: wbCreds{UID: "u1"}, via: via}
	r := wbCheckin(context.Background(), a)
	if r.Outcome != CheckinOwnApp {
		t.Fatalf("got %+v", r)
	}
	if len(paths) != 2 || !strings.HasSuffix(paths[1], "/billing/meter/get-user-resource-summary") || strings.Contains(paths[1], "/v2/") {
		t.Fatalf("asked %v", paths)
	}
}

// #3326: uni-switch's own check-ins go through the same checkiner, so a done
// WorkBuddy gives before its day turns is asked again too, and claimed
// once it has.
func TestWorkBuddyCheckinEarlyDoneAskedAgain(t *testing.T) {
	f := &fakeWBCheckin{active: true, checked: true}
	srv := f.serve(t)
	now := time.Date(2026, 10, 22, 16, 0, 28, 0, time.UTC) // 00:00:28 Beijing
	c := wbCheckinFixture(t, srv, &now)
	if rs := c.checkinNow(context.Background(), false); len(rs) != 1 || rs[0].Outcome != CheckinDone || !rs[0].Again {
		t.Fatalf("at midnight: %+v", rs)
	}
	f.mu.Lock()
	f.checked = false
	f.mu.Unlock()
	now = now.Add(wbCheckinEvery)
	if rs := c.checkinNow(context.Background(), false); len(rs) != 1 || rs[0].Outcome != CheckinClaimed {
		t.Fatalf("30 minutes on: %+v", rs)
	}
	if s, cl := f.counts(); s != 2 || cl != 1 {
		t.Fatalf("status %d, claims %d", s, cl)
	}
}
