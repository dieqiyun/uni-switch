package gateway

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// tavilyUsageDoc is GET /usage's 200 reply as Tavily's API reference
// (docs.tavily.com/documentation/api-reference/endpoint/usage) gives it.
const tavilyUsageDoc = `{
  "key": {
    "usage": 150,
    "limit": 1000,
    "search_usage": 100,
    "extract_usage": 25,
    "crawl_usage": 15,
    "map_usage": 7,
    "research_usage": 3
  },
  "account": {
    "current_plan": "Bootstrap",
    "plan_usage": 500,
    "plan_limit": 15000,
    "paygo_usage": 25,
    "paygo_limit": 100,
    "search_usage": 350,
    "extract_usage": 75,
    "crawl_usage": 50,
    "map_usage": 15,
    "research_usage": 10
  }
}`

// tavilyUnauthorized is Tavily's 401, as its real API answered an invalid
// key at /search and /usage on 2026-10-14.
const tavilyUnauthorized = `{"detail":{"error":"Unauthorized: missing or invalid API key."}}`

// A Tavily /usage reply is read into what the key has used of its limit:
// the key's own limit when it has one; else, as a free account's key has
// none (key.limit null), its plan's, with the plan's usage; with neither,
// no limit. A reply that leaves out what it takes is unknown, never 0.
func TestReadTavilyUsage(t *testing.T) {
	q, err := readTavilyUsage([]byte(tavilyUsageDoc))
	if err != nil || !q.Known || q.Used != 150 || q.Limit != 1000 || q.Of != "key" || q.Plan != "Bootstrap" || q.PaygoLeft != 75 || q.UsedUp {
		t.Errorf("the documented reply: %+v, %v", q, err)
	}
	free := strings.NewReplacer(`"limit": 1000`, `"limit": null`, `"plan_limit": 15000`, `"plan_limit": 1000`,
		`"paygo_usage": 25`, `"paygo_usage": 0`, `"paygo_limit": 100`, `"paygo_limit": null`, `"Bootstrap"`, `"Researcher"`).Replace(tavilyUsageDoc)
	if q, err := readTavilyUsage([]byte(free)); err != nil || !q.Known || q.Used != 500 || q.Limit != 1000 || q.Of != "plan" || q.Plan != "Researcher" || q.UsedUp {
		t.Errorf("a free account's key, its limit the plan's: %+v, %v", q, err)
	}
	spent := strings.Replace(free, `"plan_usage": 500`, `"plan_usage": 1000`, 1)
	if q, _ := readTavilyUsage([]byte(spent)); !q.Known || !q.UsedUp {
		t.Errorf("a plan used up with no pay-as-you-go: %+v", q)
	}
	paygo := strings.NewReplacer(`"plan_usage": 500`, `"plan_usage": 1000`, `"paygo_limit": null`, `"paygo_limit": 100`).Replace(free)
	if q, _ := readTavilyUsage([]byte(paygo)); !q.Known || q.UsedUp || q.PaygoLeft != 100 {
		t.Errorf("a plan used up with pay-as-you-go left is not used up: %+v", q)
	}
	keySpent := strings.Replace(tavilyUsageDoc, `"usage": 150`, `"usage": 1000`, 1)
	if q, _ := readTavilyUsage([]byte(keySpent)); !q.UsedUp {
		t.Errorf("a key at its own limit: %+v", q)
	}
	unlimited := strings.NewReplacer(`"limit": 1000`, `"limit": null`, `"plan_limit": 15000`, `"plan_limit": null`).Replace(tavilyUsageDoc)
	if q, err := readTavilyUsage([]byte(unlimited)); err != nil || !q.Known || !q.Unlimited || q.Used != 150 || q.UsedUp {
		t.Errorf("no limit anywhere: %+v, %v", q, err)
	}
	for name, b := range map[string]string{
		"key.limit left out":  strings.Replace(tavilyUsageDoc, `"limit": 1000,`, ``, 1),
		"plan_limit left out": strings.NewReplacer(`"limit": 1000`, `"limit": null`, `"plan_limit": 15000,`, ``).Replace(tavilyUsageDoc),
		"key.usage null":      strings.Replace(tavilyUsageDoc, `"usage": 150`, `"usage": null`, 1),
		"no key":              `{"account":{"plan_usage":1,"plan_limit":2}}`,
		"a string limit":      strings.Replace(tavilyUsageDoc, `"limit": 1000`, `"limit": "1000"`, 1),
		"the 401's body":      tavilyUnauthorized,
		"not JSON":            `<html>502 Bad Gateway</html>`,
		"empty":               ``,
		"truncated":           tavilyUsageDoc[:80],
	} {
		if q, err := readTavilyUsage([]byte(b)); err == nil || q.Known {
			t.Errorf("%s: read as %+v, want unknown", name, q)
		}
	}
}

