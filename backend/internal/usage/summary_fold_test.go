package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// foldRecord is the i-th call of a month of calls: three providers, two
// hosts and two accounts, keys, callers and sessions, at prices that sum
// to floats which an order of adding changes.
func foldRecord(now time.Time, i int) Record {
	r := Record{
		Time:     now.Add(-time.Duration(i%(31*24)) * time.Hour).Add(-time.Duration(i%60) * time.Minute),
		Agent:    []string{"codex", "claude", "opencode"}[i%3],
		Provider: []string{"openai", "relay", "glm"}[i%3],
		Model:    []string{"m", "m2"}[i%2],
		Session:  fmt.Sprintf("s%d-%d", i%3, i%97),
		Input:    10 + i%13, Output: 1 + i%7, CacheRead: i % 5,
		Status: 200, TTFT: int64(i % 300),
	}
	if i%3 == 1 {
		r.Host = "relay.example"
		r.ProviderKeyID, r.ProviderKeyName = fmt.Sprint("k", i%2), "key"
	}
	if i%3 == 2 {
		r.ProviderAccount = []string{"a@example.com", "b@example.com"}[i%2]
	}
	if i%11 == 0 {
		r.CallerKeyID, r.CallerKeyName = "ck", fmt.Sprint("caller ", i)
	}
	if i%17 == 0 {
		r.Status = 500
	}
	return r
}

func writeFoldLog(t *testing.T, now time.Time, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(Path()), 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(Path())
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for i := n - 1; i >= 0; i-- { // the oldest first, as the log has them
		if err := enc.Encode(foldRecord(now, i)); err != nil {
			f.Close()
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// A period's summary counted on from the blocks it counted before is the
// summary counted afresh, to the last bit of every float: as calls come in
// over a block's edge, when a provider's calls go to another host, when an
// account first answers for a provider and its calls are told apart by
// account from then on, in every block counted before too, and when the log
// is written over.
func TestSummaryCountedOnIsCountedAfresh(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local))
	writeFoldLog(t, now, 3*logBlockRows+100)
	periods := []Period{Today, Week, Month, All}
	carried := func() int {
		summaries.Lock()
		defer summaries.Unlock()
		return summaryPrefixes.carried
	}
	check := func(step string) {
		t.Helper()
		snapshot := readLogSnapshot()
		for _, p := range periods {
			got := SummarizeTop(p, -1)
			want := summarizeFrom(p.shown(), now, snapshot.first, snapshot.keyProviders, func(fn func(Record)) { snapshot.visit(p.Since(now), fn) })
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s, %s: counted on\n%+v\nafresh\n%+v", step, p, got.Totals, want.Totals)
			}
			if got.Calls == 0 {
				t.Fatalf("%s, %s: no calls", step, p)
			}
		}
	}
	check("first")
	before := carried()
	// over the edge of the last block, and a block more
	for i := 0; i < logBlockRows+50; i++ {
		r := foldRecord(now, i)
		r.Time = now.Add(-time.Duration(i) * time.Second)
		Append(r)
		if i == 0 || i == 600 {
			check(fmt.Sprint("appended ", i+1))
		}
	}
	check("appended a block")
	if n := carried() - before; n < len(periods) {
		t.Fatalf("only %d summaries counted on from what they had counted", n)
	}
	// a call to a host glm's calls never went to
	Append(Record{Time: now, Agent: "opencode", Provider: "glm", Model: "m", Host: "glm.other", Input: 5, Status: 200})
	check("a second host")
	// a provider an account first answers for is counted by account
	Append(Record{Time: now, Agent: "codex", Provider: "openai", Model: "m", ProviderAccount: "c@example.com", Input: 5, Status: 200})
	check("an account")
	for i := 0; i < logBlockRows; i++ {
		Append(foldRecord(now, i))
	}
	check("another block")
	writeFoldLog(t, now, 2*logBlockRows+7)
	check("written over")
}
