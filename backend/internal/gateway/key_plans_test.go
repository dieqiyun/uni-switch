package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// yckonnnn on X: Volcengine's pay as you go and its Agent Plan, a key of
// each in one provider. Each key is taken only at its own plan's
// endpoints, so each request with it goes there: on the first try, on
// a failover to it, and for a model only its plan has.
func TestExplicitProvidersKeepTheirEndpoints(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	restingUntil.Lock()
	restingUntil.m, restingUntil.note = map[string]time.Time{}, map[string]Rest{}
	restingUntil.Unlock()
	routed.Lock()
	routed.failures = map[string]int{}
	routed.Unlock()
	home := map[string]string{"k-coding": "/api/coding/v3", "k-api": "/api/v3", "k-agent": "/api/plan/v3"}
	var mu sync.Mutex
	var seen []string
	limited := map[string]bool{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/models") {
			io.WriteString(w, `{"data":[{"id":"kimi-k3"},{"id":"deepseek-v3-250324"}]}`)
			return
		}
		var req struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		seen = append(seen, key+" "+strings.TrimSuffix(r.URL.Path, "/chat/completions")+" "+req.Model)
		lim := limited[key]
		mu.Unlock()
		// Ark answers a key at another plan's endpoint as unknown
		if strings.TrimSuffix(r.URL.Path, "/chat/completions") != home[key] {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":{"code":"AuthenticationError","message":"the API key is invalid"}}`)
			return
		}
		if lim {
			w.WriteHeader(429)
			io.WriteString(w, `{"error":{"message":"rate limited"}}`)
			return
		}
		io.WriteString(w, `{"id":"from-`+key+`","choices":[]}`)
	}))
	defer up.Close()
	for _, cfg := range []struct {
		id, key, path string
		models        []string
	}{
		{"volc", "k-coding", "/api/coding/v3", []string{"kimi-k3"}},
		{"volc-api", "k-api", "/api/v3", []string{"kimi-k3", "deepseek-v3-250324"}},
		{"volc-agent", "k-agent", "/api/plan/v3", []string{"kimi-k3", "deepseek-v3-250324"}},
	} {
		if err := provider.Save(provider.Provider{ID: cfg.id, Name: cfg.id, Chat: up.URL + cfg.path, Key: cfg.key, Models: cfg.models}); err != nil {
			t.Fatal(err)
		}
	}

	s := New()
	send := func(model string) (int, string, []string) {
		mu.Lock()
		seen = nil
		mu.Unlock()
		rec := httptest.NewRecorder()
		body := `{"model":"` + model + `","messages":[{"role":"user","content":"hi"}]}`
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
		mu.Lock()
		defer mu.Unlock()
		return rec.Code, rec.Body.String(), append([]string(nil), seen...)
	}
	// the plan's own key, at the plan's endpoint
	if code, body, tried := send("kimi-k3"); code != 200 || !strings.Contains(body, "from-k-coding") || strings.Join(tried, ",") != "k-coding /api/coding/v3 kimi-k3" {
		t.Fatalf("first: %d %s, tried %v", code, body, tried)
	}
	// a model pay as you go alone has: its key alone, at /api/v3
	if code, body, tried := send("deepseek-v3-250324"); code != 200 || !strings.Contains(body, "from-k-api") || strings.Join(tried, ",") != "k-api /api/v3 deepseek-v3-250324" {
		t.Fatalf("pay-as-you-go model: %d %s, tried %v", code, body, tried)
	}
	// A qualified provider stays there even while another can answer.
	mu.Lock()
	limited["k-coding"] = true
	mu.Unlock()
	qualifiedCode, qualifiedBody, qualifiedTries := send("volc/kimi-k3")
	if qualifiedCode != 429 || strings.Contains(qualifiedBody, "from-k-api") {
		t.Fatalf("qualified provider escaped: %d %s, tried %v", qualifiedCode, qualifiedBody, qualifiedTries)
	}
	for _, attempt := range qualifiedTries {
		if !strings.HasPrefix(attempt, "k-coding ") { t.Fatalf("qualified provider used another key: %v", qualifiedTries) }
	}
	// failover: the Coding Plan key limited, the next key at its own endpoint
	mu.Lock()
	limited["k-coding"] = true
	mu.Unlock()
	if code, body, tried := send("kimi-k3"); code != 200 || !strings.Contains(body, "from-k-api") ||
		strings.Join(tried, ",") != "k-coding /api/coding/v3 kimi-k3,k-api /api/v3 kimi-k3" {
		t.Fatalf("failover: %d %s, tried %v", code, body, tried)
	}
	// and on to the Agent Plan's
	mu.Lock()
	limited["k-api"] = true
	mu.Unlock()
	code, body, tried := send("kimi-k3")
	if code != 200 || !strings.Contains(body, "from-k-agent") {
		t.Fatalf("second failover: %d %s, tried %v", code, body, tried)
	}
	for _, x := range tried {
		f := strings.Fields(x)
		if home[f[0]] != f[1] {
			t.Errorf("key %s sent to %s", f[0], f[1])
		}
	}
}
