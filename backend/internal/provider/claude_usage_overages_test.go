package provider

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// claudeOveragesText is Claude Code's /usage for an account past a limit
// and billed for what it uses: its overages line, then its windows (the
// strings as Claude Code 2.1.316 has them).
const claudeOveragesText = `You are currently using your overages to power your Claude Code usage. We will automatically switch you back to your subscription rate limits when they reset

Current session: 100% used · resets 3pm (UTC)
Current week (all models): 40% used · resets Nov 6 at 11am (UTC)
`

// StringKe (#4532): the overages line made the whole reading an error,
// so the card showed no windows and the switch, which stays on an account
// it can't read, left Claude Code on overages. It is read with the
// windows, and a spent "Extra usage" row that ends with the window used
// up.
func TestClaudeUsageOverages(t *testing.T) {
	now := time.Date(2026, 11, 1, 13, 0, 0, 0, time.UTC)
	ws, err := parseClaudeUsage(claudeOveragesText, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 3 || ws[0].Name != "5 hours" || ws[0].Used != 100 || ws[1].Name != "7 days" || ws[1].Used != 40 {
		t.Fatalf("windows: %+v", ws)
	}
	x := ws[2]
	if x.Name != "Extra usage" || !x.Text || x.Used != 100 || x.ResetsAt == nil || !x.ResetsAt.Equal(*ws[0].ResetsAt) {
		t.Fatalf("overages row: %+v", x)
	}
	if !usedUp(SubscriptionQuota{Windows: ws}, now) {
		t.Fatal("an account on overages read as having room")
	}
	// past the session's reset, the row is gone with it
	if usedPast(SubscriptionQuota{Windows: elapsed(ws, now.Add(3*time.Hour))}, 98, now.Add(3*time.Hour)) {
		t.Fatal("still spent after the used-up window renewed")
	}
	// on overages with every window under the line it names (a model's
	// own week used up), the account is still spent, till the next reading
	text := strings.Replace(claudeOveragesText, "Current session: 100%", "Current session: 10%", 1)
	ws, err = parseClaudeUsage(text, now)
	if err != nil || len(ws) != 3 || ws[2].ResetsAt != nil || !usedPast(SubscriptionQuota{Windows: ws}, 98, now) {
		t.Fatalf("overages, windows under the line: %+v %v", ws, err)
	}
}

// The switch moves Claude Code off an account on overages to a spare
// with room, as off one whose window is used up.
func TestClaudeSwitchOffOverages(t *testing.T) {
	claudeThree(t) // pro@x.com signed in; team and max saved
	var text atomic.Value
	text.Store("")
	fakeClaudeUsage(t, &text, nil)
	forget := func() {
		lastQuotas.Lock()
		lastQuotas.m, lastQuotas.loaded = nil, false
		lastQuotas.Unlock()
		loginUsageCache.Lock()
		loginUsageCache.m = nil
		loginUsageCache.Unlock()
	}
	forget()
	t.Cleanup(forget)
	if err := SetSwitchOnly("claude", true); err != nil {
		t.Fatal(err)
	}
	spare := ""
	for _, l := range Logins("claude") {
		if strings.HasPrefix(l.User, "max@x.com") {
			spare = l.User
			if err := SetLoginOn("claude", l.User, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	// windows under the line, so only the overages line says it's spent
	reset := time.Now().UTC().Add(72*time.Hour).Format("Jan 2 at 3pm") + " (UTC)"
	under := "Current session: 10% used · resets " + reset + "\nCurrent week (all models): 40% used · resets " + reset + "\n"
	var mu sync.Mutex
	active := under
	UsageClaudeVia(func(_ context.Context, dir string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if dir == "" {
			return active, nil
		}
		return under, nil // the spare
	})
	ctx := context.Background()
	if to, err := SwitchWhenSpent(ctx, "claude"); err != nil || to != "" {
		t.Fatalf("with room: switched to %q: %v", to, err)
	}
	mu.Lock()
	active = strings.SplitN(claudeOveragesText, "\n", 2)[0] + "\n\n" + under
	mu.Unlock()
	forget()
	claudeUsage.Lock()
	for k, e := range claudeUsage.m {
		e.tried, e.at = e.tried.Add(-time.Hour), e.at.Add(-time.Hour)
		claudeUsage.m[k] = e
	}
	claudeUsage.Unlock()
	if to, err := SwitchWhenSpent(ctx, "claude"); err != nil || to != spare {
		t.Fatalf("on overages: switched to %q: %v, want %q", to, err, spare)
	}
	for user, u := range LoginUsage(ctx, "claude") {
		if strings.HasPrefix(user, "pro@x.com") && (u.Error != "" || len(u.Windows) != 3) {
			t.Fatalf("the overages account's card: %+v", u)
		}
	}
}