// keyedTavily is a fake Tavily that answers each key as answers says
// ("ok", or a status), and records the keys it was asked with.
type keyedTavily struct {
	*httptest.Server
	mu      sync.Mutex
	asked   []string
	answers map[string]int
	usage   map[string]string // /usage's reply by key; none is a 401
}

func newKeyedTavily(t *testing.T, answers map[string]int) *keyedTavily {
	f := &keyedTavily{answers: answers, usage: map[string]string{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/usage" {
			f.mu.Lock()
			b, ok := f.usage[key]
			f.mu.Unlock()
			if !ok {
				// as an API that says the key back would
				w.WriteHeader(401)
				fmt.Fprintf(w, `{"detail":{"error":"Unauthorized: invalid API key %s"}}`, key)
				return
			}
			io.WriteString(w, b)
			return
		}
		f.mu.Lock()
		f.asked = append(f.asked, key)
		status := f.answers[key]
		f.mu.Unlock()
		switch status {
		case 0, 200:
			io.WriteString(w, `{"query":"x","results":[{"title":"Go downloads","url":"https://go.dev/dl/","content":"go1.27.1 from `+key+`","score":0.9}]}`)
		case 401:
			w.WriteHeader(401)
			fmt.Fprintf(w, `{"detail":{"error":"Unauthorized: missing or invalid API key %s."}}`, key)
		case 429:
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(429)
			io.WriteString(w, `{"detail":{"error":"Too many requests"}}`)
		default:
			w.WriteHeader(status)
			io.WriteString(w, `{"detail":{"error":"This request exceeds your plan's set usage limit."}}`)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *keyedTavily) took() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.asked
	f.asked = nil
	return out
}

func setTavily(t *testing.T, url, keys, pick string) provider.SearchAPI {
	t.Helper()
	if err := provider.SetSearchAPI(provider.SearchAPI{Vendor: "tavily", Key: keys, URL: url, Pick: pick}); err != nil {
		t.Fatal(err)
	}
	return provider.StoredSearchAPIs()[0]
}

// Several keys take turns: each search starts from the next, so the
// searches are spread over them; set to "order", every search asks the
// first.
func TestSearchKeysTakeTurnsRoundRobin(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	forgetSearchKeys()
	t.Cleanup(forgetSearchKeys)
	f := newKeyedTavily(t, map[string]int{})
	setTavily(t, f.URL, "tvly-a,tvly-b,tvly-c", "")
	s := New()
	var got []string
	for range 7 {
		if _, _, err := s.apiSearch(context.Background(), "latest go"); err != nil {
			t.Fatal(err)
		}
		got = append(got, f.took()...)
	}
	if want := []string{"tvly-a", "tvly-b", "tvly-c", "tvly-a", "tvly-b", "tvly-c", "tvly-a"}; !slices.Equal(got, want) {
		t.Errorf("in turn: asked %v, want %v", got, want)
	}

	setTavily(t, f.URL, "", "order")
	got = nil
	for range 3 {
		if _, _, err := s.apiSearch(context.Background(), "latest go"); err != nil {
			t.Fatal(err)
		}
		got = append(got, f.took()...)
	}
	if want := []string{"tvly-a", "tvly-a", "tvly-a"}; !slices.Equal(got, want) {
		t.Errorf("in order: asked %v, want %v", got, want)
	}
	if a := setTavily(t, f.URL, "tvly-a,tvly-b", ""); a.Pick != provider.SearchPickOrder {
		t.Errorf("a new key alone changed the pick to %q", a.Pick)
	}
	if a := setTavily(t, f.URL, "", "turns"); a.Pick != provider.SearchPickTurns {
		t.Errorf("pick=turns left %q", a.Pick)
	}
}

// A key refused (Tavily's real 401) rests: the search goes on to the next
// key, and the searches after it don't ask it while another key answers,
// until its rest ends. A key that isn't refused (a 500) isn't rested.
func TestSearchKeyRefusedRests(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	forgetSearchKeys()
	t.Cleanup(forgetSearchKeys)
	f := newKeyedTavily(t, map[string]int{"tvly-bad": 401})
	setTavily(t, f.URL, "tvly-bad,tvly-b,tvly-c", "")
	s := New()
	if _, _, err := s.apiSearch(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	if got := f.took(); !slices.Equal(got, []string{"tvly-bad", "tvly-b"}) {
		t.Fatalf("first search asked %v, want the refused key then the next", got)
	}
	r, ok := searchKeyRest("tavily", "tvly-bad", time.Now())
	if !ok || r.Why != "turned away" || time.Until(r.Until) < 9*time.Minute {
		t.Fatalf("the 401's key rests %+v, %v", r, ok)
	}
	var got []string
	for range 6 {
		if _, _, err := s.apiSearch(context.Background(), "q"); err != nil {
			t.Fatal(err)
		}
		got = append(got, f.took()...)
	}
	if slices.Contains(got, "tvly-bad") {
		t.Errorf("a resting key was asked while others answered: %v", got)
	}
	if !slices.Contains(got, "tvly-b") || !slices.Contains(got, "tvly-c") {
		t.Errorf("the searches weren't spread over the others: %v", got)
	}

	// its rest over, it is asked again in its turn
	searchRests.Store(searchKeyID("tavily", "tvly-bad"), keyRest{Until: time.Now().Add(-time.Second), Why: "turned away"})
	got = nil
	for range 3 {
		s.apiSearch(context.Background(), "q")
		got = append(got, f.took()...)
	}
	if !slices.Contains(got, "tvly-bad") {
		t.Errorf("a key whose rest ended wasn't asked again: %v", got)
	}

	// every key resting: each is still asked, the soonest awake first
	forgetSearchKeys()
	f.answers["tvly-b"], f.answers["tvly-c"] = 432, 429
	setTavily(t, f.URL, "tvly-bad,tvly-b,tvly-c", "")
	_, _, err := s.apiSearch(context.Background(), "q")
	if err == nil {
		t.Fatal("every key refused, and the search said nothing")
	}
	f.took()
	if r, _ := searchKeyRest("tavily", "tvly-b", time.Now()); r.Why != "out of credit" || time.Until(r.Until) < 59*time.Minute {
		t.Errorf("Tavily's 432 rests %+v", r)
	}
	if r, _ := searchKeyRest("tavily", "tvly-c", time.Now()); r.Why != "rate limited" || time.Until(r.Until) < 110*time.Second || time.Until(r.Until) > 125*time.Second {
		t.Errorf("a 429 with Retry-After: 120 rests %+v", r)
	}
	f.answers["tvly-c"] = 200
	if _, _, err := s.apiSearch(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	if got := f.took(); !slices.Equal(got, []string{"tvly-c"}) {
		t.Errorf("all resting, asked %v, want the one whose rest ends first", got)
	}
	if _, ok := searchKeyRest("tavily", "tvly-c", time.Now()); ok {
		t.Error("a key that answered still rests")
	}

	// a 500 isn't about the key
	forgetSearchKeys()
	f.answers["tvly-bad"] = 500
	s.apiSearch(context.Background(), "q")
	if _, ok := searchKeyRest("tavily", "tvly-bad", time.Now()); ok {
		t.Error("a 500 rested the key")
	}
}

// How long a refusal rests a key: Retry-After held within its bounds,
// and 432/433 out of credit only from Tavily, whose codes they are.
func TestSearchRestFor(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		vendor     string
		status     int
		retryAfter string
		d          time.Duration
		why        string
	}{
		{"tavily", 401, "", turnedAwayRest, "turned away"},
		{"brave", 403, "", turnedAwayRest, "turned away"},
		{"exa", 402, "", outOfCreditRest, "out of credit"},
		{"tavily", 432, "", outOfCreditRest, "out of credit"},
		{"tavily", 433, "", outOfCreditRest, "out of credit"},
		{"brave", 432, "", rateLimitRest, "rate limited"},
		{"tavily", 429, "", rateLimitRest, "rate limited"},
		{"tavily", 429, "120", 2 * time.Minute, "rate limited"},
		{"tavily", 429, "1", rateLimitRestMin, "rate limited"},
		{"tavily", 429, "86400", rateLimitRestMax, "rate limited"},
		{"tavily", 429, now.Add(3 * time.Minute).Format(http.TimeFormat), 3 * time.Minute, "rate limited"},
		{"tavily", 429, "soon", rateLimitRest, "rate limited"},
	} {
		if d, why := searchRestFor(c.vendor, c.status, c.retryAfter, now); d != c.d || why != c.why {
			t.Errorf("%s %d %q: %v %q, want %v %q", c.vendor, c.status, c.retryAfter, d, why, c.d, c.why)
		}
	}
}

// What each key has used is read from Tavily's /usage, in the order of
// the keys; one whose read fails is unknown with why, never 0 used. A key
// it finds used up rests, and wakes when a later read finds credit.
func TestSearchQuotas(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	forgetSearchKeys()
	searchQuotas.Clear()
	t.Cleanup(func() { forgetSearchKeys(); searchQuotas.Clear() })
	f := newKeyedTavily(t, map[string]int{})
	good, spent, bad := "tvly-dev-good0000000001234", "tvly-dev-spent000000005678", "tvly-dev-bad00000000009abc"
	f.usage[good] = tavilyUsageDoc
	f.usage[spent] = strings.Replace(tavilyUsageDoc, `"usage": 150`, `"usage": 1000`, 1)
	a := setTavily(t, f.URL, good+","+spent+","+bad, "")
	if CanReadSearchQuota(provider.SearchAPI{Vendor: "brave", Key: "b"}) || !CanReadSearchQuota(a) {
		t.Error("only Tavily's quota is read")
	}
	qs := SearchQuotas(context.Background(), a, true)
	if len(qs) != 3 {
		t.Fatalf("%d quotas for 3 keys", len(qs))
	}
	if q := qs[0]; q.Key != "tvly-…1234" || !q.Known || q.Used != 150 || q.Limit != 1000 || q.UsedUp || q.Resting != "" {
		t.Errorf("good: %+v", q)
	}
	if q := qs[1]; q.Key != "tvly-…5678" || !q.UsedUp || q.Resting != "used up" {
		t.Errorf("used up: %+v", q)
	}
	if q := qs[2]; q.Key != "tvly-…9abc" || q.Known || q.Err == "" || !strings.Contains(q.Err, "401") {
		t.Errorf("a 401 is unknown: %+v", q)
	}
	// the used-up key is asked last
	s := New()
	for range 4 {
		s.apiSearch(context.Background(), "q")
	}
	if got := f.took(); slices.Contains(got, spent) {
		t.Errorf("a used-up key was asked while others answered: %v", got)
	}
	// topped up: read again, it wakes
	f.mu.Lock()
	f.usage[spent] = tavilyUsageDoc
	f.mu.Unlock()
	if q := SearchQuotas(context.Background(), a, false)[1]; !q.UsedUp {
		t.Errorf("a read kept for %v was read again unasked: %+v", searchQuotaKeep, q)
	}
	if q := SearchQuotas(context.Background(), a, true)[1]; q.UsedUp || q.Resting != "" {
		t.Errorf("topped up, read fresh: %+v", q)
	}
	// no answer: unknown, not 0
	f.Close()
	if q := SearchQuotas(context.Background(), a, true)[0]; q.Known || q.Err == "" {
		t.Errorf("no answer: %+v", q)
	}
}

// No key is ever said in full: not in a search's error (an API that says
// the key back in its own), a quota's, the log, or the state uni-switch keeps
// of the keys.
func TestSearchKeyNeverInFull(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	forgetSearchKeys()
	searchQuotas.Clear()
	t.Cleanup(func() { forgetSearchKeys(); searchQuotas.Clear() })
	var logged bytes.Buffer
	was := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(was) })
	k1, k2 := "tvly-dev-SECRETone00000000aaaa", "tvly-dev-SECRETtwo00000000bbbb"
	f := newKeyedTavily(t, map[string]int{k1: 401, k2: 401})
	a := setTavily(t, f.URL, k1+","+k2, "")
	s := New()
	_, _, err := s.apiSearch(context.Background(), "q")
	if err == nil {
		t.Fatal("both keys refused, and no error")
	}
	said := []string{err.Error(), logged.String(), a.MaskedKey()}
	for _, q := range SearchQuotas(context.Background(), a, true) {
		said = append(said, fmt.Sprintf("%+v", q))
	}
	for _, m := range []*sync.Map{&searchRests, &searchQuotas, &searchTurns} {
		m.Range(func(k, v any) bool { said = append(said, fmt.Sprintf("%v %+v", k, v)); return true })
	}
	all := strings.Join(said, "\n")
	for _, k := range []string{k1, k2, "SECRET"} {
		if strings.Contains(all, k) {
			t.Errorf("%q is said in full:\n%s", k, all)
		}
	}
	if !strings.Contains(err.Error(), "tvly-…aaaa") || !strings.Contains(err.Error(), "key 2 (tvly-…bbbb)") {
		t.Errorf("the error doesn't name the keys masked: %v", err)
	}
}
