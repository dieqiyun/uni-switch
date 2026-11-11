package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// deadKeys answers as #1772's mock did: a key starting with "dead" is
// out of its weekly allowance, in a 429 that names no reset; any other
// is served.
type deadKeys struct {
	mu   sync.Mutex
	keys []string
}

func (d *deadKeys) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body)
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	d.mu.Lock()
	d.keys = append(d.keys, key)
	d.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if strings.HasPrefix(key, "dead") {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"message":"you have reached your weekly usage limit","type":"rate_limit_error"}}`)
		return
	}
	io.WriteString(w, `{"id":"c1","object":"chat.completion","model":"glm-5.3-flash","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
}

func (d *deadKeys) took() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := d.keys
	d.keys = nil
	return out
}

func deadChain(t *testing.T, keys ...string) (*deadKeys, *Server) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	restingUntil.Lock()
	restingUntil.m, restingUntil.note = map[string]time.Time{}, map[string]Rest{}
	restingUntil.Unlock()
	t.Cleanup(func() {
		restingUntil.Lock()
		restingUntil.m, restingUntil.note = map[string]time.Time{}, map[string]Rest{}
		restingUntil.Unlock()
	})
	d := &deadKeys{}
	up := httptest.NewServer(d)
	t.Cleanup(up.Close)
	p := provider.Provider{ID: "mockdead", Name: "mockdead", Chat: up.URL + "/v1", Models: []string{"glm-5.3-flash"}, Routing: provider.Ordered}
	p.Key = keys[0]
	for _, k := range keys[1:] {
		p.Keys = append(p.Keys, provider.KeyAccount{Key: k})
	}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	return d, New()
}

func postDead(t *testing.T, srv *Server) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"mockdead/glm-5.3-flash","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// #1772 (a fleet on four Ollama Cloud keys): every key out of its weekly
// allowance, each new request had asked each key again (the first twice)
// and answered 429 with no Retry-After, so a client that waits as told
// asked again at once. A key is held once it was refused so twice in a
// row (a single refusal may be a moment's, zoecats on Discord), and, its
// vendor naming no time, asked again a minute on, which Retry-After says.
func TestAllRestingAnswersFromRest(t *testing.T) {
	d, srv := deadChain(t, "dead-a", "dead-b")
	rec := postDead(t, srv)
	if rec.Code != 429 {
		t.Fatalf("first: %d %s", rec.Code, rec.Body)
	}
	if got := d.took(); strings.Join(got, ",") != "dead-a,dead-b" {
		t.Errorf("first request asked %v, want each key once", got)
	}
	if ra := rec.Header().Get("Retry-After"); ra != "" {
		t.Errorf("first: Retry-After %q, while each key is asked again at once", ra)
	}
	// each asked once more, the soonest back first, one a request as
	// every candidate resting has it (restLast)
	var again []string
	for i := 0; i < 2; i++ {
		if rec = postDead(t, srv); rec.Code != 429 {
			t.Fatalf("confirming: %d %s", rec.Code, rec.Body)
		}
		again = append(again, d.took()...)
	}
	if slices.Sort(again); strings.Join(again, ",") != "dead-a,dead-b" {
		t.Errorf("asked again %v, want each key once", again)
	}
	for i := 0; i < 2; i++ {
		rec = postDead(t, srv)
		if rec.Code != 429 {
			t.Fatalf("again: %d %s", rec.Code, rec.Body)
		}
		if got := d.took(); len(got) != 0 {
			t.Errorf("again: asked %v while every key rests", got)
		}
		ra, err := strconv.Atoi(rec.Header().Get("Retry-After"))
		if err != nil || ra < 50 || ra > int(fallbackCooldown/time.Second) {
			t.Errorf("again: Retry-After %q, want about %v", rec.Header().Get("Retry-After"), fallbackCooldown)
		}
		if !strings.Contains(rec.Body.String(), "weekly usage limit") {
			t.Errorf("again: the vendor's reason is gone: %s", rec.Body)
		}
	}
}

