package usage

import (
	"os"
	"strings"
	"testing"
	"time"
)

// A record carries its cache read and cache creation as it carries in and
// out: 0 is written too, so a call that missed the cache reads as that, not
// as one whose cache wasn't kept (#1839). A record from before they were
// written reads as none.
func TestRecordAlwaysCarriesCacheTokens(t *testing.T) {
	// a log of its own: at -count>1 the last run's lines would stand here
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	now := time.Now().UTC()
	Append(Record{Time: now, Agent: "claude", Provider: "p", Model: "m", Input: 3, Output: 900, Status: 200})
	Append(Record{Time: now.Add(time.Second), Agent: "claude", Provider: "p", Model: "m", Input: 3, Output: 900, CacheRead: 52000, CacheWrite: 400, Status: 200})
	b, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines: %q", lines)
	}
	for i, want := range [][2]string{{`"cache_read":0`, `"cache_write":0`}, {`"cache_read":52000`, `"cache_write":400`}} {
		for _, w := range want {
			if !strings.Contains(lines[i], w) {
				t.Errorf("record %d has no %s: %s", i, w, lines[i])
			}
		}
	}
	// an older record, without the fields, beside them
	old := `{"t":"` + now.Add(2*time.Second).Format(time.RFC3339Nano) + `","agent":"claude","provider":"p","model":"m","in":10,"out":5,"ms":9,"status":200}`
	if err := os.WriteFile(Path(), append(b, []byte(old+"\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	recs := Load(now.Add(-time.Minute))
	if len(recs) != 3 {
		t.Fatalf("records: %+v", recs)
	}
	if r := recs[1]; r.CacheRead != 52000 || r.CacheWrite != 400 {
		t.Errorf("cache read back: %+v", r)
	}
	if r := recs[2]; r.Input != 10 || r.CacheRead != 0 || r.CacheWrite != 0 {
		t.Errorf("an older record: %+v", r)
	}
}
