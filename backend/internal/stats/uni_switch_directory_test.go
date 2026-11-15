package stats

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func TestUniSwitchNoPartnerTelemetry(t *testing.T) {
	t.Setenv("UNI_SWITCH_DATA_DIR", t.TempDir())
	t.Setenv("DO_NOT_TRACK", "")
	t.Setenv("UNI_SWITCH_NO_STATS", "")
	if err := settings.Save(settings.Settings{}); err != nil {
		t.Fatal(err)
	}
	oldUsage := usage
	usage = func() Usage { return Usage{} }
	t.Cleanup(func() { usage = oldUsage })
	cache := filepath.Join(appdir.Cache(), "partner-counts.json")
	if err := os.MkdirAll(filepath.Dir(cache), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, []byte(`{"days":{"2026-09-27":{"old-partner":{"shown":12}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var events []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Batch []struct{ Event string } `json:"batch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		for _, event := range body.Batch {
			events = append(events, event.Event)
		}
		mu.Unlock()
	}))
	defer srv.Close()
	t.Setenv("UNI_SWITCH_STATS_HOST", srv.URL)
	if err := Send(context.Background(), "development", "app", time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 || events[0] != "uni-switch active" {
		t.Fatalf("old partner counts were sent: %v", events)
	}
}
