package gateway

import (
	"bytes"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Tests don't ask vendors how much of an allowance is used: the gateway
// would, behind a request, of whatever upstream a test serves — nor how
// much of a key's own windows.
func init() {
	allowances = func(string) map[string]provider.Allowance { return nil }
	keyAllowance = func(provider.Provider) (provider.Allowance, bool) { return nil, false }
}

func restsOf(cs []candidate) string {
	s := ""
	for _, c := range cs {
		s += c.rest + " "
	}
	return s
}

func TestRouting(t *testing.T) {
	forgetRouting()
	cs := []candidate{{rest: "r#a"}, {rest: "r#b"}, {rest: "r#c"}}
	p := provider.Provider{ID: "r"}
	if got := restsOf(route(p, cs, "m", provider.Chat)); got != "r#a r#b r#c " {
		t.Fatalf("smart: %s", got)
	}

	p.Routing = provider.Rotate
	var firsts string
	for range 4 {
		firsts += route(p, cs, "m", provider.Chat)[0].rest + " "
	}
	if firsts != "r#a r#b r#c r#a " {
		t.Fatalf("in turn: %s", firsts)
	}
	if restsOf(cs) != "r#a r#b r#c " {
		t.Fatal("rotating changed the list it was given")
	}

	p.Routing = provider.Ordered
	if got := restsOf(route(p, cs, "m", provider.Chat)); got != "r#a r#b r#c " {
		t.Fatalf("in order: %s", got)
	}

	p.ID, p.Routing = "u", provider.LeastUsed
	cs = []candidate{{rest: "u#a"}, {rest: "u#b"}, {rest: "u#c"}}
	served("u#a", "u#a", 5000)
	served("u#c", "u#c", 10)
	if got := restsOf(route(p, cs, "m", provider.Chat)); got != "u#b u#c u#a " {
		t.Fatalf("least used: %s", got)
	}
}

func TestRoutingKeepsModelAllowancesSeparate(t *testing.T) {
	old := allowances
	t.Cleanup(func() { allowances = old })
	allowances = func(string) map[string]provider.Allowance {
		return map[string]provider.Allowance{
			"account": {{Used: 25, Model: "grok"}, {Used: 100, Model: "claude"}},
		}
	}
	p := provider.Provider{ID: "cursor", Account: &provider.Account{Agent: "cursor", User: "account"}}
	for _, routing := range []string{"", provider.LeastUsed} {
		for _, reverse := range []bool{false, true} {
			p.Routing = routing
			cs := []candidate{
				{p: p, model: "claude-opus-5-5", rest: "cursor#account"},
				{p: p, model: "grok-4.7-xhigh-fast", rest: "cursor#account"},
			}
			if reverse {
				cs[0], cs[1] = cs[1], cs[0]
			}
			got, wg := weigh(p, cs, "group/mixed", provider.Chat)
			if got[0].model != "grok-4.7-xhigh-fast" {
				t.Errorf("routing %q, reverse %v: exhausted model goes first: %s", routing, reverse, got[0].model)
			}
			for _, c := range got {
				want := 25.0
				if c.model == "claude-opus-5-5" {
					want = 100
				}
				w := weighed(c, p, wg, false, provider.Chat)
				if !w.Known || w.Used != want {
					t.Errorf("routing %q, reverse %v: %s trace = known %v, used %g; want %g", routing, reverse, c.model, w.Known, w.Used, want)
				}
			}
		}
	}
}

// Smart routing keeps the first while it has quota to spare, then goes to
// whichever has the most; a failure rests as long as it says.
func TestSmartRouting(t *testing.T) {
	forgetRouting()
	old := allowances
	defer func() { allowances = old }()
	one := func(used float64, resets time.Time) provider.Allowance {
		return provider.Allowance{{Used: used, Resets: resets}}
	}
	share := map[string]provider.Allowance{"a": one(10, time.Time{}), "b": one(50, time.Time{}), "c": one(20, time.Time{})}
	allowances = func(string) map[string]provider.Allowance { return share }
	acctFor := func(user, model string) candidate {
		return candidate{p: provider.Provider{Account: &provider.Account{Agent: "x", User: user}}, model: model, rest: "s#" + user}
	}
	acct := func(user string) candidate { return acctFor(user, "m") }
	p := provider.Provider{ID: "s", Account: &provider.Account{Agent: "x"}}
	cs := []candidate{acct("a"), acct("b"), acct("c")}
	if got := restsOf(route(p, cs, "m", provider.Chat)); got != "s#a s#b s#c " {
		t.Fatalf("all fine: %s", got)
	}
	share["a"], share["b"] = one(99, time.Time{}), one(93, time.Time{})
	if got := restsOf(route(p, cs, "m", provider.Chat)); got != "s#c s#b s#a " {
		t.Fatalf("a used up, b low: %s", got)
	}

	// the allowance renewing soonest goes first: what it has left is lost
	// at its reset; one not known, or already renewed, after
	now := time.Now()
	share = map[string]provider.Allowance{
		"a": one(5, now.Add(6*24*time.Hour)),
		"b": one(70, now.Add(20*time.Hour)),
		"c": one(10, time.Time{}),
		"d": one(40, now.Add(3*24*time.Hour)),
		"e": one(95, now.Add(time.Hour)),
		"f": one(99, now.Add(-time.Hour)), // used up, but that has renewed since
	}
	cs = []candidate{acct("a"), acct("b"), acct("c"), acct("d"), acct("e"), acct("f")}
	if got := restsOf(route(p, cs, "m", provider.Chat)); got != "s#b s#d s#a s#c s#f s#e " {
		t.Fatalf("by reset: %s", got)
	}
	// minutes apart is the same hour: the order given stays
	share["a"] = one(5, share["b"][0].Resets.Truncate(time.Hour).Add(time.Minute))
	share["b"] = one(70, share["b"][0].Resets.Truncate(time.Hour).Add(2*time.Minute))
	if got := restsOf(route(p, cs, "m", provider.Chat)[:2]); got != "s#a s#b " {
		t.Fatalf("same hour: %s", got)
	}

	// the week decides, not the five hours in it; the five hours only
	// between weeks renewing in the same hour
	// Keep the weekly resets in one hour: time.Now() near an hour
	// boundary made this tie-breaker test flaky in CI.
	routingNow := now.Truncate(time.Hour).Add(20 * time.Minute)
	week, day := 7*24*time.Hour, 24*time.Hour
	both := func(five, weekly time.Duration) provider.Allowance {
		return provider.Allowance{
			{Used: 30, Resets: routingNow.Add(five), Span: 5 * time.Hour},
			{Used: 30, Resets: routingNow.Add(weekly), Span: week},
		}
	}
	share = map[string]provider.Allowance{
		"a": both(10*time.Minute, 5*day),
		"b": both(4*time.Hour, 2*day),
		"c": both(3*time.Hour, 2*day+time.Minute),
	}
	cs = []candidate{acct("a"), acct("b"), acct("c")}
	if got := restsOf(route(p, cs, "m", provider.Chat)); got != "s#c s#b s#a " {
		t.Fatalf("week, then five hours: %s", got)
	}

	// Opus's own weekly allowance used up leaves the account to Sonnet
	share = map[string]provider.Allowance{
		"a": {{Used: 20, Span: week}, {Used: 100, Resets: now.Add(day), Span: week, Model: "opus"}},
		"b": {{Used: 60, Span: week}},
	}
	for _, c := range []struct{ model, want string }{{"claude-opus-5-5", "s#b s#a "}, {"claude-sonnet-5", "s#a s#b "}} {
		cs = []candidate{acctFor("a", c.model), acctFor("b", c.model)}
		if got := restsOf(route(p, cs, c.model, provider.Chat)); got != c.want {
			t.Fatalf("%s: %s", c.model, got)
		}
	}

	for _, c := range []struct {
		status int
		body   string
		want   string
	}{
		{402, `{}`, failCredit},
		{400, `{"error":{"message":"Your credit balance is too low"}}`, failCredit},
		{429, `{"error":{"code":"insufficient_quota"}}`, failCredit},
		{403, `账户余额不足`, failCredit},
		{429, `{"error":{"message":"usage limit reached for your plan"}}`, failQuota},
		{429, `{"error":{"message":"Too many requests"}}`, failRate},
		{429, "You've hit your limit · resets 5pm (Asia/Shanghai)", failQuota},
		{502, "Claude AI usage limit reached|1790000000", failQuota},
		{503, `overloaded`, failOther},
		// a 5xx is the server's, whatever its words, unless the vendor's
		// quota code says otherwise (#1418)
		{500, `当前模型 gpt-5.5 负载已经达到上限，请稍后重试 (request id: 20261011161901234567890abcdefgh)`, failOther},
		{503, `{"error":{"message":"upstream quota service unavailable"}}`, failOther},
		{500, `{"error":{"type":"usage_limit_reached","message":"The usage limit has been reached","resets_in_seconds":3600}}`, failQuota},
		{503, `{"error":{"code":429,"message":"Resource has been exhausted (e.g. check quota).","status":"RESOURCE_EXHAUSTED"}}`, failQuota},
		{403, `今日额度已达上限`, failQuota},
		{429, `{"error":{"message":"当前分组上游负载已饱和，请稍后再试"}}`, failRate},
	} {
		if got := failure(c.status, []byte(c.body)); got != c.want {
			t.Errorf("%d %s: %s, want %s", c.status, c.body, got, c.want)
		}
	}

	s := &Server{}
	until := func(id string) time.Duration {
		restingUntil.Lock()
		defer restingUntil.Unlock()
		return time.Until(restingUntil.m[id]).Round(time.Minute)
	}
	key := func(rest string) candidate { return candidate{rest: rest} }
	s.restAfter(key("t#credit"), 402, http.Header{}, nil)
	h := http.Header{}
	h.Set("Retry-After", "300")
	s.restAfter(key("t#rate"), 429, h, []byte("slow down"))
	s.restAfter(key("t#fail"), 500, http.Header{}, nil)
	// failing again while it rests keeps its step (ly on Discord: one
	// request's own retries counted alike); once the rest is over, the
	// next failure climbs to the next
	s.restAfter(key("t#fail"), 500, http.Header{}, nil)
	if until("t#fail") != time.Minute {
		t.Fatalf("failing again while resting: %v", until("t#fail"))
	}
	restingUntil.Lock()
	restingUntil.m["t#fail"] = time.Now().Add(-time.Second)
	restingUntil.Unlock()
	s.restAfter(key("t#fail"), 500, http.Header{}, nil)
	if until("t#credit") != creditRest || until("t#rate") != 5*time.Minute || until("t#fail") != 2*time.Minute {
		t.Fatalf("rests: %v %v %v", until("t#credit"), until("t#rate"), until("t#fail"))
	}
	served("t#fail", "t#fail", 1)
	s.restAfter(key("t#fail"), 500, http.Header{}, nil)
	if until("t#fail") != time.Minute {
		t.Fatalf("answering again starts over: %v", until("t#fail"))
	}
	// a subscription out of quota rests until the window it filled renews,
	// or until when Claude Code says it resets
	share = map[string]provider.Allowance{"q": {{Used: 30, Span: week}, {Used: 100, Resets: now.Add(3 * time.Hour), Span: 5 * time.Hour}}}
	s.restAfter(acct("q"), 429, http.Header{}, []byte("You've hit your limit · resets 5pm"))
	s.restAfter(acct("r"), 429, http.Header{}, []byte("Claude AI usage limit reached|"+strconv.FormatInt(now.Add(2*time.Hour).Unix(), 10)))
	s.restAfter(acct("o"), 429, http.Header{}, []byte("You've hit your limit"))
	if until(acct("q").restKey()) != 3*time.Hour || until(acct("r").restKey()) != 2*time.Hour || until(acct("o").restKey()) != quotaRest {
		t.Fatalf("subscription rests: %v %v %v", until(acct("q").restKey()), until(acct("r").restKey()), until(acct("o").restKey()))
	}
	// and so does one that failed some other way with a window full
	s.restAfter(acct("q"), 502, http.Header{}, []byte("Claude Code ended without an answer"))
	if until(acct("q").restKey()) != 3*time.Hour {
		t.Fatalf("failed full: %v", until(acct("q").restKey()))
	}

	h = http.Header{}
	h.Set("X-Ratelimit-Reset-Tokens", "6m0s")
	if d := retryAfter(h, time.Now()); d != 6*time.Minute {
		t.Fatalf("reset header: %v", d)
	}
}

// A Claude account whose allowance isn't known goes before those known,
// as its answer tells what it has left; one of another agent, after.
func TestUnknownClaudeGoesFirst(t *testing.T) {
	old := allowances
	defer func() { allowances = old }()
	now := time.Now()
	allowances = func(agent string) map[string]provider.Allowance {
		if agent == "codex" {
			return map[string]provider.Allowance{"k": {{Used: 10, Resets: now.Add(24 * time.Hour)}}}
		}
		return nil
	}
	acct := func(agent, user string) candidate {
		return candidate{p: provider.Provider{Account: &provider.Account{Agent: agent, User: user}}, model: "m", rest: "g#" + user}
	}
	p := provider.Provider{ID: "g"}
	cs := []candidate{acct("codex", "k"), acct("x", "u"), acct("claude", "c")}
	got, wg := weigh(p, cs, "m", provider.Anthropic)
	if s := restsOf(got); s != "g#c g#k g#u " {
		t.Fatalf("order: %s", s)
	}
	if w := weighed(got[0], p, wg, false, provider.Anthropic); !w.Learns || w.Known {
		t.Fatalf("claude: %+v", w)
	}
	if w := weighed(got[2], p, wg, false, provider.Anthropic); w.Learns {
		t.Fatalf("other agent: %+v", w)
	}
}

// Smart with an account that has the five hours alone (Claude
// Enterprise): they renew soonest, so it goes first, ahead of weeks — but
// behind a week renewing sooner still — and so does it with its five
// hours not started, which renew within five hours of now (#576), not
// after every week as a reset not known.
func TestSmartFiveHoursAlone(t *testing.T) {
	old := allowances
	defer func() { allowances = old }()
	now := time.Now().Truncate(time.Hour).Add(20 * time.Minute)
	five, week := 5*time.Hour, 7*24*time.Hour
	weekly := func(in time.Duration) provider.Allowance {
		return provider.Allowance{
			{Used: 10, Resets: now.Add(time.Hour), Span: five},
			{Used: 30, Resets: now.Add(in), Span: week},
		}
	}
	share := map[string]provider.Allowance{
		"w": weekly(2 * 24 * time.Hour),
		"e": {{Used: 0, Span: five}}, // not started
	}
	allowances = func(string) map[string]provider.Allowance { return share }
	acct := func(user string) candidate {
		return candidate{p: provider.Provider{Account: &provider.Account{Agent: "x", User: user}}, model: "m", rest: "s#" + user}
	}
	p := provider.Provider{ID: "s", Account: &provider.Account{Agent: "x"}}
	if got := restsOf(route(p, []candidate{acct("w"), acct("e")}, "m", provider.Chat)); got != "s#e s#w " {
		t.Fatalf("five hours not started, ahead of a week in two days: %s", got)
	}
	share["e"] = provider.Allowance{{Used: 40, Resets: now.Add(3 * time.Hour), Span: five}}
	if got := restsOf(route(p, []candidate{acct("w"), acct("e")}, "m", provider.Chat)); got != "s#e s#w " {
		t.Fatalf("five hours started, ahead of a week in two days: %s", got)
	}
	share["w"] = weekly(time.Hour)
	if got := restsOf(route(p, []candidate{acct("e"), acct("w")}, "m", provider.Chat)); got != "s#w s#e " {
		t.Fatalf("a week renewing within the hour first: %s", got)
	}
}

// A relay's 429 that names the model whose credentials cooled down is
// that model's rest: its siblings on the same key still answer upstream,
// and none of them is told the model's sentence (#3039, Welkon's relay:
// "All credentials for model model-a are cooling down via provider
// upstream", Retry-After an hour). Resting the key benched every sibling
// for that hour without one being asked. Read as a rate limit or out of
// credit, the model's ladder and sink are its own too.
func TestModelCoolingDownRestsTheModelOnly(t *testing.T) {
	old := allowances
	defer func() { allowances = old }()
	fresh(t)
	forgetSinks(t)
	allowances = func(string) map[string]provider.Allowance {
		return map[string]provider.Allowance{}
	}
	key := func(model string) candidate {
		return candidate{p: provider.Provider{ID: "relay", Key: "sk-relay"}, model: model, rest: "relay"}
	}
	s := &Server{}
	header := http.Header{}
	header.Set("Retry-After", "3597")
	for _, tc := range []struct {
		name, why string
		body      string
		noWait    bool // sent with no Retry-After: on the credit ladder
	}{
		// the report's own bytes
		{"quota", failQuota, `{"error":{"message":"All credentials for model model-a are cooling down via provider upstream (last error: usage_limit_reached: The usage limit has been reached)"}}`, false},
		// CLIProxyAPI's modelCooldownError as it marshals it, with no
		// last error: a rate limit
		{"rate", failRate, `{"error":{"code":"model_cooldown","message":"All credentials for model model-a are cooling down via provider codex","model":"model-a","provider":"codex","reset_seconds":3597,"reset_time":"59m57s"}}`, false},
		// OmniRoute's, naming no model: the requested one
		{"requested", failRate, `{"error":{"message":"All credentials for the requested model are cooling down","type":"rate_limit_error","code":"model_cooldown","reset_seconds":3597}}`, false},
		{"credit", failCredit, `{"error":{"message":"All credentials for model model-a are cooling down via provider upstream (last error: insufficient balance)"}}`, true},
	} {
		forgetSinks(t)
		a := key("model-a")
		// the key's own run of rests of this kind, just ended: the
		// model's rest climbs its own ladder, not the key's
		restingUntil.Lock()
		restingUntil.m[a.restKey()] = time.Now().Add(-time.Second)
		restingUntil.note[a.restKey()] = Rest{Why: tc.why, Failures: 3}
		restingUntil.Unlock()
		h := header
		if tc.noWait {
			h = http.Header{}
		}
		r := s.restAfter(a, 429, h, []byte(tc.body))
		if r.Why != tc.why {
			t.Fatalf("%s: read as %q, want %q", tc.name, r.Why, tc.why)
		}
		if r.Key != a.restID() || r.Model != a.model {
			t.Fatalf("%s: model-a's cooling-down rested %+v (model %q), not the model alone", tc.name, r, r.Model)
		}
		if _, ok := restOf(key("model-b").restID()); ok {
			t.Fatalf("%s: model-a cooling down rested model-b", tc.name)
		}
		if _, ok := restOf(key("model-b").restKey()); ok {
			t.Fatalf("%s: model-a cooling down rested the whole key", tc.name)
		}
		if _, ok := sunk.m[a.restKey()]; ok {
			t.Fatalf("%s: model-a cooling down sank the whole key", tc.name)
		}
		if (tc.why == failRate || tc.why == failCredit) && r.Failures != 1 {
			t.Fatalf("%s: model-a's rest is rung %d of the key's ladder, want the model's first", tc.name, r.Failures)
		}
		if tc.why == failRate {
			if _, ok := sunk.m[a.restID()]; !ok {
				t.Fatalf("%s: model-a, rate limited with quota left, didn't sink", tc.name)
			}
		}
		if tc.why == failQuota {
			// the model's own sentence is the model's rest's to keep
			if kept, _ := restOf(a.restID()); !bytes.Contains([]byte(kept.said), []byte("model-a")) {
				t.Fatalf("%s: model-a's rest keeps %q", tc.name, kept.said)
			}
		}
	}

	// an account-level cool-down that names no model rests the key: the
	// model-scoped branch is for wordings that name one
	forgetSinks(t)
	whole := key("model-b")
	if r := s.restAfter(whole, 429, header, []byte(`{"error":{"message":"All credentials are cooling down via provider upstream"}}`)); r.Key != whole.restKey() {
		t.Fatalf("an account-level cool-down rested %+v, want the key", r)
	}
}
