package usage

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func reportedHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	os.WriteFile(catalog.CachePath(), []byte(`{"anthropic":{"id":"anthropic","models":{"claude-opus-5-5":{"id":"claude-opus-5-5","cost":{"input":4,"output":20,"cache_read":0.2,"cache_write":5}}}}}`), 0o644)
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	forgetReported()
	t.Cleanup(forgetReported)
}

// forgetReported is a restart, as far as what was reported goes.
func forgetReported() {
	reported.ingest.Lock()
	reported.path, reported.info, reported.off, reported.rids = "", nil, 0, nil
	reported.ingest.Unlock()
}

// A reported call (#1874) is written beside the gateway's own, marked
// reported: priced at uni-switch's prices when it said no cost, at its own
// when it did; counted in the totals and in a source of its own, which
// via=reported narrows to and the gateway's "through" leaves out. A rid
// already reported is skipped, in one batch, in a later one, and after a
// restart, which reads the log again.
func TestIngestReported(t *testing.T) {
	reportedHome(t)
	now := time.Now().Add(-time.Hour)
	Append(Record{Time: now.Add(-time.Minute), Agent: "claude", Provider: "claude", Model: "claude-opus-5-5", Input: 1000, Output: 100, Status: 200, RequestID: "gw-1"})
	own := 0.25
	recs := []Record{
		{Time: now, Agent: "my-app", Provider: "claude", Model: "claude-opus-5-5", Input: 1_000_000, Output: 100_000, CacheRead: 1_000_000, Status: 200, RequestID: "r-1"},
		{Time: now.Add(time.Second), Agent: "my-app", Provider: "claude", Model: "claude-opus-5-5", Input: 10, Output: 1, Status: 200, RequestID: "r-2", OwnCost: &own},
		{Time: now.Add(2 * time.Second), Agent: "my-app", Provider: "claude", Model: "claude-opus-5-5", Input: 10, Output: 1, Status: 200, RequestID: "r-1"},
		// a gateway call's rid is no reported call's
		{Time: now.Add(3 * time.Second), Agent: "my-app", Provider: "nowhere", Model: "mystery", Input: 5, Output: 5, Status: 200, RequestID: "gw-1"},
	}
	took, err := IngestReported(recs)
	if err != nil || fmt.Sprint(took) != "[true true false true]" {
		t.Fatal(took, err)
	}
	if took, err := IngestReported(recs[:2]); err != nil || fmt.Sprint(took) != "[false false]" {
		t.Fatal("again:", took, err)
	}
	forgetReported()
	if took, err := IngestReported(append(recs[:2:2], Record{Time: now, Agent: "my-app", Provider: "claude", Model: "x", Status: 200, RequestID: "r-3"})); err != nil || fmt.Sprint(took) != "[false false true]" {
		t.Fatal("after a restart:", took, err)
	}
	b, _ := os.ReadFile(Path())
	if n := bytes.Count(b, []byte(`"source":"reported"`)); n != 4 {
		t.Fatalf("%d reported lines:\n%s", n, b)
	}

	priced := (1e6*4 + 1e5*20 + 1e6*0.2) / 1e6
	page := QueryPage(All, Filter{}, 0, 50)
	if page.Total != 5 || page.Reported.Calls != 4 || page.Through.Calls != 1 || page.Direct.Calls != 0 {
		t.Fatalf("total %d, reported %+v, through %+v", page.Total, page.Reported, page.Through)
	}
	byRID := map[string]Row{}
	for _, r := range page.Rows {
		byRID[r.RequestID+"/"+r.Source] = r
	}
	if r := byRID["r-1/reported"]; !r.Priced || math.Abs(r.Cost-priced) > 1e-9 || SourceOf(r) != SourceReported {
		t.Fatalf("priced at uni-switch's prices: %+v", r)
	}
	if r := byRID["r-2/reported"]; !r.Priced || r.Cost != own || r.OwnCost == nil || *r.OwnCost != own {
		t.Fatalf("its own cost: %+v", r)
	}
	if r := byRID["gw-1/reported"]; r.Priced || r.Cost != 0 {
		t.Fatalf("an unknown model is unpriced: %+v", r)
	}
	if r := byRID["gw-1/"]; r.Agent != "claude" || SourceOf(r) != SourceGateway {
		t.Fatalf("the gateway's own: %+v", r)
	}
	if want := priced + own + (1000*4+100*20)/1e6; math.Abs(page.Sum.Cost-want) > 1e-9 {
		t.Fatalf("sum cost %v, want %v", page.Sum.Cost, want)
	}
	if p := QueryPage(All, Filter{Through: SourceReported}, 0, 50); p.Total != 4 || p.Reported.Calls != 4 || p.Through.Calls != 1 {
		t.Fatalf("via=reported: %d rows, %+v / %+v", p.Total, p.Reported, p.Through)
	}
	if p := QueryPage(All, Filter{Through: SourceGateway}, 0, 50); p.Total != 1 || p.Rows[0].RequestID != "gw-1" {
		t.Fatalf("via=through: %+v", p.Rows)
	}
	if s := Summarize(All); s.Calls != 5 || math.Abs(s.Cost-page.Sum.Cost) > 1e-9 {
		t.Fatalf("summary %d calls, cost %v; the page %v", s.Calls, s.Cost, page.Sum.Cost)
	}
	if rows, _, _ := Ledger(All, Filter{Through: SourceReported}); len(rows) != 4 {
		t.Fatalf("ledger via=reported: %d", len(rows))
	}
}

