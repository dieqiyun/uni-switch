package gui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// A route kept before usage.AgentOf knew Kimi Code 2.x's User-Agent
// ("kimi-code-cli/…") carries that raw name. The ledger names its records
// again as it reads them, so the Usage page's request list showed Kimi
// Code; the Routing page's request list and the Context tab's agent cards
// read the kept routes and drew a "kimi-code-cli" of their own with the
// cube (jiabubu on Discord). Both read the routes as AgentOf names them
// now: "kimi", Kimi Code's id, which /api/state's clients give its icon.
func TestKeptRoutesNameTheirAgentAgain(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	mux := http.NewServeMux()
	traceRoutes(mux)
	dir := gateway.HistoryDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	day := now.Format("2006-01-02")
	var b bytes.Buffer
	for i, agent := range []string{"kimi-code-cli", "kimi", "kimi-code-desktop", "claude-desktop", "Python-urllib"} {
		r := ctxRoute(int64(i+1), agent, "s", 30000, 1000, 200000, 0)
		r.Time = now.Add(-time.Duration(5-i) * time.Minute)
		j, _ := json.Marshal(r)
		b.Write(append(j, '\n'))
	}
	if err := os.WriteFile(filepath.Join(dir, day+".jsonl"), b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	var hist struct {
		Routes []gateway.Route `json:"routes"`
	}
	getJSON(t, mux, "/api/gateway/history?day="+day, &hist)
	var got []string
	for _, r := range hist.Routes {
		got = append(got, r.Agent)
	}
	want := []string{"kimi", "kimi", "kimi-code-desktop", "claude-desktop", "Python-urllib"}
	if len(got) != len(want) {
		t.Fatalf("history's agents %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("history's agents %v, want %v", got, want)
		}
	}

}
