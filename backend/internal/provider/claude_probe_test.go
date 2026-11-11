package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
)

// A Claude account is tested by Claude Code, with a saved account's own
// sign-in, and nothing uni-switch sends reaches Anthropic's API in its name.
func TestClaudeTestRunsThroughClaudeCode(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("asked the API directly: %s %s", r.Method, r.URL)
	}))
	defer api.Close()
	type call struct{ oauth, model string }
	var mu sync.Mutex
	var calls []call
	old := claudeCLIProbe
	defer func() { claudeCLIProbe = old }()
	ProbeClaudeVia(func(_ context.Context, oauth, model string) error {
		mu.Lock()
		calls = append(calls, call{oauth, model})
		mu.Unlock()
		if model == "claude-bad" {
			return errors.New("Claude Code: no such model")
		}
		return nil
	})

	acct := &Account{Agent: "claude", User: "b@example.com", token: func(context.Context) (string, error) { return "saved-token", nil }}
	acct.sign = func(context.Context, *http.Request, []byte) error { return errClaudeViaCLI }
	p := Provider{ID: "claude", Anthropic: api.URL, Account: acct}
	r := p.TestModels(context.Background(), []string{"claude-sonnet-4-5", "claude-bad"})
	if len(r) != 2 || !r[0].OK || r[1].OK || r[1].Error != "Claude Code: no such model" || r[0].Protocol != Anthropic {
		t.Fatalf("results %+v", r)
	}
	sort.Slice(calls, func(i, j int) bool { return calls[i].model > calls[j].model }) // models are tried at once
	if len(calls) != 2 || calls[0] != (call{"saved-token", "claude-sonnet-4-5"}) || calls[1].model != "claude-bad" {
		t.Fatalf("Claude Code asked %+v", calls)
	}

	// the one Claude Code is signed in to: it signs itself
	calls = nil
	own := Provider{ID: "claude", Anthropic: api.URL, Account: &Account{Agent: "claude"}}
	if r := own.TestModels(context.Background(), []string{"claude-haiku-4-5"}); len(r) != 1 || !r[0].OK {
		t.Fatalf("results %+v", r)
	}
	if len(calls) != 1 || calls[0] != (call{"", "claude-haiku-4-5"}) {
		t.Fatalf("Claude Code asked %+v", calls)
	}

	req, _ := http.NewRequest(http.MethodPost, api.URL+"/v1/messages", nil)
	if err := p.Sign(context.Background(), req, Anthropic, nil); !errors.Is(err, errClaudeViaCLI) {
		t.Fatalf("a direct request was signed: %v", err)
	}
}
