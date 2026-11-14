package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agentenv"
)

// larchsis on X: two GLM Coding Plan keys in one provider, and only one
// card on the Usage page; after a restart both showed at first, and the
// automatic refresh took the second away again. Zhipu's /api/monitor
// answers a rate limit 200 in the platform's envelope, in Chinese
// ("并发数过高"), which wasn't taken for a passing failure, and a key on a
// provider whose OpenAI URL is still pay as you go (one with several keys
// is never moved to the plan's, LearnZhipuCoding) lost its card to any
// failed read. A key read before is a plan's: a read that fails keeps its
// card, as last read when the failure passes, else with why.
func TestZhipuSecondKeyKeepsItsCard(t *testing.T) {
	isolate(t)
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	t.Setenv("PATH", h)
	for _, v := range agentenv.Vars {
		t.Setenv(v, "")
	}
	restart := func() {
		lastQuotas.Lock()
		lastQuotas.m, lastQuotas.loaded = nil, false
		lastQuotas.Unlock()
		forgetPlanQuotas()
	}
	restart()
	t.Cleanup(restart)

	// what the second key's quota read answers: "" its plan
	var second atomic.Value
	second.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-Host")+r.URL.Path != "open.bigmodel.cn/api/monitor/usage/quota/limit" {
			w.WriteHeader(http.StatusNotFound) // the term and resets: not known
			return
		}
		if r.URL.RawQuery != "" { // a team's: none
			w.Write([]byte(`{"code":500,"msg":"no team","success":false}`))
			return
		}
		if said := second.Load().(string); said != "" && r.Header.Get("Authorization") == "glm-b.secondkey" {
			w.Write([]byte(said))
			return
		}
		w.Write([]byte(`{"code":200,"msg":"操作成功","success":true,"data":{"level":"pro","limits":[
			{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":12,"nextResetTime":4102444800000},
			{"type":"TOKENS_LIMIT","unit":6,"number":1,"percentage":40,"nextResetTime":4102444800000}]}}`))
	}))
	defer srv.Close()
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = rewrite{srv}
	t.Cleanup(func() { http.DefaultClient.Transport = old })

	if err := Save(Provider{ID: "zhipu", Name: "Zhipu GLM", Preset: "zhipu", Key: "glm-a.firstkey", KeyName: "one",
		Keys: []KeyAccount{{Name: "two", Key: "glm-b.secondkey"}},
		Chat: "https://open.bigmodel.cn/api/paas/v4", Anthropic: "https://open.bigmodel.cn/api/anthropic"}); err != nil {
		t.Fatal(err)
	}
	read := func() map[string]SubscriptionQuota {
		forgetPlanQuotas()
		out := map[string]SubscriptionQuota{}
		for _, q := range PlanQuotas(context.Background()) {
			out[q.User] = q
		}
		return out
	}
	cards := read()
	for _, u := range []string{"one", "two"} {
		if q := cards[u]; len(q.Windows) != 2 || q.Error != "" || q.AsOf != nil {
			t.Fatalf("first read, key %s: %+v", u, q)
		}
	}

	// rate limited, as the envelope says it
	second.Store(`{"code":1302,"msg":"您当前使用该API的并发数过高，请降低并发，或联系客服增加限额。","success":false}`)
	lastQuotas.Lock()
	lastQuotas.m, lastQuotas.loaded = nil, false // from disk, as after a restart
	lastQuotas.Unlock()
	cards = read()
	if q := cards["one"]; len(q.Windows) != 2 || q.AsOf != nil {
		t.Fatalf("first key while the second is limited: %+v", q)
	}
	if q, ok := cards["two"]; !ok || len(q.Windows) != 2 || q.Error != "" || q.AsOf == nil || q.Windows[1].Used != 40 {
		t.Fatalf("second key rate limited: kept card wanted, got %v %+v", ok, q)
	}

	// a failure that says nothing known: the card stays, saying why
	second.Store(`{"code":500,"msg":"系统繁忙","success":false}`)
	cards = read()
	if q, ok := cards["two"]; !ok || q.Error == "" {
		t.Fatalf("second key failing: a card with its error wanted, got %v %+v", ok, q)
	}

	// a key never read that fails still has no card: nothing says it's a plan's
	if err := Save(Provider{ID: "zhipu-payg", Name: "Pay as you go", Preset: "zhipu", Key: "glm-b.secondkey",
		Chat: "https://open.bigmodel.cn/api/paas/v4"}); err != nil {
		t.Fatal(err)
	}
	forgetPlanQuotas()
	for _, q := range PlanQuotas(context.Background()) {
		if q.Provider == "zhipu-payg" {
			t.Fatalf("a key never read has a card: %+v", q)
		}
	}
}
