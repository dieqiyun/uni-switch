package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// Each new reading of an account changes the history this process wrote
// last, while the file is still that one: it isn't read and parsed again,
// nor parsed once more before it is written over. A file another process
// wrote since is read, and one that doesn't parse is still kept aside.
func TestQuotaHistoryWrittenNotReadAgain(t *testing.T) {
	testenv.SetHome(t, t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t0 := time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC)
	r5, rw := t0.Add(3*time.Hour), t0.Add(4*24*time.Hour)
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	reads := 0
	oldRead := readQuotaHist
	readQuotaHist = func() quotaHist {
		reads++
		return oldRead()
	}
	t.Cleanup(func() { readQuotaHist = oldRead })

	noteQuotaHistory([]SubscriptionQuota{histCard("A@x.com", 10, r5, 40, rw)}, at(0))
	for m := 10; m <= 60; m += 10 {
		noteQuotaHistory([]SubscriptionQuota{histCard("A@x.com", float64(m), r5, 40, rw)}, at(m))
	}
	if reads != 1 {
		t.Fatalf("six new readings read the history file %d times after the first", reads-1)
	}
	five := lineOf(t, QuotaHistories(t0.Add(-time.Hour), "", ""), "a@x.com", "5 hours")
	if len(five) != 7 || five[6].Left != 40 {
		t.Fatalf("5 hours points = %+v", five)
	}

	// another process writes the file: its history is read and kept
	path := quotaHistPath()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(filepath.Dir(path), "other.json")
	var external quotaHist
	if err := json.Unmarshal(b, &external); err != nil {
		t.Fatal(err)
	}
	external[quotaHistKey("codex", "b@x.com")] = map[string][]QuotaPoint{"5 hours": {{At: at(5), Left: 50}}}
	b, err = json.Marshal(external)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(other, path); err != nil {
		t.Fatal(err)
	}
	noteQuotaHistory([]SubscriptionQuota{histCard("A@x.com", 70, r5, 40, rw)}, at(70))
	hs := QuotaHistories(t0.Add(-time.Hour), "", "")
	if p := lineOf(t, hs, "a@x.com", "5 hours"); len(p) != 8 {
		t.Fatalf("a's points after another write = %+v", p)
	}
	if p := lineOf(t, hs, "b@x.com", "5 hours"); len(p) != 1 {
		t.Fatalf("b's points = %+v", p)
	}

	// a file that doesn't parse, written over ours, is kept aside first
	os.WriteFile(other, []byte(`{"cut`), 0o600)
	os.Rename(other, path)
	noteQuotaHistory([]SubscriptionQuota{histCard("A@x.com", 80, r5, 40, rw)}, at(80))
	if bad, _ := filepath.Glob(path + ".bad-*"); len(bad) != 1 {
		t.Fatalf("the unparsed file kept aside as %v", bad)
	}
}
