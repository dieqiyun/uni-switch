package provider

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ollamaProReply is /api/balance for a plan with credits bought on top,
// as #1888 gives it.
const ollamaProReply = `{
  "included": {
    "balance_usd": 72.5,
    "allowance_usd": 100,
    "period": {
      "from": "2026-09-15T09:30:00Z",
      "until": "2026-10-15T09:30:00Z"
    }
  },
  "purchased": {
    "balance_usd": 25.0
  }
}`

// The included allowance is a month's window of dollars spent, renewing
// at the period's end; the bought credits are the balance.
func TestReadOllamaBalance(t *testing.T) {
	_, ws, err := readOllamaPlan([]byte(ollamaProReply))
	if err != nil || len(ws) != 1 {
		t.Fatalf("%v %+v", err, ws)
	}
	w := ws[0]
	until := time.Date(2026, 10, 15, 9, 30, 0, 0, time.UTC)
	if w.Name != "Month" || w.Amount != 27.5 || w.Limit != 100 || w.Unit != "USD" || math.Abs(w.Used-27.5) > 1e-9 ||
		w.Span != 30*24*time.Hour || w.ResetsAt == nil || !w.ResetsAt.Equal(until) {
		t.Errorf("allowance: %+v", w)
	}
	if !w.Aside {
		t.Error("a spent allowance with credits bought stops the key")
	}
	if got, err := readOllamaCredits([]byte(ollamaProReply)); err != nil || got != "$25.00" {
		t.Errorf("credits: %q %v", got, err)
	}
}

// What a reply leaves out is unknown, never $0 or used up.
func TestReadOllamaBalanceMissing(t *testing.T) {
	window := func(body string) []QuotaWindow {
		t.Helper()
		_, ws, err := readOllamaPlan([]byte(body))
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		return ws
	}
	// no credits ever bought: the allowance alone, which stops the key
	// when spent, and no balance card
	noBought := `{"included":{"balance_usd":0,"allowance_usd":20,"period":{"from":"2026-10-01T00:00:00Z","until":"2026-11-01T00:00:00Z"}}}`
	if ws := window(noBought); len(ws) != 1 || ws[0].Used != 100 || ws[0].Aside || ws[0].Name != "Month" {
		t.Errorf("no purchased: %+v", ws)
	}
	if got, err := readOllamaCredits([]byte(noBought)); !errors.Is(err, errNoBalance) {
		t.Errorf("no purchased reads as %q %v", got, err)
	}
	// credits bought and spent: $0, said, and the allowance not aside
	if ws := window(`{"included":{"balance_usd":5,"allowance_usd":20,"period":null},"purchased":{"balance_usd":0}}`); len(ws) != 1 || ws[0].Aside {
		t.Errorf("credits spent: %+v", ws)
	}
	if got, err := readOllamaCredits([]byte(`{"included":null,"purchased":{"balance_usd":0}}`)); err != nil || got != "$0.00" {
		t.Errorf("$0 bought: %q %v", got, err)
	}
	// no period: a window with no reset and no span
	if ws := window(`{"included":{"balance_usd":"15","allowance_usd":"20","period":null}}`); len(ws) != 1 || ws[0].ResetsAt != nil ||
		ws[0].Span != 0 || ws[0].Name != "Allowance" || ws[0].Amount != 5 {
		t.Errorf("null period: %+v", ws)
	}
	// a free plan: no allowance, or none of it told, is no window
	for _, body := range []string{
		`{"included":null,"purchased":{"balance_usd":0}}`,
		`{"included":{"balance_usd":0,"allowance_usd":0,"period":null}}`,
		`{"included":{"allowance_usd":20,"period":null}}`,
		`{"included":{"balance_usd":3}}`,
		`{"purchased":{"balance_usd":4}}`,
		`{}`,
	} {
		if ws := window(body); len(ws) != 0 {
			t.Errorf("%s: %+v", body, ws)
		}
	}
	// a reply of neither: said, with its shape, not hidden as no balance
	if _, err := readOllamaCredits([]byte(`{"error":"x","balance":1}`)); err == nil || errors.Is(err, errNoBalance) || !strings.Contains(err.Error(), "balance") {
		t.Errorf("another shape: %v", err)
	}
	if _, _, err := readOllamaPlan([]byte(`not json`)); err == nil {
		t.Error("a reply that isn't JSON read as no plan")
	}
}

