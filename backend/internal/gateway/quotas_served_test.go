package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// GET /v1/uni-switch/quotas tells when each key last answered a request through
// the gateway, and marks the latest last (#570); one that never did has
// neither.
func TestQuotasLastServedAt(t *testing.T) {
	fresh(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/balance" {
			io.WriteString(w, `{"balance":"7"}`)
			return
		}
		servedBy(provider.Chat, "sol")(w, r)
	}))
	t.Cleanup(up.Close)
	for _, id := range []string{"qa", "qb"} {
		if err := provider.Save(provider.Provider{ID: id, Name: strings.ToUpper(id), Key: "k-" + id, Models: []string{"sol"},
			Chat: up.URL + "/v1", BalanceURL: up.URL + "/balance", BalancePath: "balance"}); err != nil {
			t.Fatal(err)
		}
	}
	provider.ForgetBalances()
	s := New()
	before := time.Now().Add(-time.Second)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"qa/sol","messages":[{"role":"user","content":"hi"}]}`)))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/uni-switch/quotas", nil)
	r.RemoteAddr = "127.0.0.1:5000"
	s.Handler().ServeHTTP(rec, r)
	var out struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err, rec.Body)
	}
	seen := 0
	for _, q := range out.Data {
		switch q["provider"] {
		case "qa":
			seen++
			s, _ := q["lastServedAt"].(string)
			at, err := time.Parse(time.RFC3339, s)
			if err != nil || at.Before(before) || q["last"] != true {
				t.Errorf("served key: %v (%v)", q, err)
			}
		case "qb":
			seen++
			if _, ok := q["lastServedAt"]; ok || q["last"] != nil {
				t.Errorf("a key that never answered: %v", q)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("quotas %s", rec.Body)
	}
}
