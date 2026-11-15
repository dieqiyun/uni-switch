package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func TestHistoricalWindowHoldsUsesEachWindowsOwnReading(t *testing.T) {
	holdsHome(t)
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	from, to := now.Add(-13*24*time.Hour), now.Add(-6*24*time.Hour)
	weekAt := to.Add(-24 * time.Hour)
	fiveReset := to.Add(-time.Hour)
	fiveStart := fiveReset.Add(-5 * time.Hour)
	fiveAt := to.Add(-2 * time.Hour)
	futureReset := now.Add(24 * time.Hour)
	call := func(at time.Time, n int, user string) {
		Append(Record{Time: at, Provider: "codex", ProviderAccount: user, Model: "gpt-5.5", Input: n, Status: 200})
	}
	user := "sample@example.com"
	call(from.Add(-time.Hour), 1, user) // the ledger covers the entire old week
	call(weekAt.Add(-time.Hour), 1100, user)
	call(fiveAt.Add(-time.Hour), 2200, user)
	call(fiveAt.Add(time.Minute), 999999, user) // after both selected readings
	call(now.Add(-time.Hour), 888888, user)     // today's traffic
	call(fiveAt.Add(-time.Minute), 777777, "other@example.com")
	hist := []provider.QuotaHistory{{Provider: "codex", User: user, Lines: []provider.QuotaLine{
		{Name: "5 hours", Points: []provider.QuotaPoint{{At: fiveAt, Left: 80, Start: &fiveStart, ResetsAt: &fiveReset}, {At: now, Left: 0, ResetsAt: &futureReset}}},
		{Name: "7 days", Points: []provider.QuotaPoint{{At: weekAt, Left: 60, Start: &from, ResetsAt: &to}, {At: now, Left: 0, ResetsAt: &futureReset}}},
	}}}
	// Today's percentages are not historical observations.
	cards := []provider.SubscriptionQuota{{Provider: "codex", User: user, Windows: []provider.QuotaWindow{
		{Name: "5 hours", Used: 1, ResetsAt: &futureReset, Holds: &provider.WindowHolds{Tokens: 9999999}},
		{Name: "7 days", Used: 1, ResetsAt: &futureReset},
	}}}
	got := HistoricalWindowHolds(cards, hist, from, to)
	if len(got) != 2 {
		t.Fatalf("historical windows: %+v", got)
	}
	for i, want := range []struct {
		at     time.Time
		tokens int64
		used   float64
	}{{fiveAt, 11000, 20}, {weekAt, 2750, 40}} {
		w := got[i].Windows[0]
		if h := w.Holds; h == nil || h.Tokens != want.tokens || h.Used != want.used || !h.Priced || !got[i].ReadAt.Equal(want.at) {
			t.Errorf("%s: %+v at %v, want %d at %v", w.Name, h, got[i].ReadAt, want.tokens, want.at)
		}
	}
	if cards[0].Windows[0].Holds.Tokens != 9999999 || hist[0].Lines[0].Points[0].Left != 80 {
		t.Fatal("the caller's current card or history was changed")
	}
	// A range with no readings never falls back to the current card.
	if got := HistoricalWindowHolds(cards, hist, to, now.Add(-time.Hour)); len(got) != 0 {
		t.Fatalf("missing old readings: %+v", got)
	}
	// A card that tells a balance may have paid from it, so a past cycle
	// is no more reckoned than the live one.
	paid := []provider.SubscriptionQuota{{Provider: "codex", User: user, Balance: "12 credits", Windows: cards[0].Windows}}
	for _, s := range HistoricalWindowHolds(paid, hist, from, to) {
		if w := s.Windows[0]; w.Holds != nil || w.HoldsWhy != "" {
			t.Errorf("%s with a balance: %+v (%q)", w.Name, w.Holds, w.HoldsWhy)
		}
	}
}

func TestWindowHoldsUsesObservedRefillStart(t *testing.T) {
	holdsHome(t)
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	reset := now.Add(3 * 24 * time.Hour)
	nominal := reset.Add(-7 * 24 * time.Hour)
	refill := now.Add(-24 * time.Hour)
	before := refill.Add(-time.Hour)
	at := now.Add(-time.Hour)
	user := "sample@example.com"
	points := []provider.QuotaPoint{{At: before, Left: 20, Start: &nominal, ResetsAt: &reset}, {At: refill, Left: 100, Start: &nominal, ResetsAt: &reset}, {At: at, Left: 80, Start: &nominal, ResetsAt: &reset}}
	hist := []provider.QuotaHistory{{Provider: "codex", User: user, Lines: []provider.QuotaLine{{Name: "7 days", Points: points}}}}
	b, _ := json.Marshal(map[string]map[string][]provider.QuotaPoint{"codex|" + user: {"7 days": points}})
	dir := filepath.Dir(provider.Path())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "quota-history.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, r := range []Record{
		{Time: nominal.Add(-time.Hour), Input: 1},
		{Time: before, Input: 9000000},
		{Time: refill.Add(time.Hour), Input: 1000},
	} {
		r.Provider = "codex"
		r.ProviderAccount = user
		r.Model = "gpt-5.5"
		r.Status = 200
		Append(r)
	}
	cards := []provider.SubscriptionQuota{{Provider: "codex", User: user, ReadAt: &at, Windows: []provider.QuotaWindow{{Name: "7 days", Used: 20, ResetsAt: &reset, Span: 7 * 24 * time.Hour}}}}
	for name, got := range map[string][]provider.SubscriptionQuota{
		"current":    WithWindowHolds(cards, now),
		"historical": HistoricalWindowHolds(cards, hist, refill, now),
	} {
		if len(got) != 1 {
			t.Fatalf("%s: %+v", name, got)
		}
		h := got[0].Windows[0].Holds
		if h == nil || h.Tokens != 5000 || !h.Since.Equal(refill) {
			t.Errorf("%s: %+v, want 5000 tokens since %v", name, h, refill)
		}
	}
	// The observed start narrows only what is reckoned: the card's window
	// keeps its span, so its Pace (WithWindowStarts) is the vendor's.
	if got := WithWindowHolds(cards, now)[0].Windows[0]; got.Span != 7*24*time.Hour {
		t.Fatalf("current window's span became %v", got.Span)
	}
	if cards[0].Windows[0].Span != 7*24*time.Hour {
		t.Fatal("cached window changed")
	}
}

func TestWindowHoldsFirstReadingIsNotARefill(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	at, reset := now.Add(-time.Hour), now.Add(3*24*time.Hour)
	cards := []provider.SubscriptionQuota{{Provider: "codex", User: "sample", ReadAt: &at, Windows: []provider.QuotaWindow{{Name: "7 days", Used: 20, ResetsAt: &reset, Span: 7 * 24 * time.Hour}}}}
	firstAt := at.Add(-time.Hour)
	hist := []provider.QuotaHistory{{Provider: "codex", User: "sample", Lines: []provider.QuotaLine{{Name: "7 days", Points: []provider.QuotaPoint{{At: firstAt, Left: 80, ResetsAt: &reset}}}}}}
	got := withRecordedWindowStarts(cards, hist, now)
	if got[0].Windows[0].Span != 7*24*time.Hour {
		t.Fatal("first observed reading was mistaken for a reset")
	}
}
