package gateway

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// standInGroup is 01huadalang's setup (Discord, #feedback): a group "G"
// of Codex, signed in to two accounts, listed first and a pay-as-you-go
// MiMo key listed second as the backup, routed routing with Stays mode.
// Codex answers 429 usage_limit_reached while down; allowances, when
// known, says each account's 5 hours used (100 while down, five after)
// and its week at weekly. up has Codex back: the 5 hours renewed and every
// rest over, as when the vendor's reset passes.
type standInGroup struct {
	s      *Server
	mu     sync.Mutex
	down   bool
	five   float64
	weekly float64
}

func (g *standInGroup) up() {
	g.mu.Lock()
	g.down = false
	g.mu.Unlock()
	restingUntil.Lock()
	for k := range restingUntil.m {
		restingUntil.m[k] = time.Now().Add(-time.Second)
	}
	restingUntil.Unlock()
	provider.ForgetAccounts()
}

// responsesTurn is the Responses body of a conversation's n-th user turn
// (n from 1), each earlier turn answered.
func responsesTurn(n int) string {
	var b strings.Builder
	b.WriteString(`{"model":"group/g","stream":true,"input":[`)
	for i := 1; i <= n; i++ {
		if i > 1 {
			b.WriteString(`{"role":"assistant","content":[{"type":"output_text","text":"yo"}]},`)
		}
		b.WriteString(`{"role":"user","content":[{"type":"input_text","text":"turn ` + itoa(i) + `"}]}`)
		if i < n {
			b.WriteString(",")
		}
	}
	b.WriteString(`]}`)
	return b.String()
}

// ask sends turn n of session sess, and says who answered it: "codex" or
// "mimo", and the affinity's why.
func (g *standInGroup) ask(t *testing.T, sess string, n int) (string, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(responsesTurn(n)))
	req.Header.Set("Authorization", "Bearer chatgpt-token")
	req.Header.Set("session_id", sess)
	g.s.Handler().ServeHTTP(rec, req)
	why := ""
	if a := lastRoute(g.s).Affinity; a != nil {
		why = a.Why
	}
	switch body := rec.Body.String(); {
	case strings.Contains(body, "pong"):
		return "codex", why
	case strings.Contains(body, "hello"):
		return "mimo", why
	default:
		t.Fatalf("turn %d of %s: %d %s", n, sess, rec.Code, body)
	}
	return "", why
}
