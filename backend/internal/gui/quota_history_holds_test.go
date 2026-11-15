package gui

import (
	"encoding/base64"
	"encoding/json"
	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestQuotaHistoryHoldsRequiresCompletedRange(t *testing.T) {
	sandboxHome(t)
	now := holdUsageClock(t, time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC))
	mux := http.NewServeMux()
	accountAllowanceRoutes(mux)
	base := "/api/accounts/quotas/history/holds?provider=codex&user=sample%40example.com"
	for _, query := range []string{"", "&from=bad&to=bad", "&from=2026-10-08T00:00:00Z&to=2026-10-07T00:00:00Z", "&from=2026-10-08T00:00:00Z&to=2026-10-15T00:00:00Z"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", base+query, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("invalid range %q: %d %s", query, w.Code, w.Body)
		}
	}
	w := httptest.NewRecorder()
	path := base + "&from=" + now.Add(-7*24*time.Hour).Format(time.RFC3339) + "&to=" + now.Add(-time.Hour).Format(time.RFC3339)
	mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	var cards []provider.SubscriptionQuota
	if err := json.Unmarshal(w.Body.Bytes(), &cards); w.Code != 200 || err != nil || len(cards) != 0 {
		t.Fatalf("unknown history: %d %s (%v)", w.Code, w.Body, err)
	}
}

func TestQuotaHistoryHoldsReadsLocalSnapshot(t *testing.T) {
	home := sandboxHome(t)
	now := holdUsageClock(t, time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC))
	user := "history-api@example.com"
	jwt := func(claims any) string {
		b, _ := json.Marshal(claims)
		return "e30." + base64.RawURLEncoding.EncodeToString(b) + ".sig"
	}
	write := func(path string, data any) {
		t.Helper()
		b, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(home, ".codex", "auth.json"), map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
		"id_token":     jwt(map[string]any{"email": user, "https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "plus", "chatgpt_account_id": "history-api"}}),
		"access_token": jwt(map[string]any{"exp": now.Add(24 * time.Hour).Unix()}), "refresh_token": "fake-refresh", "account_id": "history-api",
	}})
	future := now.Add(3 * 24 * time.Hour)
	write(filepath.Join(filepath.Dir(provider.Path()), "quotas.json"), map[string]any{"codex/" + user: map[string]any{"at": now, "quota": provider.SubscriptionQuota{Provider: "codex", User: user, ReadAt: &now, Windows: []provider.QuotaWindow{{Name: "7 days", Used: 1, ResetsAt: &future}}}, "spans": []map[string]any{{"span": int64(7 * 24 * time.Hour)}}}})
	provider.ForgetAccounts()
	t.Cleanup(provider.ForgetAccounts)
	provider.ForgetKeptCardsForTest()
	t.Cleanup(provider.ForgetKeptCardsForTest)
	from, to := now.Add(-8*24*time.Hour), now.Add(-24*time.Hour)
	at := to.Add(-time.Hour)
	write(filepath.Join(filepath.Dir(provider.Path()), "quota-history.json"), map[string]map[string][]provider.QuotaPoint{"codex|" + user: {"7 days": {{At: at, Left: 60, Start: &from, ResetsAt: &to}}}})
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	write(catalog.CachePath(), map[string]any{"openai": map[string]any{"id": "openai", "models": map[string]any{"gpt-5.5": map[string]any{"id": "gpt-5.5", "cost": map[string]any{"input": 1.25, "output": 10}}}}})
	for _, r := range []usage.Record{{Time: from.Add(-time.Hour), Input: 1}, {Time: at.Add(-time.Hour), Input: 1000, Output: 200}} {
		r.Provider = "codex"
		r.ProviderAccount = user
		r.Model = "gpt-5.5"
		r.Status = 200
		usage.Append(r)
	}
	var requests atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(503) }))
	t.Cleanup(fake.Close)
	old := provider.CodexBase
	provider.CodexBase = fake.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = old })
	mux := http.NewServeMux()
	accountAllowanceRoutes(mux)
	w := httptest.NewRecorder()
	path := "/api/accounts/quotas/history/holds?provider=codex&user=" + user + "&from=" + from.Format(time.RFC3339) + "&to=" + to.Format(time.RFC3339)
	mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	var cards []provider.SubscriptionQuota
	if err := json.Unmarshal(w.Body.Bytes(), &cards); w.Code != 200 || err != nil || len(cards) != 1 {
		t.Fatalf("historical API: %d %s (%v)", w.Code, w.Body, err)
	}
	h := cards[0].Windows[0].Holds
	if h == nil || h.Tokens != 3000 || h.Used != 40 {
		t.Fatalf("historical estimate: %+v, want 3000 at 40%%", h)
	}
	if n := requests.Load(); n != 0 {
		t.Fatalf("historical selection polled the vendor %d times", n)
	}
}
