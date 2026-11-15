package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// Settings' What's new offers, besides the notes since the last update,
// this version and the 10, 20, 50 or 100 before it (Linka on Discord). The
// site is asked for that many alone, once per language and longest range:
// a shorter range, or the same again, is cut from what was had. A failed
// ask is said, with the shorter range had before; a range uni-switch doesn't
// offer is the default. The site's list is uni-switch-releases' shape, with
// 0.1.1407 never released and each note's Install section.
func TestWhatsNewBackRanges(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	var list []update.Note
	for v := 1420; v >= 1290; v-- {
		if v == 1407 {
			continue
		}
		s := strconv.Itoa(v)
		list = append(list, update.Note{
			Version: "0.1." + s,
			Notes:   "## Bug Fixes\n\n- fix " + s + "\n\n## Install\n\n- macOS: [uni-switch-0.1." + s + "-mac-arm64.dmg](https://github.com/dieqiyun/uni-switch/releases/download/v0.1." + s + "/uni-switch-0.1." + s + "-mac-arm64.dmg)",
		})
	}
	var mu sync.Mutex
	var asked []string
	down := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.RawQuery)
		failing := down
		mu.Unlock()
		if failing {
			http.Error(w, `{"error":"no releases"}`, http.StatusServiceUnavailable)
			return
		}
		// as the site answers: up to upto, newest first, at most limit
		var out []update.Note
		upto := r.URL.Query().Get("upto")
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		for _, n := range list {
			if !update.Newer(n.Version, upto) && (limit <= 0 || len(out) < limit) {
				out = append(out, n)
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"releases": out})
	}))
	defer srv.Close()
	t.Setenv("UNI_SWITCH_NOTES_FEED", srv.URL)
	t.Setenv("UNI_SWITCH_UPDATE_FEED", "http://127.0.0.1:1/")
	oldV, oldNews, oldUp := Version, news, updates
	t.Cleanup(func() { Version, news, updates = oldV, oldNews, oldUp })
	Version = "0.1.1410"
	news = &whatsNew{after: "0.1.1408", pending: true, shown: true}
	updates = &updater{state: "latest", notesIn: "en"}
	mux := http.NewServeMux()
	whatsNewRoutes(mux)
	ask := func(q string) (whatsNewJSON, []string) {
		mu.Lock()
		asked = nil
		mu.Unlock()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/whatsnew?all=1&lang=en"+q, nil))
		var j whatsNewJSON
		if err := json.Unmarshal(rec.Body.Bytes(), &j); err != nil {
			t.Fatal(rec.Code, rec.Body.String(), err)
		}
		mu.Lock()
		defer mu.Unlock()
		return j, slices.Clone(asked)
	}
	versions := func(j whatsNewJSON) []string {
		out := []string{}
		for _, r := range j.Releases {
			out = append(out, r.Version)
		}
		return out
	}
	// the n releases before 0.1.1410, newest first, 0.1.1407 skipped
	before := func(n int) []string {
		out := []string{}
		for _, r := range list {
			if !update.Newer(r.Version, Version) && len(out) < n+1 {
				out = append(out, r.Version)
			}
		}
		return out
	}

	// the default is as it was: the notes since the last update
	j, _ := ask("")
	if got := versions(j); !slices.Equal(got, []string{"0.1.1410", "0.1.1409"}) {
		t.Fatalf("default: %v", got)
	}
	// 10 before: eleven releases, from the site's 11 alone, Install out
	j, got := ask("&last=10")
	if !slices.Equal(got, []string{"lang=en&limit=11&upto=0.1.1410"}) {
		t.Fatalf("10 asked the site %q", got)
	}
	if !slices.Equal(versions(j), before(10)) || j.Error != "" {
		t.Fatalf("10: %v (%q), want %v", versions(j), j.Error, before(10))
	}
	if j.Releases[0].Notes != "## Bug Fixes\n\n- fix 1410" {
		t.Fatalf("10: Install kept: %q", j.Releases[0].Notes)
	}
	// the same again: not asked again
	if j, got = ask("&last=10"); len(got) != 0 || len(j.Releases) != 11 {
		t.Fatalf("10 again asked %q, %d releases", got, len(j.Releases))
	}
	// 100, past MaxNotes: asked once, 101 releases; then 20 and 50 are cut
	// from it, without asking
	if j, got = ask("&last=100"); len(got) != 1 || !slices.Equal(versions(j), before(100)) {
		t.Fatalf("100 asked %q, got %v", got, versions(j))
	}
	for _, n := range []int{20, 50, 10} {
		if j, got = ask("&last=" + strconv.Itoa(n)); len(got) != 0 || !slices.Equal(versions(j), before(n)) {
			t.Fatalf("%d after 100 asked %q, got %v", n, got, versions(j))
		}
	}
	// a range uni-switch doesn't offer: the default, the site not asked for it
	for _, q := range []string{"&last=7", "&last=x", "&last=1000"} {
		if j, _ = ask(q); !slices.Equal(versions(j), []string{"0.1.1410", "0.1.1409"}) {
			t.Fatalf("%s: %v", q, versions(j))
		}
	}

	// a fresh run: the site failing says so, keeping
	// the shorter range had before
	news = &whatsNew{}
	if j, _ = ask("&last=20"); len(j.Releases) != 21 {
		t.Fatalf("fresh 20: %d", len(j.Releases))
	}
	mu.Lock()
	down = true
	mu.Unlock()
	j, got = ask("&last=50")
	if len(got) != 1 || j.Error == "" {
		t.Fatalf("50 with the site down: asked %q, error %q", got, j.Error)
	}
	if !slices.Equal(versions(j), before(20)) {
		t.Fatalf("50 with the site down: %v, want the 20 had", versions(j))
	}
	// and once it answers, asked again: the 50
	mu.Lock()
	down = false
	mu.Unlock()
	if j, got = ask("&last=50"); len(got) != 1 || j.Error != "" || !slices.Equal(versions(j), before(50)) {
		t.Fatalf("50 back: asked %q, error %q, %v", got, j.Error, versions(j))
	}
	// nothing had at all and the site failing: an error, no notes
	news = &whatsNew{}
	mu.Lock()
	down = true
	mu.Unlock()
	if j, _ = ask("&last=10"); j.Error == "" || len(j.Releases) != 0 || j.URL == "" {
		t.Fatalf("down from the start: %+v", j)
	}
	// a build from source has no releases to list
	Version = "dev"
	if j, got = ask("&last=10"); len(got) != 0 || len(j.Releases) != 0 {
		t.Fatalf("dev: asked %q, %v", got, versions(j))
	}
}
