package sessions

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// #1947 (XcodeFish): ZCode keeps the AI SDK's counts, input with the cache
// in it, and its calls went to the usage ledger as they were, so the
// Requests page, which adds the cache to the input for a hit rate, counted
// it twice: 50.8% for a run whose prompts were 96.9% cache. The rows are
// the reporter's: each prompt read back the one before it.
func TestZCodeCallInputLeavesOutCache(t *testing.T) {
	setupAgents(t)
	home, _ := os.UserHomeDir()
	db, err := sql.Open("sqlite", filepath.Join(home, ".zcode", "cli", "db", "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows := []struct{ input, read, write, output, reasoning int }{
		{75084, 0, 0, 300, 120},
		{79211, 75082, 0, 250, 0},
		{82649, 79209, 0, 410, 90},
		// the reporter's 275 calls, summed
		{43823215, 42480454, 0, 1000, 0},
		// a write beside the read leaves the input too
		{5000, 3000, 1500, 10, 0},
	}
	for i, r := range rows {
		at := int64(1790497000000 + i*10000)
		ocInsert(t, db, "ses_zcC", fmt.Sprintf("msg_zh%d", i), at, at+5000, fmt.Sprintf(`{"id":"msg_zh%d","role":"assistant","modelId":"glm-5.3-flash","providerID":"zai","tokens":{"input":%d,"output":%d,"reasoning":%d,"cache":{"read":%d,"write":%d}},"time":{"created":%d,"completed":%d}}`,
			i, r.input, r.output, r.reasoning, r.read, r.write, at, at+5000))
	}
	cs := zcodeCalls(t)
	for i, r := range rows {
		c, ok := cs[fmt.Sprintf("msg_zh%d", i)]
		want := Tokens{Input: r.input - r.read - r.write, Output: r.output, CacheRead: r.read, CacheWrite: r.write}
		if !ok || c.Tokens != want || c.Reasoning != r.reasoning {
			t.Errorf("row %d: %+v, want %+v", i, c.Tokens, want)
		}
	}
	// the page's hit rate, read / (input + write + read), is the
	// reporter's 96.9% again
	s := cs["msg_zh3"].Tokens
	if rate := float64(s.CacheRead) / float64(s.Input+s.CacheWrite+s.CacheRead); fmt.Sprintf("%.1f", rate*100) != "96.9" {
		t.Errorf("hit rate %.1f%%", rate*100)
	}
	// and a call holds what the session's summary holds for it: the
	// Sessions page and the Usage page tell the same input
	found := false
	for _, ss := range List(0) {
		if ss.Agent == "zcode" && ss.ID == "ses_zcC" {
			found = true
			in := ss.Input
			want := 10 // msg_z5's
			for _, r := range rows {
				want += r.input - r.read - r.write
			}
			if in != want {
				t.Errorf("the session's input %d, its calls' %d", in, want)
			}
		}
	}
	if !found {
		t.Error("no session ses_zcC")
	}
}
