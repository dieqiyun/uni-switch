package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestCodexQuotaUsesProviderWindowDuration(t *testing.T) {
	for _, tt := range []struct {
		seconds int64
		want    string
	}{{5 * 60 * 60, "5 hours"}, {7 * 24 * 60 * 60, "7 days"}, {0, "Allowance"}} {
		if got := quotaDurationName(tt.seconds); got != tt.want {
			t.Errorf("quotaDurationName(%d) = %q, want %q", tt.seconds, got, tt.want)
		}
	}

	w := codexWindow{UsedPercent: 16, LimitWindowSecs: 7 * 24 * 60 * 60}
	if got := w.window(); got.Name != "7 days" || got.Used != 16 {
		t.Fatalf("Codex window = %+v", got)
	}
}

func TestCompactQuotaNumber(t *testing.T) {
	for _, tt := range []struct {
		in   float64
		want string
	}{{7, "7"}, {7.5, "7.5"}, {7.25, "7.25"}} {
		if got := compactNumber(tt.in); got != tt.want {
			t.Errorf("compactNumber(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// A stale copy comes back at once while it refreshes, and accounts removed
// from uni-switch in the meantime are left out of it.
func TestSubscriptionUsageServesStale(t *testing.T) {
	signIn(t)
	if err := Delete("codex"); err != nil {
		t.Fatal(err)
	}
	// The refresh is held at the vendor, so a copy that waited for it could
	// not come back: what the stale copy returns, it returns while the
	// refresh is still in flight (#1586). Held rather than timed, because a
	// loaded machine makes a wall-clock bound say nothing either way.
	held := make(chan struct{})
	reached := make(chan struct{})
	var releaseOnce, reachedOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(held) }) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reachedOnce.Do(func() { close(reached) })
		<-held
		w.Write([]byte(`{"copilot_plan":"individual","token_based_billing":true,"quota_snapshots":` +
			`{"premium_interactions":{"has_quota":true,"unlimited":false,"entitlement":"300","percent_remaining":50}}}`))
	}))
	oldUser := CopilotUserURL
	CopilotUserURL = srv.URL
	t.Cleanup(func() { CopilotUserURL = oldUser })

	c := &subscriptionUsageCache
	c.Lock()
	// all four fields the call reads: asked is part of the state this test
	// primes, not something to inherit from whatever ran before it
	c.at, c.asked, c.pending = time.Now().Add(-time.Hour), false, nil
	c.data = []SubscriptionQuota{{Provider: "claude", Name: "Claude Code"}, {Provider: "codex", Name: "Codex"}}
	c.Unlock()
	t.Cleanup(func() {
		for {
			c.Lock()
			p := c.pending
			if p == nil {
				c.data, c.at = nil, time.Time{}
				c.Unlock()
				return
			}
			c.Unlock()
			<-p
		}
	})
	// before the wait above, which the held vendor would otherwise keep
	// blocked, and before the process is left with a server nobody closes
	t.Cleanup(func() { release(); srv.Close() })

	// The deadline is only so a copy that did wait fails as a FAIL rather
	// than hanging the run: nothing here is measured against it.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got := SubscriptionUsage(ctx)
	if len(got) != 1 || got[0].Provider != "claude" {
		t.Fatalf("got %+v, want only claude: a copy that waited for the held refresh comes back with nothing", got)
	}
	if !SubscriptionUsageReading() {
		t.Fatal("the copy came back with no refresh in flight")
	}
	select {
	case <-reached:
	case <-time.After(15 * time.Second):
		t.Fatal("a stale copy did not start a refresh")
	}
	// what showed the stale copy hears when the new one lands
	landed := make(chan struct{}, 1)
	OnSubscriptionUsage(func() {
		select {
		case landed <- struct{}{}:
		default:
		}
	})
	t.Cleanup(func() { OnSubscriptionUsage(nil) })
	release()
	c.Lock()
	p := c.pending
	c.at = time.Time{}
	c.Unlock()
	if p != nil {
		<-p
	}
	SubscriptionUsage(context.Background())
	select {
	case <-landed:
	case <-time.After(15 * time.Second):
		t.Fatal("not told the refresh landed")
	}
}

