package supplier

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/contract"
)

func TestBalanceParsersPreserveScopeUnitsAndZero(t *testing.T) {
	for _, test := range []struct {
		name, adapter, body, scope string
		amount                     float64
		unlimited                  bool
	}{
		{"zero_token", "newapi_token", `{"success":true,"data":{"total_available":0,"total_used":500000,"total_granted":500000,"unlimited_quota":false}}`, "密钥额度", 0, false},
		{"unlimited_token", "newapi_token", `{"data":{"total_used":0,"unlimited_quota":true}}`, "密钥额度", 0, true},
		{"account", "newapi_account", `{"data":{"quota":500000,"used_quota":250000}}`, "账户余额", 1, false},
		{"sub_quota", "sub2api", `{"isValid":true,"quota":{"remaining":0,"used":3,"limit":3}}`, "密钥额度", 0, false},
		{"wallet", "sub2api", `{"isValid":true,"balance":3}`, "账户余额", 3, false},
		{"subscription", "sub2api", `{"is_active":true,"remaining":-1,"subscription":{"daily_usage_usd":2,"daily_limit_usd":5,"weekly_usage_usd":9,"weekly_limit_usd":10}}`, "订阅额度", 1, false},
		{"unlimited_subscription", "sub2api", `{"remaining":-1,"subscription":{}}`, "订阅额度", 0, true},
		{"rate_window", "sub2api", `{"mode":"quota_limited","rate_limits":[{"window":"5h","used":2,"limit":4}]}`, "周期额度", 2, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			q := autoQuery(test.adapter)
			q.UserID = ptr("7")
			r, err := parseAdapter(fixture(t, test.body), q)
			if err != nil {
				t.Fatal(err)
			}
			if r.Scope != test.scope || r.Unlimited != test.unlimited || test.unlimited && r.Amount != nil || !test.unlimited && (r.Amount == nil || *r.Amount != test.amount) {
				t.Fatalf("unexpected balance: %+v", r)
			}
		})
	}
}
func TestBalanceRejectsInvalidNumbersCustomPathsAndAccountIdentity(t *testing.T) {
	q := contract.BalanceQuery{Adapter: "custom", Path: "/credits", JSONPath: "data.0.amount", Unit: "USD", Divisor: 2}
	r, err := parseAdapter(fixture(t, `{"data":[{"amount":"4"}]}`), q)
	if err != nil || r.Amount == nil || *r.Amount != 2 {
		t.Fatalf("custom path: %+v %v", r, err)
	}
	for _, body := range []string{`{"data":[{"amount":"NaN"}]}`, `{"data":[{"amount":"Inf"}]}`, `{"data":[{"amount":1e999}]}`, `{"success":false,"data":[{"amount":4}]}`} {
		if _, err := parseAdapter(fixture(t, body), q); err == nil {
			t.Fatal("invalid balance accepted")
		}
	}
	for _, path := range []string{"//other.test/credits", "https://other.test/credits", "/x?key=secret", "/x\\evil"} {
		bad := q
		bad.Path = path
		if validateBalance(bad) == nil {
			t.Fatal("cross-origin/query path accepted")
		}
	}
	account := autoQuery("newapi_account")
	account.UserID = ptr("0")
	if validateBalance(account) == nil {
		t.Fatal("invalid account identity")
	}
}
func TestAutoBalanceUsesAnonymousStatusTrailingSlashAndCache(t *testing.T) {
	var log requestLog
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.add(r.URL.Path)
		if r.URL.Path == "/gateway/api/status" {
			if r.Header.Get("Authorization") != "" || r.Header.Get("x-api-key") != "" || r.Header.Get("New-Api-User") != "" {
				t.Error("public status received credentials")
			}
			fmt.Fprint(w, `{"success":true,"data":{"quota_per_unit":100,"quota_display_type":"CNY","usd_exchange_rate":7}}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-provider-key" {
			t.Error("wrong provider auth")
		}
		if r.URL.Path == "/gateway/api/usage/token/" {
			fmt.Fprint(w, `{"success":true,"data":{"object":"token_usage","total_available":200,"total_used":100,"total_granted":300,"unlimited_quota":false}}`)
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()
	service := New()
	c := connection(server.URL + "/gateway/v1")
	for i := 0; i < 2; i++ {
		r, err := service.AutoBalance(context.Background(), c)
		if err != nil {
			t.Fatal(err)
		}
		if r.Amount == nil || *r.Amount != 14 || r.Unit != "CNY" || r.Scope != "密钥额度" {
			t.Fatalf("currency/scope regression: %+v", r)
		}
	}
	expected := []string{"/gateway/v1/usage", "/gateway/api/usage/token/", "/gateway/api/status", "/gateway/v1/dashboard/billing/subscription", "/gateway/api/usage/token/", "/gateway/api/status"}
	paths := log.values()
	if !reflect.DeepEqual(paths, expected) {
		t.Fatalf("cache or prefix regression: %v", paths)
	}
}
func TestBalanceCacheBindsQueryAndClearsChangedEndpoint(t *testing.T) {
	var phase atomic.Int32
	var log requestLog
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.add(r.URL.Path)
		if phase.Load() == 0 && r.URL.Path == "/v1/usage" {
			fmt.Fprint(w, `{"mode":"unrestricted","remaining":8}`)
			return
		}
		if phase.Load() == 1 && r.URL.Path == "/v1/dashboard/billing/credit_grants" {
			fmt.Fprint(w, `{"total_available":4}`)
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()
	s := New()
	c := connection(server.URL)
	if _, err := s.AutoBalance(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	phase.Store(1)
	r, err := s.AutoBalance(context.Background(), c)
	if err != nil || r.Amount == nil || *r.Amount != 4 {
		t.Fatalf("stale strategy not invalidated: %+v %v", r, err)
	}
	paths := log.values()
	if len(paths) < 3 || paths[1] != "/v1/usage" || paths[2] != "/v1/usage" {
		t.Fatal("cached strategy was not reprobed")
	}
	other := c
	other.Query = ptr(autoQuery("newapi_token"))
	other.Query.SiteURL = ptr(server.URL + "/other")
	if cacheKey(c) == cacheKey(other) {
		t.Fatal("query settings omitted from cache identity")
	}
	other = c
	other.APIKey = "another-synthetic-key"
	if cacheKey(c) == cacheKey(other) {
		t.Fatal("credential omitted from cache identity")
	}
}
func TestAutoBalanceStopsOn429AndDoesNotUseKeyForAccount(t *testing.T) {
	for _, rateAt := range []string{"/v1/usage", "/api/status", "/v1/dashboard/billing/subscription"} {
		t.Run(rateAt, func(t *testing.T) {
			var log requestLog
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				log.add(r.URL.Path)
				if r.URL.Path == rateAt {
					w.WriteHeader(429)
					return
				}
				if r.URL.Path == "/api/usage/token/" {
					fmt.Fprint(w, `{"data":{"object":"token_usage","total_available":0,"total_used":0,"total_granted":0,"unlimited_quota":false}}`)
					return
				}
				w.WriteHeader(404)
			}))
			defer server.Close()
			_, err := New().AutoBalance(context.Background(), connection(server.URL))
			paths := log.values()
			if err == nil || err.Status != 429 || paths[len(paths)-1] != rateAt {
				t.Fatalf("continued after 429: %v %v", paths, err)
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/user/self" {
			if r.Header.Get("Authorization") != "Bearer synthetic-console-token" || r.Header.Get("New-Api-User") != "7" {
				t.Error("wrong account identity")
			}
			fmt.Fprint(w, `{"data":{"quota":100,"used_quota":0}}`)
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()
	c := connection(server.URL)
	q := autoQuery("newapi_account")
	q.UserID = ptr("7")
	c.Query = &q
	c.AccountToken = ptr("synthetic-console-token")
	r, err := New().AutoBalance(context.Background(), c)
	if err != nil || r.Scope != "账户余额" {
		t.Fatalf("account query failed: %+v %v", r, err)
	}
	c.AccountToken = nil
	if _, err := New().QueryBalance(context.Background(), c); err == nil || !strings.Contains(err.Message, "Access Token") {
		t.Fatal("API key substituted for console token")
	}
}
func TestBillingAndTokenCurrenciesRemainDistinct(t *testing.T) {
	sub := fixture(t, `{"object":"billing_subscription","hard_limit_usd":10}`)
	usage := fixture(t, `{"total_usage":125}`)
	r, err := parseBilling(sub, usage, nil, nil)
	if err != nil || *r.Amount != 8.75 || *r.Used != 1.25 {
		t.Fatalf("billing cents regression: %+v %v", r, err)
	}
	token := fixture(t, `{"data":{"total_available":10,"total_used":1,"total_granted":11,"unlimited_quota":false}}`)
	status := fixture(t, `{"success":true,"data":{"quota_display_type":"TOKENS"}}`)
	r, err = tokenResult(token, status, nil)
	if err != nil || r.Unit != "tokens" || *r.Amount != 10 {
		t.Fatal("raw token quota converted as currency")
	}
}

func TestReverseProxyEncodedPrefixIsNotDecodedIntoNewPathSegments(t *testing.T) {
	site, err := balanceSite("https://example.test/team%2Ftenant/v1", autoQuery("auto"))
	if err != nil || site.String() != "https://example.test/team%2Ftenant/" {
		t.Fatalf("encoded prefix changed: %v %v", site, err)
	}
	u, err := balanceURL(site, "api/usage/token/")
	if err != nil || u.String() != "https://example.test/team%2Ftenant/api/usage/token/" {
		t.Fatalf("encoded endpoint changed: %v %v", u, err)
	}
}