// An Ollama Cloud key's allowance is a plan card and its bought credits a
// balance card, on the Usage page and in uni-switch quota; a key that bought
// none has no balance card, and a key Ollama refuses says so.
func TestOllamaCloudCards(t *testing.T) {
	signIn(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/balance" || r.Method != http.MethodGet {
			t.Errorf("asked %s %s", r.Method, r.URL.Path)
		}
		switch r.Header.Get("Authorization") {
		case "Bearer ok-pro":
			w.Write([]byte(ollamaProReply))
		case "Bearer ok-legacy":
			w.Write([]byte(ollamaLegacyReply))
		case "Bearer ok-plain":
			w.Write([]byte(`{"included":{"balance_usd":19,"allowance_usd":20,"period":{"from":"2026-10-01T00:00:00Z","until":"2026-11-01T00:00:00Z"}}}`))
		default:
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"invalid credentials"}`)) // as ollama.com answers a bad key
		}
	}))
	defer srv.Close()
	old := ollamaBalanceURL
	ollamaBalanceURL = srv.URL + "/api/balance"
	t.Cleanup(func() { ollamaBalanceURL = old })
	if err := Save(Provider{ID: "ollama-cloud", Name: "Ollama Cloud", Icon: "ollama", Chat: "https://ollama.com/v1", Anthropic: "https://ollama.com",
		Key: "ok-pro", KeyName: "pro", Keys: []KeyAccount{{Name: "plain", Key: "ok-plain"}, {Name: "legacy", Key: "ok-legacy"}, {Name: "bad", Key: "ok-bad"}}}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	plans := map[string]SubscriptionQuota{}
	for _, q := range PlanQuotas(ctx) {
		if q.Provider == "ollama-cloud" {
			plans[q.User] = q
		}
	}
	if q := plans["pro"]; len(q.Windows) != 1 || q.Windows[0].Name != "Month" || q.Windows[0].Amount != 27.5 || q.Error != "" {
		t.Errorf("pro's plan: %+v", q)
	}
	if q := plans["plain"]; len(q.Windows) != 1 || q.Windows[0].Amount != 1 || q.Windows[0].Aside {
		t.Errorf("plain's plan: %+v", q)
	}
	if q := plans["legacy"]; len(q.Windows) != 2 || q.Windows[0].Name != "Session limit" || q.Windows[0].Used != 25 || q.Windows[1].Used != 60 || q.Error != "" {
		t.Errorf("legacy's plan: %+v", q)
	}
	if _, ok := plans["bad"]; ok || len(plans) != 3 {
		t.Errorf("plan cards: %+v", plans)
	}
	var lines []string
	for _, q := range KeyBalances(ctx) {
		if q.Provider == "ollama-cloud" {
			lines = append(lines, q.User+"|"+q.Balance+"|"+q.Error)
		}
	}
	want := `pro|$25.00|,legacy|$25.00|,bad||401 Unauthorized: {"error":"invalid credentials"}`
	if strings.Join(lines, ",") != want {
		t.Errorf("balance cards:\n%s\nwant\n%s", strings.Join(lines, ","), want)
	}
}

// ollamaLegacyReply is /api/balance for a legacy (early) plan, as
// yangmingyuan380 posted it on #1888, byte for byte.
const ollamaLegacyReply = `{
  "included": {
    "session": {
      "remaining_percent": 75,
      "resets_at": "2026-10-01T07:00:00Z"
    },
    "weekly": {
      "remaining_percent": 40,
      "resets_at": "2026-10-05T00:00:00Z"
    }
  },
  "purchased": {
    "balance_usd": 25.0
  }
}`

// A legacy plan's session and weekly limits are its plan card's windows,
// used = 100 - remaining_percent, each resetting at its resets_at; the
// bought credits are still the balance card.
func TestReadOllamaLegacyPlan(t *testing.T) {
	_, ws, err := readOllamaPlan([]byte(ollamaLegacyReply))
	if err != nil || len(ws) != 2 {
		t.Fatalf("%v %+v", err, ws)
	}
	session := time.Date(2026, 10, 1, 7, 0, 0, 0, time.UTC)
	week := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	if w := ws[0]; w.Name != "Session limit" || w.Used != 25 || w.Span != 0 || w.Unit != "" || w.ResetsAt == nil || !w.ResetsAt.Equal(session) || !w.Aside {
		t.Errorf("session: %+v", w)
	}
	if w := ws[1]; w.Name != "Weekly" || w.Used != 60 || w.Span != 7*24*time.Hour || w.ResetsAt == nil || !w.ResetsAt.Equal(week) || !w.Aside {
		t.Errorf("weekly: %+v", w)
	}
	if got, err := readOllamaCredits([]byte(ollamaLegacyReply)); err != nil || got != "$25.00" {
		t.Errorf("credits: %q %v", got, err)
	}
	// without credits the limits stop the key; one that doesn't say what
	// is left is no window, not used up
	_, ws, err = readOllamaPlan([]byte(`{"included":{"session":{"remaining_percent":0,"resets_at":"x"},"weekly":{"resets_at":"2026-10-05T00:00:00Z"}}}`))
	if err != nil || len(ws) != 1 || ws[0].Used != 100 || ws[0].Aside || ws[0].ResetsAt != nil {
		t.Errorf("no credits: %v %+v", err, ws)
	}
}
