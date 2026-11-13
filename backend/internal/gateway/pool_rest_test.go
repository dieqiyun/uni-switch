package gateway

import (
	"net/http"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A subscription refused for a pool of its allowance that counts some
// models only rests for that model alone: the account still takes the
// models the pool doesn't count (Xiaopodev on X: Cursor's Other Models
// used up kept Auto off the account too). One of the whole account's
// windows used up rests the account.
func TestPoolRefusalRestsTheModelOnly(t *testing.T) {
	old := allowances
	defer func() { allowances = old }()
	fresh(t)
	now := time.Now()
	week := now.Add(39 * time.Hour)
	allowances = func(string) map[string]provider.Allowance {
		return map[string]provider.Allowance{
			"me@x.com": {
				{Used: 100, Resets: week, Span: 7 * 24 * time.Hour, Model: "opus"},
				{Used: 40, Resets: week, Span: 7 * 24 * time.Hour},
			},
			"out@x.com": {{Used: 100, Resets: week, Span: 7 * 24 * time.Hour}},
		}
	}
	acct := func(user, model string) candidate {
		return candidate{p: provider.Provider{ID: "claude", Account: &provider.Account{Agent: "claude", User: user}}, model: model, rest: "claude"}
	}
	s := &Server{}
	refused := []byte(`{"error":{"message":"usage limit reached for this model"}}`)

	opus, sonnet := acct("me@x.com", "claude-opus-4-7"), acct("me@x.com", "claude-sonnet-4-6")
	r := s.restAfter(opus, 429, http.Header{}, refused)
	if r.Why != failQuota || r.Key != opus.restID() || r.By != "window" {
		t.Fatalf("Opus out of its week rests %+v", r)
	}
	// the app shows it on Opus's rows only (#1960)
	if r.Model != opus.model {
		t.Fatalf("Opus's own rest says model %q", r.Model)
	}
	if kept, _ := restOf(opus.restID()); kept.Model != opus.model {
		t.Fatalf("Opus's rest, read back, says model %q", kept.Model)
	}
	if _, ok := restOf(sonnet.restKey()); ok {
		t.Fatal("Opus out of its own week rests the account")
	}
	if _, ok := restOf(sonnet.restID()); ok {
		t.Fatal("Opus out of its own week rests Sonnet")
	}
	if !spentAfter([]candidate{opus}) || spentAfter([]candidate{sonnet}) {
		t.Fatal("spentAfter doesn't go by the model's rest")
	}

	whole := acct("out@x.com", "claude-sonnet-4-6")
	if r := s.restAfter(whole, 429, http.Header{}, refused); r.Key != whole.restKey() || r.Model != "" {
		t.Fatalf("the account's week used up rests %s (model %q), not the account", r.Key, r.Model)
	}
	if _, ok := restOf(acct("out@x.com", "claude-opus-4-7").restKey()); !ok {
		t.Fatal("the account's week used up leaves its other models in")
	}
}
