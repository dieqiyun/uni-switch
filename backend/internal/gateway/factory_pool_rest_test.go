package gateway

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// factoryStandardOut is Factory's refusal for Standard's week used up, as
// #1979 quotes it, with the 402 it comes with.
const factoryStandardOut = `{"error":{"message":"Factory: You've reached your weekly standard usage limit (resets in 0h 19min). Switch to Droid Core or enable Extra Usage to continue."}}`

// Factory's Standard pool used up is Standard's: the account's Droid Core
// models still answer (#1979, Ziqi-Cui: a group of claude-opus-5-5 then
// glm-5.3 on two Factory accounts fell back to GLM a few times, then every
// request was Opus's 402, both accounts resting 30 minutes "for credit").
// The allowance is as the built-in reads /api/billing/limits and as the
// plugin hands its windows on (Standard's notModels, Core's models), so a
// moved Factory is held to the same.
func TestFactoryStandardOutLeavesCore(t *testing.T) {
	now := time.Now()
	soon := now.Add(19 * time.Minute).UTC().Format(time.RFC3339)
	later := now.Add(4 * 24 * time.Hour).UTC().Format(time.RFC3339)
	limits := func(std, core int) []byte {
		return []byte(`{"limits":{"standard":{"fiveHour":{"usedPercent":` + strconv.Itoa(std) + `,"windowEnd":"` + soon + `"},"weekly":{"usedPercent":` + strconv.Itoa(std) + `,"windowEnd":"` + soon + `"}},` +
			`"core":{"fiveHour":{"usedPercent":` + strconv.Itoa(core) + `,"windowEnd":"` + later + `"},"weekly":{"usedPercent":` + strconv.Itoa(core) + `,"windowEnd":"` + later + `"}}},"extraUsageAllowed":false,"extraUsageBalanceCents":0}`)
	}
	// the factory plugin's limitWindows on limits(100, 1), as it answers
	// usage (packages/factory/index.mjs, 0.1.x), its windows' ends moved
	// to now
	core := `["glm-5.3","glm-5.3-flash","glm-5.2","kimi-k3","deepseek-v4.1-flash","qwen3.8-max","minimax-m3","minimax-m2.7","mistral-large-4","mistral-medium-3.5","nemotron-3-ultra"]`
	pluginUsage := func(std, coreUsed int) []byte {
		return []byte(`{"windows":[` +
			`{"name":"5 hours","used":` + strconv.Itoa(std) + `,"span":18000,"resetsAt":"` + soon + `","notModels":` + core + `},` +
			`{"name":"7 days","used":` + strconv.Itoa(std) + `,"span":604800,"resetsAt":"` + soon + `","notModels":` + core + `},` +
			`{"name":"Droid Core · 5 hours","used":` + strconv.Itoa(coreUsed) + `,"span":18000,"resetsAt":"` + later + `","models":` + core + `},` +
			`{"name":"Droid Core · 7 days","used":` + strconv.Itoa(coreUsed) + `,"span":604800,"resetsAt":"` + later + `","models":` + core + `}]}`)
	}

	for _, way := range []struct {
		name  string
		agent string
		of    func(std, core int) provider.Allowance
	}{
		{"built-in", "factory", func(std, core int) provider.Allowance {
			return provider.FactoryAllowanceForTest(limits(std, core), now)
		}},
		{"plugin", "plugin:factory", func(std, core int) provider.Allowance {
			return provider.PluginAllowanceForTest(pluginUsage(std, core), now)
		}},
	} {
		t.Run(way.name, func(t *testing.T) {
			old, oldAwait := allowances, awaitAllowance
			defer func() { allowances, awaitAllowance = old, oldAwait }()
			defer narrowing.Wait()
			fresh(t)
			al := map[string]provider.Allowance{"a@x.com": way.of(100, 1), "b@x.com": way.of(100, 1)}
			allowances = func(string) map[string]provider.Allowance { return al }
			acct := func(user, model string) candidate {
				return candidate{p: provider.Provider{ID: "factory", Account: &provider.Account{Agent: way.agent, User: user}}, model: model, rest: "factory"}
			}
			s := &Server{}

			// both accounts' Opus refused for Standard, as in the report
			for _, u := range []string{"a@x.com", "b@x.com"} {
				opus := acct(u, "claude-opus-5-5")
				r := s.restAfter(opus, 402, http.Header{}, []byte(factoryStandardOut))
				if r.Why != failQuota || r.Key != opus.restID() {
					t.Fatalf("%s: Standard used up rests %+v, want Opus alone out of quota", u, r)
				}
				if d := time.Until(r.Until); d > 20*time.Minute {
					t.Fatalf("%s: Opus rests %v, past Standard's window", u, d)
				}
				// Droid Core's models on the same account, and Standard's
				// own siblings told apart from them
				if _, ok := restingOf(acct(u, "glm-5.3")); ok {
					t.Fatalf("%s: Standard used up rests GLM-5.3, which Droid Core counts", u)
				}
				if _, ok := restingOf(acct(u, "kimi-k3")); ok {
					t.Fatalf("%s: Standard used up rests Kimi K3", u)
				}
			}
			// read again once Standard's window has renewed, Opus is back:
			// a window truly starting again tells both hooks, and lifts a
			// rest out of quota, the refusal's own time included, as one
			// for credit wasn't (Ziqi-Cui on #1979)
			renewed(way.agent, "a@x.com")
			restarted(way.agent, "a@x.com")
			if _, ok := restingOf(acct("a@x.com", "claude-opus-5-5")); ok {
				t.Fatal("Opus still rests after Standard's window renewed")
			}
			if _, ok := restingOf(acct("b@x.com", "claude-opus-5-5")); !ok {
				t.Fatal("one account's window renewed lifted the other's rest")
			}
			// the next requests still go to GLM: nothing rests it, and the
			// group's order doesn't spend it with Opus
			for i := 0; i < 3; i++ {
				if spentAfter([]candidate{acct("a@x.com", "glm-5.3"), acct("b@x.com", "glm-5.3")}) {
					t.Fatal("GLM-5.3 counted spent after Opus's Standard refusal")
				}
			}

			// Droid Core used up leaves Standard to the vendors' models
			fresh(t)
			al = map[string]provider.Allowance{"a@x.com": way.of(10, 100)}
			glm := acct("a@x.com", "glm-5.3")
			r := s.restAfter(glm, 402, http.Header{}, []byte(`{"error":{"message":"Factory: You've reached your weekly Droid Core usage limit (resets in 3d 4h)."}}`))
			if r.Why != failQuota || r.Key != glm.restID() {
				t.Fatalf("Core used up rests %+v, want GLM alone", r)
			}
			if _, ok := restingOf(acct("a@x.com", "claude-opus-5-5")); ok {
				t.Fatal("Core used up rests Opus, which Standard counts")
			}

			// an account out of money is still out whole
			fresh(t)
			al = map[string]provider.Allowance{"a@x.com": way.of(100, 1)}
			opus := acct("a@x.com", "claude-opus-5-5")
			r = s.restAfter(opus, 402, http.Header{}, []byte(`{"error":{"message":"Payment required: your subscription payment failed"}}`))
			if r.Why != failCredit || r.Key != opus.restKey() {
				t.Fatalf("a payment 402 rests %+v, want the account out of credit", r)
			}
			if _, ok := restingOf(acct("a@x.com", "glm-5.3")); !ok {
				t.Fatal("a payment 402 leaves the account's other models in")
			}

			// windows not read yet (a start, or a reading that failed):
			// the account rests, as nothing tells Standard's pool from
			// it, and once the reading lands the rest is Opus's alone
			// and GLM-5.3 routes again (#2036, Ziqi-Cui). The request
			// isn't kept waiting for the reading.
			fresh(t)
			var mu sync.Mutex
			read := map[string]provider.Allowance{}
			allowances = func(string) map[string]provider.Allowance {
				mu.Lock()
				defer mu.Unlock()
				return read
			}
			land := make(chan struct{})
			awaitAllowance = func(_ context.Context, agent, user string) bool {
				if agent != way.agent || user != "a@x.com" {
					t.Errorf("awaited %s %s's reading", agent, user)
				}
				<-land
				mu.Lock()
				defer mu.Unlock()
				read = map[string]provider.Allowance{user: way.of(100, 1)}
				return true
			}
			opus = acct("a@x.com", "claude-opus-5-5")
			r = s.restAfter(opus, 402, http.Header{}, []byte(factoryStandardOut))
			if r.Why != failQuota || r.Key != opus.restKey() {
				t.Fatalf("Standard used up, windows unread, rests %+v, want the account", r)
			}
			if _, ok := restingOf(acct("a@x.com", "glm-5.3")); !ok {
				t.Fatal("windows unread: GLM-5.3 routes before anything tells Standard's pool from the account")
			}
			close(land)
			narrowing.Wait()
			if _, ok := restingOf(acct("a@x.com", "glm-5.3")); ok {
				t.Fatal("GLM-5.3 still rests after the reading scoped Standard away from Droid Core")
			}
			if got, ok := restingOf(opus); !ok || got.Key != opus.restID() || got.Model != "claude-opus-5-5" || !got.Until.Equal(r.Until) {
				t.Fatalf("Opus after the reading: %+v %v, want its own rest until %v", got, ok, r.Until)
			}
			if _, ok := restingOf(acct("b@x.com", "glm-5.3")); ok {
				t.Fatal("another account rests")
			}
		})
	}
}

