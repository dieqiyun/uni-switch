package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// The Routing page's accounts and keys counted each account's tried and
// answered over the last few requests the gateway keeps in memory, while
// the day tabs right above it counted the day's whole history — the two
// pages could disagree by orders of magnitude (#3351). The history read
// the live page already polls now carries the day's tallies, counted from
// the same files the Usage page reads, so its counts cover the day and
// not the last sixty requests.
func TestLiveHistoryTalliesTheWholeDay(t *testing.T) {
	sandboxHome(t)
	old := served.Swap(nil)
	t.Cleanup(func() { served.Store(old) })
	if err := os.MkdirAll(gateway.HistoryDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(rs []gateway.Route) {
		t.Helper()
		f, err := os.OpenFile(filepath.Join(gateway.HistoryDir(), time.Now().Local().Format("2006-01-02")+".jsonl"),
			os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		enc := json.NewEncoder(f)
		for _, r := range rs {
			if enc.Encode(r) != nil {
				t.Fatal("a route the gateway would have written")
			}
		}
		f.Close()
	}
	// ninety requests weighed and tried on one account, then ten on
	// another: the trace keeps the last sixty alone, so the live page's
	// own counting could only ever name a fraction of the busy one's day
	start := time.Now().Local().Add(-2 * time.Hour).Truncate(time.Second)
	var rs []gateway.Route
	for i := 0; i < 100; i++ {
		w := gateway.Weighed{ID: "p/A", Provider: "p", Name: "P", Kind: "account", Who: "A", Model: "m"}
		if i >= 90 {
			w = gateway.Weighed{ID: "p/B", Provider: "p", Name: "P", Kind: "account", Who: "B", Model: "m"}
		}
		try := gateway.Try{ID: w.ID, Start: start.Add(time.Duration(i) * time.Minute), Done: true, Millis: 100, Status: 200}
		if i == 0 || i == 45 { // two of the busy account's tries were turned away
			try.Status, try.Fail = 429, "rate"
		}
		rs = append(rs, gateway.Route{ID: int64(i + 1), Time: try.Start, Done: true,
			Model: "m", Provider: "p", Order: []gateway.Weighed{w}, Tries: []gateway.Try{try}})
	}
	write(rs)
	ask := func() map[string]struct {
		Tried int            `json:"tried"`
		OK    int            `json:"ok"`
		Fails map[string]int `json:"fails"`
	} {
		mux := http.NewServeMux()
		traceRoutes(mux)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/gateway/history?day=", nil))
		var out struct {
			Tallies map[string]struct {
				Tried int            `json:"tried"`
				OK    int            `json:"ok"`
				Fails map[string]int `json:"fails"`
			} `json:"tallies"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("the history read: %v %s", err, w.Body)
		}
		return out.Tallies
	}
	// the page groups an account by its provider and its user, however it
	// is weighed into a request; the tallies speak the same keying
	got := ask()
	busy, quiet := got[`["p","a"]`], got[`["p","b"]`]
	if busy.Tried != 90 || busy.OK != 88 || busy.Fails["rate"] != 2 {
		t.Errorf("the busy account's day: tried %d, answered %d, fails %v", busy.Tried, busy.OK, busy.Fails)
	}
	if quiet.Tried != 10 || quiet.OK != 10 {
		t.Errorf("the quiet account's day: tried %d, answered %d", quiet.Tried, quiet.OK)
	}
	// a day that has grown since is counted again, not served stale
	try := gateway.Try{ID: "p/B", Start: start.Add(3 * time.Hour), Done: true, Millis: 50, Status: 200}
	write([]gateway.Route{{ID: 101, Time: try.Start, Done: true, Model: "m", Provider: "p",
		Order: []gateway.Weighed{{ID: "p/B", Provider: "p", Name: "P", Kind: "account", Who: "B", Model: "m"}},
		Tries: []gateway.Try{try}}})
	if got = ask(); got[`["p","b"]`].Tried != 11 {
		t.Errorf("the day grown, the quiet account's day: tried %d", got[`["p","b"]`].Tried)
	}
}
