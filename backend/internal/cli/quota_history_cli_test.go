package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// stdoutOf is what f prints.
func stdoutOf(t *testing.T, f func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan []byte)
	go func() { b, _ := io.ReadAll(r); done <- b }()
	err = f()
	w.Close()
	os.Stdout = old
	return string(<-done), err
}

// uni-switch quota history tells each account's windows over time as uni-switch
// kept them, narrowed to a provider or an account and to --days; --json is
// what GET /v1/uni-switch/quotas/history answers (#802).
func TestQuotaHistoryCmd(t *testing.T) {
	groupsHome(t)
	now := time.Now().UTC().Truncate(time.Second)
	reset := now.Add(2 * time.Hour)
	pt := func(ago time.Duration, left float64, r time.Time) provider.QuotaPoint {
		return provider.QuotaPoint{At: now.Add(-ago), Left: left, ResetsAt: &r}
	}
	h := map[string]map[string][]provider.QuotaPoint{
		"codex|a@x.com":  {"5 hours": {pt(30*time.Hour, 40, now.Add(-26*time.Hour)), pt(3*time.Hour, 100, reset), pt(time.Hour, 72.5, reset)}},
		"claude|b@x.com": {"Weekly": {pt(2*time.Hour, 55, now.Add(72*time.Hour))}},
	}
	b, _ := json.Marshal(h)
	if err := os.WriteFile(filepath.Join(filepath.Dir(provider.Path()), "quota-history.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := stdoutOf(t, func() error { return quotaCmd([]string{"quota", "history", "codex", "--days", "1", "--json"}) })
	if err != nil {
		t.Fatal(err)
	}
	var hs []provider.QuotaHistory
	if err := json.Unmarshal([]byte(out), &hs); err != nil {
		t.Fatal(err, out)
	}
	// the day's two, and the one before so the line reaches its edge
	if len(hs) != 1 || hs[0].Provider != "codex" || hs[0].User != "a@x.com" || len(hs[0].Lines) != 1 || len(hs[0].Lines[0].Points) != 3 {
		t.Fatalf("codex, a day: %s", out)
	}
	if p := hs[0].Lines[0].Points[2]; !p.At.Equal(now.Add(-time.Hour)) || p.Left != 72.5 || !p.ResetsAt.Equal(reset) {
		t.Errorf("last point = %+v", p)
	}

	out, err = stdoutOf(t, func() error { return quotaCmd([]string{"quota", "history", "B@x.com", "--json"}) })
	if err != nil || !strings.Contains(out, `"provider": "claude"`) || strings.Contains(out, "codex") {
		t.Fatalf("by account: %v %s", err, out)
	}

	out, err = stdoutOf(t, func() error { return quotaCmd([]string{"quota", "history"}) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"codex · a@x.com", "claude · b@x.com", "5 hours", "72.5%", "↻ ", "Weekly", "55%"} {
		if !strings.Contains(out, want) {
			t.Errorf("text lacks %q:\n%s", want, out)
		}
	}

	if _, err := stdoutOf(t, func() error { return quotaCmd([]string{"quota", "history", "nobody"}) }); err == nil {
		t.Error("an account with nothing kept wasn't an error")
	}
	if _, err := stdoutOf(t, func() error { return quotaCmd([]string{"quota", "history", "--days", "x"}) }); err == nil {
		t.Error("--days x wasn't an error")
	}
}