// Posts at once, and the gateway's own calls between them, keep every
// line whole, and a rid posted by several at once is written once.
func TestIngestReportedConcurrent(t *testing.T) {
	reportedHome(t)
	now := time.Now().Add(-time.Hour)
	const posts, each = 16, 40
	var wg sync.WaitGroup
	accepted := make([]int, posts)
	for p := range posts {
		wg.Add(2)
		go func() {
			defer wg.Done()
			var recs []Record
			for i := range each {
				// half the rids every post sends, half its own
				rid := fmt.Sprintf("shared-%d", i)
				if i%2 == 1 {
					rid = fmt.Sprintf("own-%d-%d", p, i)
				}
				recs = append(recs, Record{Time: now, Agent: "my-app", Provider: "claude", Model: "claude-opus-5-5", Input: 1, Output: 1, Status: 200, RequestID: rid})
			}
			took, err := IngestReported(recs)
			if err != nil {
				t.Error(err)
			}
			for _, ok := range took {
				if ok {
					accepted[p]++
				}
			}
		}()
		go func() {
			defer wg.Done()
			Append(Record{Time: now, Agent: "claude", Provider: "claude", Model: "claude-opus-5-5", Input: 1, Output: 1, Status: 200})
		}()
	}
	wg.Wait()
	total := 0
	for _, n := range accepted {
		total += n
	}
	if want := each/2 + posts*each/2; total != want {
		t.Fatalf("accepted %d, want %d", total, want)
	}
	recs := Load(time.Time{})
	if len(recs) != total+posts {
		t.Fatalf("%d lines, want %d", len(recs), total+posts)
	}
	seen := map[string]bool{}
	for _, r := range recs {
		if r.Source != SourceReported {
			continue
		}
		if seen[r.RequestID] {
			t.Fatal("written twice:", r.RequestID)
		}
		seen[r.RequestID] = true
	}
}

// What apps report (#1874), backfilled or not, is shown on the Usage page
// and nowhere that acts on live traffic: not as a gateway session, not in
// what a session was routed to, not in a subscription window's holds, and a
// backfill reaching back doesn't make the log reach back to a window's start.
func TestReportedTouchesNothingLive(t *testing.T) {
	reportedHome(t)
	now := time.Now()
	gw := Record{Time: now.Add(-time.Hour), Agent: "claude", Provider: "claude", ProviderAccount: "dee@example.com", Model: "claude-opus-5-5", Input: 100, Output: 100, Status: 200, Session: "s-gw"}
	Append(gw)
	if took, err := IngestReported([]Record{
		{Time: now.Add(-30 * 24 * time.Hour), Agent: "claude", Provider: "claude", Model: "claude-opus-5-5", Input: 9000, Output: 9000, Status: 200, RequestID: "old", Session: "s-rep"},
		{Time: now.Add(-2 * time.Hour), Agent: "claude", Provider: "claude", Model: "claude-opus-5-5", Input: 9000, Output: 9000, Status: 200, RequestID: "new", Session: "s-rep"},
	}); err != nil || fmt.Sprint(took) != "[true true]" {
		t.Fatal(took, err)
	}
	// a row that says it was reported and names an account, as an older or
	// hand-made line might
	Append(Record{Time: now.Add(-3 * time.Hour), Agent: "claude", Provider: "claude", ProviderAccount: "dee@example.com", Model: "claude-opus-5-5", Input: 9000, Output: 9000, Status: 200, RequestID: "acct", Source: SourceReported})

	s := readLogSnapshot()
	if math.Abs(s.firstOwn.Sub(gw.Time).Seconds()) > 1e-3 {
		t.Fatalf("the log's own first call %v, want %v", s.firstOwn, gw.Time)
	}
	if s.first.After(now.Add(-29 * 24 * time.Hour)) {
		t.Fatalf("the chart's first call %v is after the backfill", s.first)
	}
	for _, g := range GatewaySessions(time.Time{}, nil) {
		if g.ID == "s-rep" {
			t.Fatalf("a reported session is a gateway session: %+v", g)
		}
	}
	if v := Vias(time.Time{}).Of("claude|s-rep"); len(v) != 0 {
		t.Fatalf("a reported session was routed: %+v", v)
	}
	if !(Record{Source: SourceReported}).IsReported() || (Record{}).IsReported() {
		t.Fatal("IsReported")
	}
	reset := now.Add(3 * 24 * time.Hour)
	qs := []provider.SubscriptionQuota{{Provider: "claude", User: "dee@example.com", Windows: []provider.QuotaWindow{
		{Name: "7 days", Used: 40, ResetsAt: &reset, Span: 7 * 24 * time.Hour}}}}
	if h := WithWindowHolds(qs, now)[0].Windows[0].Holds; h != nil {
		t.Fatalf("the gateway's log began an hour ago, and the week was reckoned: %+v", h)
	}
	// with the gateway's own log reaching back, the week counts its call only
	Append(Record{Time: now.Add(-20 * 24 * time.Hour), Agent: "claude", Provider: "claude", Model: "claude-opus-5-5", Input: 1, Output: 1, Status: 200})
	h := WithWindowHolds(qs, now)[0].Windows[0].Holds
	if h == nil || h.Routed.Calls != 1 || h.Routed.Tokens != 200 {
		t.Fatalf("the week holds %+v, want the gateway's one call", h)
	}
}
