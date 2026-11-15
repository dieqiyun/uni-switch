package gui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// #661: the site failing to give the notes (a 503 while GitHub limits it,
// and the update feed's newest without notes) is told apart from releases
// that have none, and the page gets the release page to read them on.
func TestWhatsNewTellsFailureFromNone(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	down := true
	notes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down {
			w.Header().Set("Cache-Control", "public, max-age=300")
			http.Error(w, `{"error":"no releases"}`, http.StatusServiceUnavailable)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"releases": []update.Note{}})
	}))
	defer notes.Close()
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(update.Release{Version: "0.1.737"})
	}))
	defer feed.Close()
	t.Setenv("UNI_SWITCH_NOTES_FEED", notes.URL)
	t.Setenv("UNI_SWITCH_UPDATE_FEED", feed.URL)
	old := Version
	defer func() { Version = old }()
	Version = "0.1.737"
	n := &whatsNew{}

	j := n.get(context.Background(), true, "en")
	if j.Error == "" || len(j.Releases) != 0 || j.URL != "https://github.com/dieqiyun/uni-switch/releases/tag/v0.1.737" {
		t.Fatalf("site failing: %+v", j)
	}
	// asked again once the site answers: no notes, and no error
	down = false
	j = n.get(context.Background(), true, "en")
	if j.Error != "" || len(j.Releases) != 0 || j.URL == "" {
		t.Fatalf("no notes: %+v", j)
	}
}