// A 402 is out of quota when it speaks of a usage limit and nothing of
// money, and out of credit otherwise.
func TestPaymentRequiredReadByItsWords(t *testing.T) {
	for _, c := range []struct {
		body, want string
	}{
		{factoryStandardOut, failQuota},
		{`{}`, failCredit},
		{`{"error":{"message":"Insufficient Balance"}}`, failCredit},
		{`{"error":{"message":"You have reached the limit.","reason":"MONTHLY_REQUEST_COUNT"}}`, failCredit},
		{`{"error":{"message":"usage limit reached, please top up your credit"}}`, failCredit},
		{`{"error":{"code":"insufficient_quota","message":"You exceeded your current quota"}}`, failCredit},
		{`Command Code: you have used this month's credits`, failCredit},
	} {
		if got := failure(402, []byte(c.body)); got != c.want {
			t.Errorf("402 %s: %s, want %s", c.body, got, c.want)
		}
	}
}

// A refusal that says how long until it is back in hours and minutes rests
// that long when the account's windows aren't known: Factory's "resets in
// 0h 19min" was a 30-minute credit rest (#1979).
func TestResetsInWords(t *testing.T) {
	for _, c := range []struct {
		body string
		want time.Duration
	}{
		{factoryStandardOut, 19 * time.Minute},
		{`{"error":{"message":"weekly limit reached (resets in 2d 3h)"}}`, 51 * time.Hour},
		{`{"error":{"message":"limit reached, resets in 45 minutes"}}`, 45 * time.Minute},
		{`{"error":{"message":"limit reached, resets in 5 messages"}}`, 0},
		{`{"error":{"message":"resets in 30d"}}`, 0}, // past longestWritten
		{`{"error":{"message":"usage limit reached"}}`, 0},
	} {
		if got := resetsAfter([]byte(c.body)); got != c.want {
			t.Errorf("%s: %v, want %v", c.body, got, c.want)
		}
	}

	old, oldAwait := allowances, awaitAllowance
	defer func() { allowances, awaitAllowance = old, oldAwait }()
	fresh(t)
	allowances = func(string) map[string]provider.Allowance { return nil }
	// and no reading lands: the account's rest stands (#2036)
	awaitAllowance = func(context.Context, string, string) bool { return false }
	c := candidate{p: provider.Provider{ID: "factory", Account: &provider.Account{Agent: "factory", User: "a@x.com"}}, model: "claude-opus-5-5", rest: "factory"}
	r := (&Server{}).restAfter(c, 402, http.Header{}, []byte(factoryStandardOut))
	if d := time.Until(r.Until); r.Why != failQuota || r.By != "resets" || d < 18*time.Minute || d > 19*time.Minute {
		t.Fatalf("Standard used up, windows unknown, rests %v: %+v", d, r)
	}
	narrowing.Wait()
	if got, ok := restingOf(candidate{p: c.p, model: "glm-5.3", rest: "factory"}); !ok || got.Key != c.restKey() {
		t.Fatalf("no reading came: the account's rest is %+v %v, want it standing", got, ok)
	}
}
