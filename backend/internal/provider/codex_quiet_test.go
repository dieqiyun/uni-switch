package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The background look at Codex's accounts (KeepOnAnAccountWithRoom) reads
// the account Codex is on, and the others only once that one is spent.
// It read every saved account at once every five minutes, a burst from one
// address no official client makes, for a user holding several ChatGPT
// accounts in uni-switch with no agent wired (owl on Discord).
func TestCodexLookReadsTheActiveAccountOnly(t *testing.T) {
	home := signIn(t) // me@example.com, acct-1
	rememberLogins(true)
	codexSignIn(t, home, "spare@example.com", "r-spare")
	rememberLogins(true)
	codexSignIn(t, home, "work@example.com", "r-work")
	rememberLogins(true)
	for _, u := range []string{"me@example.com", "spare@example.com"} {
		if err := SetLoginOn("codex", u, true); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	used := map[string]float64{"acct-work@example.com": 20, "acct-1": 20, "acct-spare@example.com": 20}
	hits := map[string]int{}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		id := r.Header.Get("chatgpt-account-id")
		if strings.HasSuffix(r.URL.Path, "/wham/usage") {
			hits[id]++
		}
		pct := used[id]
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"plan_type": "plus", "rate_limit": map[string]any{
			"primary_window": map[string]any{"used_percent": pct, "limit_window_seconds": 18000}}})
	}))
	defer fake.Close()
	old := CodexBase
	CodexBase = fake.URL + "/backend-api/codex"
	t.Cleanup(func() { CodexBase = old })
	look := func() (string, map[string]int) {
		t.Helper()
		loginUsageCache.Lock()
		loginUsageCache.m = nil
		loginUsageCache.Unlock()
		mu.Lock()
		hits = map[string]int{}
		mu.Unlock()
		to, err := SwitchWhenSpent(context.Background(), "codex")
		if err != nil {
			t.Fatal(err)
		}
		CodexUsedUp(context.Background())
		mu.Lock()
		defer mu.Unlock()
		got := map[string]int{}
		for k, v := range hits {
			got[k] = v
		}
		return to, got
	}

	// work@ has room: only work@ is read, by both the switch and CodexUsedUp
	// (the second from the minute's cache)
	if to, got := look(); to != "" || len(got) != 1 || got["acct-work@example.com"] != 1 {
		t.Fatalf("with room: switched to %q, read %v", to, got)
	}
	// work@ spent: now the spares are read, and the first with room taken
	mu.Lock()
	used["acct-work@example.com"] = 100
	mu.Unlock()
	to, got := look()
	if to == "" || got["acct-work@example.com"] != 1 || got["acct-1"]+got["acct-spare@example.com"] == 0 {
		t.Fatalf("spent: switched to %q, read %v", to, got)
	}
	for id, n := range got {
		if n > 1 {
			t.Fatalf("%s read %d times in one look: %v", id, n, got)
		}
	}
}

// Saved sign-ins are renewed as often as Codex renews its own, and one a
// look: renewing every idle account in the same hourly look kept them
// renewed side by side, a burst of N refreshes from one address.
func TestKeepAliveRenewsOneALook(t *testing.T) {
	home := signIn(t)
	rememberLogins(true) // me@ saved
	codexSignIn(t, home, "spare@example.com", "r-spare")
	rememberLogins(true) // spare@ saved
	codexSignIn(t, home, "work@example.com", "r-work")
	rememberLogins(true) // codex on work@
	if keepAliveEvery < 8*24*time.Hour {
		t.Fatalf("saved sign-ins renewed every %v, more often than Codex's 8 days", keepAliveEvery)
	}
	var mu sync.Mutex
	var got []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		got = append(got, body["refresh_token"])
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"access_token": fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())}),
			"refresh_token": body["refresh_token"] + "-2"})
	}))
	defer fake.Close()
	old := codexTokenURL
	codexTokenURL = fake.URL
	t.Cleanup(func() { codexTokenURL = old })

	rs := renewLogins(context.Background(), 0, keepAliveAtOnce)
	renewed := 0
	for _, r := range rs {
		if r.Renewed {
			renewed++
		}
	}
	if len(rs) != 2 || renewed != 1 || len(got) != 1 {
		t.Fatalf("one look renewed %d of %+v, refreshed %v", renewed, rs, got)
	}
}

