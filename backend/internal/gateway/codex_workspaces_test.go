package gateway

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #1424: one email in two Team workspaces, Codex signed in to the second
// (saved as "me@example.com · Team · ws-two"). The account Codex is on
// went by the first one's name, "me@example.com · Team": Smart read the
// first one's allowance for it, 98% used, and sent the turn to another
// email's account, while the seat Codex is on had 2% used and its week
// renewing first.
func TestCodexTeamSeatsOfOneEmailWeighedApart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	restingUntil.Lock()
	restingUntil.m = map[string]time.Time{}
	restingUntil.Unlock()
	claims := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
	}
	auth := func(email, plan, ws string) map[string]any {
		return map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
			"id_token": claims(map[string]any{"email": email, "https://api.openai.com/auth": map[string]any{
				"chatgpt_plan_type": plan, "chatgpt_account_id": ws}}),
			"access_token":  claims(map[string]any{"exp": time.Now().Add(time.Hour).Unix(), "who": ws}),
			"refresh_token": "r-" + ws, "account_id": ws}}
	}
	const (
		teamA = "me@example.com · Team"
		teamB = "me@example.com · Team · ws-two"
		other = "other@example.com"
	)
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	os.WriteFile(filepath.Join(home, ".codex", "auth.json"), mustJSON(auth("me@example.com", "team", "ws-two")), 0o600)
	saved := []map[string]any{
		{"agent": "codex", "user": teamA, "plan": "team", "on": true, "seen": time.Now(), "auth": auth("me@example.com", "team", "ws-one")},
		{"agent": "codex", "user": teamB, "plan": "team", "on": true, "seen": time.Now(), "auth": auth("me@example.com", "team", "ws-two")},
		{"agent": "codex", "user": other, "plan": "plus", "on": true, "seen": time.Now(), "auth": auth(other, "plus", "ws-other")},
	}
	os.MkdirAll(filepath.Dir(provider.Path()), 0o755)
	os.WriteFile(filepath.Join(filepath.Dir(provider.Path()), "logins.json"), mustJSON(saved), 0o600)
	provider.ForgetAccounts()
	t.Cleanup(provider.ForgetAccounts)
	old := allowances
	allowances = provider.Allowances
	t.Cleanup(func() { allowances = old })

	now := time.Now()
	five := now.Add(2 * time.Hour).Unix()
	// used, and when the week renews: the seat Codex is on renews first
	usage := map[string]struct {
		used float64
		week time.Duration
	}{"ws-one": {98, 5 * 24 * time.Hour}, "ws-two": {2, 2 * 24 * time.Hour}, "ws-other": {20, 6 * 24 * time.Hour}}
	var mu sync.Mutex
	var tried []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws := r.Header.Get("chatgpt-account-id")
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/backend-api/codex/responses":
			io.ReadAll(r.Body)
			tried = append(tried, ws)
			io.WriteString(w, sse(
				`data: {"type":"response.created","response":{"id":"r1","model":"gpt-5.5"}}`,
				`data: {"type":"response.output_text.delta","delta":"pong"}`,
				`data: {"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":7,"output_tokens":1}}}`))
		case "/backend-api/wham/usage":
			u, ok := usage[ws]
			if !ok {
				w.WriteHeader(401)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"plan_type": "team",
				"rate_limit": map[string]any{"allowed": true,
					"primary_window":   map[string]any{"used_percent": u.used, "limit_window_seconds": 18000, "reset_at": five},
					"secondary_window": map[string]any{"used_percent": u.used, "limit_window_seconds": 604800, "reset_at": now.Add(u.week).Unix()}}})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(up.Close)
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })
	if err := provider.SetRouting("codex", ""); err != nil { // Smart
		t.Fatal(err)
	}
	users := []string{teamA, teamB, other}
	for _, u := range users {
		provider.StaleAllowance("codex", u)
	}
	t.Cleanup(func() {
		for _, u := range users {
			provider.StaleAllowance("codex", u)
		}
	})
	share := func(u string) float64 {
		n, _ := provider.Allowances("codex")[u].For("gpt-5.5", time.Now())
		return n
	}
	for deadline := time.Now().Add(5 * time.Second); share(teamA) != 98 || share(teamB) != 2 || share(other) != 20; {
		if time.Now().After(deadline) {
			t.Fatalf("never read: %v%%, %v%%, %v%%", share(teamA), share(teamB), share(other))
		}
		time.Sleep(10 * time.Millisecond)
	}

	// the account Codex is on goes by its own seat's name, apart from the other's
	p, err := provider.Find("codex")
	if err != nil {
		t.Fatal(err)
	}
	if p.Account == nil || p.Account.User != teamB {
		t.Fatalf("the account Codex is on goes by %q, want %q", p.Account.User, teamB)
	}
	names := map[string]bool{p.Account.User: true}
	for _, q := range p.AlsoOn() {
		if names[q.Account.User] {
			t.Fatalf("two candidates go by %q", q.Account.User)
		}
		names[q.Account.User] = true
	}
	if len(names) != 3 {
		t.Fatalf("candidates: %v", names)
	}

	srv := New()
	if code, body := resetPost(t, srv); code != 200 || !strings.Contains(body, "pong") {
		t.Fatalf("turn: %d %s", code, body)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := strings.Join(tried, ","); got != "ws-two" {
		t.Fatalf("sent to %s; want the seat Codex is on, 2%% used and renewing first (ws-two)", got)
	}
}