// A provider whose keys are out for the week, its vendor saying nothing of
// when, rests them as long as the user set (quotaRest), not 15 minutes.
func TestOwnQuotaRest(t *testing.T) {
	d, srv := deadChain(t, "dead-a", "dead-b")
	if err := provider.SetQuotaRest("mockdead", 7*24*60); err != nil {
		t.Fatal(err)
	}
	rec := postDead(t, srv)
	if got := d.took(); rec.Code != 429 || len(got) != 2 {
		t.Fatalf("%d after %v: %s", rec.Code, got, rec.Body)
	}
	// the user's rest is no guess: held from the first refusal, and not
	// asked again a minute on
	rec = postDead(t, srv)
	if got := d.took(); rec.Code != 429 || len(got) != 0 {
		t.Fatalf("held: %d after %v: %s", rec.Code, got, rec.Body)
	}
	week := int(7 * 24 * time.Hour / time.Second)
	if ra, _ := strconv.Atoi(rec.Header().Get("Retry-After")); ra < week-5 || ra > week {
		t.Errorf("Retry-After %q, want a week", rec.Header().Get("Retry-After"))
	}
	for _, c := range []candidate{{p: provider.Provider{ID: "mockdead"}, rest: "mockdead#" + provider.KeyID("dead-a")}} {
		if r, ok := restingOf(c); !ok || time.Until(r.Until) < 7*24*time.Hour-time.Minute {
			t.Errorf("rests %v until %v", ok, r.Until)
		}
	}
	// a time the vendor names stands over it
	p, _ := provider.Find("mockdead")
	c := candidate{p: *p, model: "glm-5.3-flash", rest: "mockdead#x"}
	h := http.Header{"Retry-After": {"120"}}
	if r := srv.restAfter(c, 429, h, []byte(`{"error":{"message":"you have reached your weekly usage limit"}}`)); r.By != "retry-after" || time.Until(r.Until) > 3*time.Minute {
		t.Errorf("a vendor's Retry-After lost to the provider's own rest: %+v", r)
	}
}

// Only a 429 or a 402 out of quota is answered from the rest: a 403 whose
// words read as quota (a key's plan, a region) is asked again as before,
// and so is a subscription account, whose resets may be spent for it.
func TestQuotaHeldOnlyForKeysOutOfAllowance(t *testing.T) {
	deadChain(t, "dead-a")
	srv := New()
	body := []byte(`{"error":{"message":"you have reached your weekly usage limit"}}`)
	key := candidate{p: provider.Provider{ID: "k1"}, rest: "k1"}
	srv.restAfter(key, 429, nil, body)
	if _, _, ok := quotaHeld([]candidate{key}); ok {
		t.Error("a key refused once is held: its retry isn't asked")
	}
	srv.restAfter(key, 429, nil, body)
	if _, _, ok := quotaHeld([]candidate{key}); !ok {
		t.Error("a key out of its allowance twice in a row isn't held")
	}
	forbidden := candidate{p: provider.Provider{ID: "k2"}, rest: "k2"}
	srv.restAfter(forbidden, 403, nil, []byte(`{"error":{"message":"quota exceeded for this region"}}`))
	if _, _, ok := quotaHeld([]candidate{key, forbidden}); ok {
		t.Error("a 403 held")
	}
	acct := candidate{p: provider.Provider{ID: "codex", Account: &provider.Account{Agent: "codex", User: "a@b"}}, rest: "codex"}
	srv.restAfter(acct, 429, nil, body)
	if _, _, ok := quotaHeld([]candidate{acct}); ok {
		t.Error("an account held")
	}
	fresh := candidate{p: provider.Provider{ID: "k3"}, rest: "k3"}
	if _, _, ok := quotaHeld([]candidate{key, fresh}); ok {
		t.Error("held with a key not resting")
	}
}

// #1772's providers.json named its first key in "keys" as well as in
// "key": each key is one candidate, asked once.
func TestFirstKeyListedTwiceAskedOnce(t *testing.T) {
	d, srv := deadChain(t, "dead-1", "dead-1", "dead-2", "ok-3")
	if rec := postDead(t, srv); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got := d.took(); strings.Join(got, ",") != "dead-1,dead-2,ok-3" {
		t.Errorf("cold walk asked %v", got)
	}
}

// A chain whose last key is served walks past the dead ones once each.
func TestDeadChainWalksEachOnce(t *testing.T) {
	d, srv := deadChain(t, "dead-1", "dead-2", "ok-3")
	if rec := postDead(t, srv); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got := d.took(); strings.Join(got, ",") != "dead-1,dead-2,ok-3" {
		t.Errorf("cold walk asked %v", got)
	}
	if rec := postDead(t, srv); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got := d.took(); strings.Join(got, ",") != "ok-3" {
		t.Errorf("warm walk asked %v", got)
	}
}