func TestChosenWindows(t *testing.T) {
	ws := []QuotaWindow{{Name: "Gemini 3.8 Flash (High)", Model: "gemini-3.8-flash-high"}, {Name: "Gemini 3 Flash", Model: "gemini-3-flash"}, {Name: "Weekly"}}
	got := chosenWindows(ws, map[string]bool{"gemini-3.8-flash-high": true}, nil)
	if len(got) != 2 || got[0].Model != "gemini-3.8-flash-high" || got[1].Name != "Weekly" {
		t.Fatalf("got %+v", got)
	}
	// ids the quota names that none of the enabled ones match: keep them all
	if got := chosenWindows(ws[:2], map[string]bool{"gemini-pro-agent": true}, nil); len(got) != 2 {
		t.Fatalf("kept %d of 2", len(got))
	}
	// Antigravity's levels of a model enabled as one (#150): each level's
	// window is kept for the family picked, and only those
	ag := []QuotaWindow{{Model: "gemini-3.7-flash-high"}, {Model: "gemini-3.7-flash-low"}, {Model: "gemini-3.1-pro-high"},
		{Model: "claude-opus-4-6-thinking"}, {Model: "gemini-3.5-flash-lite"}}
	base := func(id string) string {
		if b, _, ok := antigravityBaseIn(antigravityListed, id); ok {
			return b
		}
		return id
	}
	got = chosenWindows(ag, map[string]bool{"gemini-3.7-flash": true, "claude-opus-4-6-thinking": true}, base)
	if len(got) != 3 || got[0].Model != "gemini-3.7-flash-high" || got[1].Model != "gemini-3.7-flash-low" || got[2].Model != "claude-opus-4-6-thinking" {
		t.Fatalf("antigravity: %+v", got)
	}
}

// A Copilot account uni-switch signed in itself gets its card even when the
// editors' own sign-in isn't on the machine: every account uni-switch knows is
// asked, not only the one the editors' or the CLI's token belongs to.
func TestCopilotQuotaWithoutEditorsSignIn(t *testing.T) {
	home := signIn(t)
	t.Setenv("COPILOT_HOME", filepath.Join(home, ".copilot")) // not the machine's own CLI, when it has one
	// the editors' own sign-in gone: what uni-switch keeps is all that's left
	for _, dir := range []string{filepath.Join(home, ".config", "github-copilot"), filepath.Join(home, ".copilot")} {
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := copilotLogin(filepath.Join(home, ".config")); ok {
		t.Fatal("the editors' own sign-in is still readable")
	}
	if err := addCopilotLogin("hubot", "Pro+", "ghu_hubot", ""); err != nil {
		t.Fatal(err)
	}
	if ls := copilotLoginList(); len(ls) != 1 || ls[0].User != "hubot" {
		t.Fatalf("copilotLoginList = %+v", ls)
	}

	old := CopilotUserURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token ghu_hubot" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"copilot_plan":"individual_pro","quota_snapshots":{"premium_interactions":{"has_quota":true,"entitlement":7000,"quota_remaining":6996}}}`))
	}))
	defer srv.Close()
	CopilotUserURL = srv.URL
	t.Cleanup(func() { CopilotUserURL = old })

	var card *SubscriptionQuota
	for _, q := range fetchSubscriptionUsage(context.Background()) {
		if q.Provider == "copilot" {
			card = &q
		}
	}
	if card == nil {
		t.Fatal("no Copilot card: an account signed in from uni-switch alone was left out")
	}
	if card.User != "hubot" || card.Plan != "Pro+" || len(card.Windows) != 1 {
		t.Fatalf("Copilot card = %+v", *card)
	}
	if w := card.Windows[0]; w.Name != "Premium requests" || w.Display != "4 / 7000" {
		t.Fatalf("window = %+v", w)
	}
}
