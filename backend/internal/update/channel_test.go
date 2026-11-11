package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// stableFor saves Settings' Update channel for one test and puts the one
// before back after it.
func stableFor(t *testing.T, channel string) {
	t.Helper()
	before := settings.Load()
	s := before
	s.UpdateChannel = channel
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { settings.Save(before) })
}

// The Stable channel (ssharpcc on X): the feed is asked with
// channel=stable, and only then; every version asks it as before. The
// language goes with it either way.
func TestLatestAsksTheChannel(t *testing.T) {
	var (
		mu    sync.Mutex
		asked []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.RawQuery)
		mu.Unlock()
		v := "0.1.1600"
		if r.URL.Query().Get("channel") == "stable" {
			v = "0.1.1550"
		}
		json.NewEncoder(w).Encode(Release{Version: v})
	}))
	defer srv.Close()
	t.Setenv("UNI_SWITCH_UPDATE_FEED", srv.URL+"/api/latest")
	ctx := context.Background()

	for _, c := range []struct{ channel, lang, query, version string }{
		{"", "zh", "lang=zh", "0.1.1600"},
		{"", "", "", "0.1.1600"},
		{settings.UpdateStable, "zh", "channel=stable&lang=zh", "0.1.1550"},
		{settings.UpdateStable, "", "channel=stable", "0.1.1550"},
	} {
		t.Run(fmt.Sprintf("%q %q", c.channel, c.lang), func(t *testing.T) {
			stableFor(t, c.channel)
			mu.Lock()
			asked = nil
			mu.Unlock()
			rel, err := LatestIn(ctx, c.lang)
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(asked) != 1 || asked[0] != c.query {
				t.Fatalf("asked %q, want %q", asked, c.query)
			}
			if rel.Version != c.version {
				t.Fatalf("got %s, want %s", rel.Version, c.version)
			}
		})
	}
}

// What's new after a Stable update is every release since the one
// installed, read as one list: the cap of 40 is every version's only.
func TestStableNotesAreAll(t *testing.T) {
	var notes []Note
	for i := 1; i <= 120; i++ {
		notes = append(notes, Note{Version: fmt.Sprintf("0.1.%d", 1000+i), Notes: fmt.Sprintf("- change %d", i)})
	}
	stableFor(t, "")
	if got := Between(notes, "0.1.1000", "0.1.1120"); len(got) != MaxNotes || got[0].Version != "0.1.1120" {
		t.Fatalf("every version: %d notes, first %s", len(got), got[0].Version)
	}
	stableFor(t, settings.UpdateStable)
	got := Between(notes, "0.1.1000", "0.1.1120")
	if len(got) != 120 || got[0].Version != "0.1.1120" || got[119].Version != "0.1.1001" {
		t.Fatalf("stable: %d notes", len(got))
	}
	if MostNotes() != StableMaxNotes {
		t.Fatalf("MostNotes on stable: %d", MostNotes())
	}
}

// Settings take "stable" or nothing; anything else is refused, and the
// value is kept as saved.
func TestUpdateChannelSaved(t *testing.T) {
	stableFor(t, settings.UpdateStable)
	if got := settings.Load().UpdateChannel; got != "stable" || !Stable() {
		t.Fatalf("saved stable, read %q", got)
	}
	for _, bad := range []string{"Stable", "beta", "every", " stable"} {
		s := settings.Load()
		s.UpdateChannel = bad
		if err := settings.Save(s); err == nil {
			t.Errorf("%q was saved", bad)
		}
	}
	if got := settings.Load().UpdateChannel; got != "stable" {
		t.Fatalf("a refused save changed it: %q", got)
	}
}
