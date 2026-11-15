package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agentenv"
)

// A ClinePass API key's card has the 5-hour, weekly and monthly limits
// beside the credits, read with the key as AxonHub reads them (zRain on
// Discord); a key whose account has no ClinePass (usage-limits 404) has
// its credits alone, and a key Cline refuses says so.
func TestClineKeyUsage(t *testing.T) {
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
	keyBalanceCache.data = nil
	t.Cleanup(func() { keyBalanceCache.data = nil })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if key != "sk-pass" && key != "sk-free" && key != "sk-down" && key != "sk-denied" && key != "sk-nobal" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"success":false,"error":"invalid API key"}`))
			return
		}
		switch r.URL.Path {
		case "/api/v1/users/me":
			w.Write([]byte(`{"success":true,"data":{"id":"usr_` + key + `","email":"a@b.c"}}`))
		case "/api/v1/users/usr_sk-pass/balance":
			w.Write([]byte(`{"success":true,"data":{"balance":4250000,"userId":"usr_sk-pass"}}`))
		case "/api/v1/users/usr_sk-free/balance", "/api/v1/users/usr_sk-down/balance", "/api/v1/users/usr_sk-denied/balance":
			w.Write([]byte(`{"success":true,"data":{"balance":12000000}}`))
		case "/api/v1/users/usr_sk-nobal/balance":
			w.WriteHeader(http.StatusBadGateway)
		case "/api/v1/users/me/plan/usage-limits":
			switch key {
			case "sk-free":
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"success":false,"error":"no active plan"}`))
				return
			case "sk-down":
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte(`{"success":false,"error":"try again later"}`))
				return
			case "sk-denied":
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"success":false,"error":"forbidden"}`))
				return
			}
			w.Write([]byte(`{"success":true,"data":{"limits":[
				{"type":"monthly","percentUsed":12.5,"resetsAt":"2026-11-01T00:00:00Z"},
				{"type":"five_hour","percentUsed":40,"resetsAt":"2026-10-05T15:00:00Z"},
				{"type":"weekly","percentUsed":130,"resetsAt":"2026-10-10T00:00:00Z"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := clineUsageAPI
	clineUsageAPI = srv.URL + "/api/v1"
	t.Cleanup(func() { clineUsageAPI = old })

	if err := Save(Provider{ID: "clinepass", Name: "ClinePass", Preset: "clinepass", Chat: "https://api.cline.bot/api/v1",
		Key: "sk-pass", KeyName: "pass", Keys: []KeyAccount{{Name: "free", Key: "sk-free"}, {Name: "bad", Key: "sk-bad"},
			{Name: "down", Key: "sk-down"}, {Name: "denied", Key: "sk-denied"}, {Name: "nobal", Key: "sk-nobal"}}}); err != nil {
		t.Fatal(err)
	}
	got := map[string]SubscriptionQuota{}
	for _, q := range KeyBalances(context.Background()) {
		got[q.User] = q
	}
	pass := got["pass"]
	if pass.Error != "" || pass.Balance != "$4.25" {
		t.Fatalf("ClinePass key's card: %+v", pass)
	}
	var ws []string
	for _, w := range pass.Windows {
		ws = append(ws, fmt.Sprintf("%s=%gh/%g@%s", w.Name, w.Span.Hours(), w.Used, w.ResetsAt.UTC().Format("01-02T15")))
	}
	if want := "5 hours=5h/40@10-05T15,Weekly=168h/100@10-10T00,Month=720h/12.5@11-01T00"; strings.Join(ws, ",") != want {
		t.Fatalf("windows %s, want %s", strings.Join(ws, ","), want)
	}
	if free := got["free"]; free.Error != "" || free.Balance != "$12.00" || len(free.Windows) != 0 {
		t.Fatalf("a key with no ClinePass: %+v", free)
	}
	if bad := got["bad"]; !strings.Contains(bad.Error, "401") || bad.Balance != "" {
		t.Fatalf("a key Cline refuses: %+v", bad)
	}
	// #79: limits that fail for a reason other than no ClinePass (a 404)
	// say so beside the balance, rather than look like no ClinePass
	for user, code := range map[string]string{"down": "503", "denied": "403"} {
		q := got[user]
		if !strings.HasPrefix(q.Error, "ClinePass limits couldn't be read: ") || !strings.Contains(q.Error, code) || q.Balance != "$12.00" || len(q.Windows) != 0 {
			t.Fatalf("a key whose limits fail with %s: %+v", code, q)
		}
	}
	// a balance that fails keeps the limits read
	if q := got["nobal"]; q.Error != "" || q.Balance != "" || len(q.Windows) != 3 {
		t.Fatalf("a key whose balance fails: %+v", q)
	}
}
