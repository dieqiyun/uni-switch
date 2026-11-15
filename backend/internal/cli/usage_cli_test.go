package cli

import (
	"encoding/csv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
	stats "github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// holdUsageClock stops the usage clock at at for the test and gives at back,
// for the test to stamp its calls by: midnight then never falls between a
// call and the period it is asked for in.
func holdUsageClock(t *testing.T, at time.Time) time.Time {
	t.Helper()
	old := stats.Clock
	stats.Clock = func() time.Time { return at }
	t.Cleanup(func() { stats.Clock = old })
	return at
}

// midnightUsageClock sets the usage clock to read a tenth of a second before
// midnight the first time and a tenth of a second after it from then on, as
// when midnight falls while uni-switch usage is worked out.
func midnightUsageClock(t *testing.T, midnight time.Time) {
	t.Helper()
	var read atomic.Bool
	old := stats.Clock
	stats.Clock = func() time.Time {
		if read.Swap(true) {
			return midnight.Add(100 * time.Millisecond)
		}
		return midnight.Add(-100 * time.Millisecond)
	}
	t.Cleanup(func() { stats.Clock = old })
}

// uni-switch usage --csv writes the period's requests, newest first, one row
// each, with the model asked for, sent and served.
func TestUsageCSV(t *testing.T) {
	groupsHome(t)
	day := stats.Today.Since(holdUsageClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local)))
	stats.Append(stats.Record{Time: day.Add(time.Second), Agent: "codex", Provider: "a", Model: "m", Requested: "fast", Served: "m-mini", Input: 10, Output: 2, Millis: 700, Status: 200})
	stats.Append(stats.Record{Time: day.Add(2 * time.Second), Agent: "claude", Provider: "b", Model: "gpt-5.5", Requested: "b/gpt-5.5", Millis: 30, Status: 502})
	stats.Append(stats.Record{Time: day.AddDate(0, 0, -3), Agent: "pi", Provider: "a", Model: "m", Input: 1, Status: 200})
	var b strings.Builder
	if err := usageTo(&b, []string{"usage", "--csv", "today"}); err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(strings.NewReader(b.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 || strings.Join(recs[0], ",") != strings.Join(stats.CSVHeader, ",") {
		t.Fatalf("%q", recs)
	}
	col := func(row []string, name string) string {
		for i, h := range recs[0] {
			if h == name {
				return row[i]
			}
		}
		t.Fatalf("no column %s", name)
		return ""
	}
	if r := recs[1]; col(r, "agent") != "claude" || col(r, "status") != "502" || col(r, "error") != "true" || col(r, "requested_model") != "b/gpt-5.5" {
		t.Errorf("newest %q", r)
	}
	if r := recs[2]; col(r, "requested_model") != "fast" || col(r, "model") != "m" || col(r, "served_model") != "m-mini" || col(r, "swapped") != "true" ||
		col(r, "input_tokens") != "10" || col(r, "output_tokens") != "2" || col(r, "duration_ms") != "700" || col(r, "error") != "false" {
		t.Errorf("swapped %q", r)
	}
	b.Reset()
	if err := usageTo(&b, []string{"usage", "all", "--csv"}); err != nil || strings.Count(b.String(), "\n") != 4 {
		t.Fatalf("all: %v %q", err, b.String())
	}
	if err := usageTo(&b, []string{"usage", "--csv", "today", "extra"}); err == nil {
		t.Fatal("took an extra argument")
	}
}

func TestUsageProviderKeys(t *testing.T) {
	groupsHome(t)
	id := provider.KeyID("fixture-provider-secret")
	stats.Append(stats.Record{Provider: "relay", Model: "m", ProviderKeyID: id, ProviderKeyName: "Team", Input: 30, Status: 200})
	out, err := said(t, func() error { return usageCmd([]string{"usage", "all"}) })
	if err != nil || !strings.Contains(out, "upstream provider keys") || !strings.Contains(out, "relay / Team") || strings.Contains(out, "fixture-provider-secret") {
		t.Fatal(out, err)
	}
	var csv strings.Builder
	if err := usageTo(&csv, []string{"usage", "--csv", "all"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(csv.String(), "provider_key_id,provider_key_name") || !strings.Contains(csv.String(), id+",Team") || strings.Contains(csv.String(), "fixture-provider-secret") {
		t.Fatal(csv.String())
	}
}

// uni-switch usage shows, beside the gateway's calls, those the agents made on
// their own, read from their session files, as the window's Usage page
// does (Kumo31 on Discord: Codex used outside uni-switch wasn't in it).
func TestUsageShowsCallsNotThroughUniSwitch(t *testing.T) {
	groupsHome(t)
	now := holdUsageClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local))
	stats.Append(stats.Record{Time: now.Add(-time.Minute), Agent: "claude", Provider: "relay", Model: "m", Input: 5, Status: 200})
	old := stats.LogCalls
	stats.LogCalls = func(time.Time) []sessions.Call {
		return []sessions.Call{{Time: now.Add(-2 * time.Minute), Agent: "codex", Session: "c1", Model: "gpt-6-luna", Tokens: sessions.Tokens{Input: 1200, Output: 300}}}
	}
	t.Cleanup(func() { stats.LogCalls = old })
	var b strings.Builder
	if err := usageTo(&b, []string{"usage", "today"}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	gw, own, ok := strings.Cut(out, "not through uni-switch")
	if !ok || !strings.Contains(gw, "relay/m") || strings.Contains(gw, "gpt-6-luna") || !strings.Contains(own, "gpt-6-luna") || !strings.Contains(own, "1.5K") {
		t.Fatal(out)
	}

	// a call of the agents' own from before this month shows under all
	stats.LogCalls = func(time.Time) []sessions.Call {
		return []sessions.Call{{Time: now.AddDate(0, 0, -40), Agent: "codex", Model: "gpt-6-luna", Tokens: sessions.Tokens{Input: 7}}}
	}
	b.Reset()
	if err := usageTo(&b, []string{"usage", "all"}); err != nil || !strings.Contains(b.String(), "not through uni-switch") || !strings.Contains(b.String(), "gpt-6-luna") {
		t.Fatal(b.String(), err)
	}
}

// uni-switch usage asked as midnight falls shows the calls through uni-switch and
// those not through it of one day, the day it was asked on. Asked apart,
// the calls not through uni-switch were the next day's, none yet, and left out.
func TestUsageAskedAtMidnight(t *testing.T) {
	groupsHome(t)
	midnight := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)
	stats.Append(stats.Record{Time: midnight.Add(-time.Minute), Agent: "claude", Provider: "relay", Model: "m", Input: 5, Status: 200})
	old := stats.LogCalls
	stats.LogCalls = func(time.Time) []sessions.Call {
		return []sessions.Call{{Time: midnight.Add(-2 * time.Minute), Agent: "codex", Session: "c1", Model: "gpt-6-luna", Tokens: sessions.Tokens{Input: 1200, Output: 300}}}
	}
	t.Cleanup(func() { stats.LogCalls = old })
	midnightUsageClock(t, midnight)
	var b strings.Builder
	if err := usageTo(&b, []string{"usage", "today"}); err != nil {
		t.Fatal(err)
	}
	gw, own, ok := strings.Cut(b.String(), "not through uni-switch")
	if !ok || !strings.Contains(gw, "relay/m") || !strings.Contains(own, "gpt-6-luna") {
		t.Fatal(b.String())
	}
}

// uni-switch usage's headline says how many calls its cost leaves out for
// having no known price, beside the sum, not only a "+" after it (#1856):
// a Kimi K3 Auto that nobody prices is never read as costing nothing.
func TestUsageSaysCallsUnpriced(t *testing.T) {
	groupsHome(t)
	day := stats.Today.Since(holdUsageClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local)))
	if err := provider.SetModelPrice("a/m", &catalog.Price{Input: 1, Output: 2}); err != nil {
		t.Fatal(err)
	}
	stats.Append(stats.Record{Time: day.Add(time.Second), Agent: "codex", Provider: "a", Model: "m", Input: 1000, Output: 100, Status: 200})
	for i := range 2 {
		stats.Append(stats.Record{Time: day.Add(time.Duration(2+i) * time.Second), Agent: "codex", Provider: "a", Model: "only-a", Input: 1000, Output: 100, Status: 200})
	}
	var b strings.Builder
	if err := usageTo(&b, []string{"usage", "today"}); err != nil {
		t.Fatal(err)
	}
	head, _, _ := strings.Cut(b.String(), "\n")
	if !strings.Contains(head, "≈$0.001+") || !strings.Contains(head, "(2 calls unpriced)") {
		t.Fatalf("headline %q", head)
	}
}

// uni-switch usage says what the requests cost in their plans' credits where
// the replies said (WorkBuddy's usage.credit, #2512): in the summary, and
// on each agent's, model's and session's row of them; nothing where none did.
func TestUsageSaysCredits(t *testing.T) {
	groupsHome(t)
	day := stats.Today.Since(holdUsageClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local)))
	c := 0.35
	for i := 0; i < 2; i++ {
		stats.Append(stats.Record{Time: day.Add(time.Duration(i+1) * time.Second), Agent: "codex", Session: "s1", Provider: "workbuddy-ai", Model: "gemini-3.5-flash", Input: 10, Output: 2, Status: 200, Credits: &c})
	}
	stats.Append(stats.Record{Time: day.Add(5 * time.Second), Agent: "pi", Session: "s2", Provider: "a", Model: "m", Input: 1, Output: 1, Status: 200})
	var b strings.Builder
	if err := usageTo(&b, []string{"usage", "today"}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if n := strings.Count(out, "0.7 credits"); n < 4 { // the summary, the agent, the model and the session
		t.Fatalf("0.7 credits said %d times:\n%s", n, out)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "pi") && strings.Contains(l, "credits") {
			t.Fatalf("a row with no credits said says some: %q", l)
		}
	}
}