// Every call uni-switch makes about a ChatGPT account goes as Codex makes it:
// its originator and User-Agent on /wham/usage, the reset credits and the
// token endpoint, as on /codex/models and /responses already. They went
// with Go's "Go-http-client/1.1" and no originator, unlike any request the
// official client sends. The refresh's body is codex-rs's too: a
// refresh_reason, and no scope.
func TestCodexAccountCallsGoAsCodex(t *testing.T) {
	signIn(t)
	type call struct {
		ua, originator, ctype string
		body                  []byte
	}
	var mu sync.Mutex
	calls := map[string][]call{}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls[r.Method+" "+r.URL.Path] = append(calls[r.Method+" "+r.URL.Path], call{r.UserAgent(), r.Header.Get("originator"), r.Header.Get("Content-Type"), b})
		mu.Unlock()
		switch r.URL.Path {
		case "/backend-api/wham/usage":
			json.NewEncoder(w).Encode(map[string]any{"plan_type": "plus",
				"rate_limit":               map[string]any{"primary_window": map[string]any{"used_percent": 10, "limit_window_seconds": 18000}},
				"rate_limit_reset_credits": map[string]any{"available_count": 1}})
		case "/backend-api/wham/rate-limit-reset-credits":
			json.NewEncoder(w).Encode(map[string]any{"available_count": 1, "credits": []any{map[string]any{"id": "c1", "reset_type": "all", "status": "available"}}})
		case "/backend-api/wham/rate-limit-reset-credits/consume":
			json.NewEncoder(w).Encode(map[string]any{"code": "ok", "windows_reset": 1})
		case "/oauth/token":
			json.NewEncoder(w).Encode(map[string]any{"access_token": fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())}),
				"refresh_token": "r-2", "id_token": fakeJWT(map[string]any{"email": "new@example.com"})})
		default:
			w.WriteHeader(404)
		}
	}))
	defer fake.Close()
	codexListedAt(t, fake.URL) // CodexBase, and the machine's own Codex CLI out of the version
	old := codexTokenURL
	codexTokenURL = fake.URL + "/oauth/token"
	t.Cleanup(func() { codexTokenURL = old })

	ctx := context.Background()
	if _, _, resets, _, _, err := codexWindows(ctx, "tok", "acct-1"); err != nil || resets == nil {
		t.Fatalf("usage: resets %+v, %v", resets, err)
	}
	if _, err := consumeCodexReset(ctx, fake.URL+"/backend-api", "tok", "acct-1", "c1", newRedeemID()); err != nil {
		t.Fatal(err)
	}
	if _, err := codexRefresh(ctx, map[string]any{"tokens": map[string]any{"refresh_token": "r-1"}}, codexRefreshScheduled); err != nil {
		t.Fatal(err)
	}
	codexExchange(ctx, "code", "verifier", "http://localhost:1455/auth/callback") // the request is what is checked

	want := map[string]int{"GET /backend-api/wham/usage": 1, "GET /backend-api/wham/rate-limit-reset-credits": 1,
		"POST /backend-api/wham/rate-limit-reset-credits/consume": 1, "POST /oauth/token": 2}
	ua := codexUserAgent(codexVersion())
	mu.Lock()
	defer mu.Unlock()
	for k, n := range want {
		if len(calls[k]) != n {
			t.Errorf("%s asked %d times, want %d (%v)", k, len(calls[k]), n, calls)
		}
		for _, c := range calls[k] {
			if c.ua != ua || c.originator != "codex_cli_rs" {
				t.Errorf("%s sent User-Agent %q, originator %q; Codex sends %q, codex_cli_rs", k, c.ua, c.originator, ua)
			}
		}
	}
	if len(calls) != len(want) {
		t.Errorf("asked %v", calls)
	}
	if tok := calls["POST /oauth/token"]; len(tok) == 2 {
		var refresh map[string]any
		if tok[0].ctype != "application/json" || json.Unmarshal(tok[0].body, &refresh) != nil {
			t.Fatalf("refresh sent as %q: %s", tok[0].ctype, tok[0].body)
		}
		keys := []string{}
		for k := range refresh {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		if strings.Join(keys, ",") != "client_id,grant_type,refresh_reason,refresh_token" ||
			refresh["refresh_reason"] != "scheduled" || refresh["grant_type"] != "refresh_token" || refresh["client_id"] != codexClientID {
			t.Errorf("refresh body %s; codex-rs sends client_id, grant_type, refresh_token and refresh_reason", tok[0].body)
		}
		if tok[1].ctype != "application/x-www-form-urlencoded" || !strings.Contains(string(tok[1].body), "grant_type=authorization_code") {
			t.Errorf("sign-in exchange sent as %q: %s", tok[1].ctype, tok[1].body)
		}
	}
}

