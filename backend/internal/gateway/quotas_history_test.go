package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// GET /v1/uni-switch/quotas/history is each account's windows over time
// (#651): the points of the last ?days=, ?provider= and ?user= picking
// one account's, and another machine without the shared key gets nothing.
func TestQuotasHistory(t *testing.T) {
	fresh(t)
	t.Setenv("UNI_SWITCH_ADDR", "")
	now := time.Now().UTC().Truncate(time.Second)
	ago := func(h int) string { return now.Add(-time.Duration(h) * time.Hour).Format(time.RFC3339) }
	seed := fmt.Sprintf(`{"codex|a@x.com":{"5 hours":[{"at":%q,"left":90},{"at":%q,"left":70}]},
		"claude|":{"Weekly":[{"at":%q,"left":40}]}}`, ago(100), ago(1), ago(2))
	dir := filepath.Dir(provider.Path())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "quota-history.json"), []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	h := localGuard(New().Handler())
	get := func(from, q string) (int, []provider.QuotaHistory) {
		r := httptest.NewRequest("GET", "/v1/uni-switch/quotas/history"+q, nil)
		r.RemoteAddr = from
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var out struct{ Data []provider.QuotaHistory }
		json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out.Data
	}
	c, all := get("127.0.0.1:5000", "")
	if c != 200 || len(all) != 2 {
		t.Fatalf("all: %d %+v", c, all)
	}
	_, codex := get("127.0.0.1:5000", "?provider=codex&user=A@x.com&days=2")
	if len(codex) != 1 || codex[0].User != "a@x.com" || len(codex[0].Lines[0].Points) != 2 {
		t.Fatalf("codex, 2 days (the point the range begins in, and the last): %+v", codex)
	}
	if c, _ := get("192.168.1.9:5000", ""); c == http.StatusOK {
		t.Fatal("another machine without a key got the history")
	}
}