// A refresh says why it is made, in codex-rs's words: the keep-alive's is
// "scheduled", one after the backend called the token expired
// "unauthorized_recovery", one asked for otherwise "forced", and one for
// a token about to run out "proactive_expiry".
func TestCodexRefreshSaysWhy(t *testing.T) {
	home := signIn(t)
	rememberLogins(true) // me@ saved
	codexSignIn(t, home, "work@example.com", "r-work")
	rememberLogins(true) // Codex on work@
	var mu sync.Mutex
	var why []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		why = append(why, body["refresh_reason"])
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"access_token": fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix()), "n": len(why)}),
			"refresh_token": body["refresh_token"] + "-2"})
	}))
	defer fake.Close()
	old := codexTokenURL
	codexTokenURL = fake.URL
	t.Cleanup(func() { codexTokenURL = old })

	ctx := context.Background()
	// me@ was last seen nine days ago: the keep-alive renews it, as due
	loginsMu.Lock()
	ls := readLogins()
	for i := range ls {
		if ls[i].User == "me@example.com" {
			ls[i].Seen = time.Now().Add(-9 * 24 * time.Hour)
		}
	}
	err := writeLogins(ls)
	loginsMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if rs := renewLogins(ctx, keepAliveEvery, keepAliveAtOnce); len(rs) != 1 || !rs[0].Renewed {
		t.Fatalf("keep-alive: %+v", rs)
	}
	tok, _, err := renewSavedLogin(ctx, "codex", "me@example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := renewSavedLoginFrom(ctx, "codex", "me@example.com", tok); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(why, ",") != "scheduled,forced,unauthorized_recovery" {
		t.Fatalf("refreshes said %v", why)
	}
}

// A saved ChatGPT account's model list is asked for once a day, one
// account a look; the hourly relist asked every switched-on saved
// account's beside the active one's, a burst of /codex/models calls from
// one address for accounts no Codex was using. An account with no list
// yet is asked at once, and one that fails waits its day like the rest.
func TestSavedCodexListsOnceADay(t *testing.T) {
	home := signIn(t) // me@example.com, acct-1
	rememberLogins(true)
	codexSignIn(t, home, "spare@example.com", "r-spare")
	rememberLogins(true)
	codexSignIn(t, home, "work@example.com", "r-work")
	rememberLogins(true) // Codex on work@
	for _, u := range []string{"me@example.com", "spare@example.com"} {
		if err := SetLoginOn("codex", u, true); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	hits := map[string]int{}
	var down atomic.Bool
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/codex/models" {
			w.WriteHeader(404)
			return
		}
		mu.Lock()
		hits[r.Header.Get("chatgpt-account-id")]++
		mu.Unlock()
		if down.Load() {
			w.WriteHeader(401)
			return
		}
		w.Write(codexListOf("gpt-6.1-sol"))
	}))
	defer fake.Close()
	codexListedAt(t, fake.URL)
	now := time.Now()
	oldNow := codexSavedNow
	codexSavedNow = func() time.Time { return now }
	t.Cleanup(func() { codexSavedNow = oldNow })
	look := func(at time.Duration) map[string]int {
		t.Helper()
		now = time.Now().Add(at)
		mu.Lock()
		hits = map[string]int{}
		mu.Unlock()
		codexFetchSaved(context.Background())
		mu.Lock()
		defer mu.Unlock()
		got := map[string]int{}
		for k, v := range hits {
			got[k] = v
		}
		return got
	}
	// no list yet: both saved accounts are asked, never the active one here
	if got := look(0); len(got) != 2 || got["acct-1"] != 1 || got["acct-spare@example.com"] != 1 {
		t.Fatalf("first look: %v", got)
	}
	// within the day: none
	if got := look(time.Hour); len(got) != 0 {
		t.Fatalf("an hour on: %v", got)
	}
	if codexSavedListAge < 24*time.Hour || codexSavedAtOnce != 1 {
		t.Fatalf("saved lists asked every %v, %d a look", codexSavedListAge, codexSavedAtOnce)
	}
	// a day on: one a look, then the other, then none till the next day
	first := look(25 * time.Hour)
	second := look(26 * time.Hour)
	if len(first) != 1 || len(second) != 1 || first["acct-1"]+second["acct-1"] != 1 {
		t.Fatalf("a day on: %v, then %v", first, second)
	}
	if got := look(27 * time.Hour); len(got) != 0 {
		t.Fatalf("both asked today, asked again: %v", got)
	}
	// failing: still once a day each, not first in every look
	down.Store(true)
	look(50 * time.Hour)
	look(51 * time.Hour)
	if got := look(52 * time.Hour); len(got) != 0 {
		t.Fatalf("failing accounts asked again within the day: %v", got)
	}
}
