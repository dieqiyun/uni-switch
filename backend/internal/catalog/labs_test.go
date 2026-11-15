package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The catalogs as models.dev serves them on 10-27, cut to Claude: its
// anthropic and azure providers (api.json) and the anthropic lab
// (models.json). models.dev took the gated Claude Mythos 5.1 out of
// anthropic's provider list (#11327) and kept its lab file (#11328).
const (
	claudeAPI10_27  = "testdata/models.dev-2026-10-27-claude.json"
	claudeLabs10_27 = "testdata/models.dev-labs-2026-10-27-anthropic.json"
)

func useCatalogs(t *testing.T, api, labs string) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Cleanup(Reset)
	for src, dst := range map[string]string{api: CachePath(), labs: LabsPath()} {
		if src == "" {
			continue
		}
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		os.MkdirAll(filepath.Dir(dst), 0o755)
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	Reset()
}

// A model models.dev keeps under its lab alone is still known: an account
// or a vendor's list that has Claude Mythos 5.1 gets its name, 1M window,
// reply limit, thinking and images from the lab file, and Mythos 5's
// levels, the newest of its family any provider gives (iebb on Discord).
func TestLabOnlyModelIsKnown(t *testing.T) {
	useCatalogs(t, claudeAPI10_27, claudeLabs10_27)
	for _, m := range Provider("anthropic") {
		if m.ID == "claude-mythos-5-1" {
			t.Fatal("fixture: models.dev's anthropic list has Mythos 5.1 again")
		}
	}
	const id = "claude-mythos-5-1"
	if got := NameOf(id); got != "Claude Mythos 5.1" {
		t.Errorf("NameOf = %q", got)
	}
	if got := ContextOf(id); got != 1_000_000 {
		t.Errorf("ContextOf = %d", got)
	}
	if got := OutputOf(id); got != 128_000 {
		t.Errorf("OutputOf = %d", got)
	}
	if !Thinks(id) || !SeesImages(id) {
		t.Errorf("Thinks %v, SeesImages %v", Thinks(id), SeesImages(id))
	}
	want := EffortsOf("claude-mythos-5")
	if len(want) == 0 {
		t.Fatal("fixture: Mythos 5 has no levels")
	}
	if got := EffortsOf(id); !slices.Equal(got, want) {
		t.Errorf("EffortsOf = %v, want Mythos 5's %v", got, want)
	}
	// a vendor prefix and Claude Code's [1m] reach it as they reach any
	for _, as := range []string{"anthropic/claude-mythos-5-1", "claude-mythos-5-1[1m]"} {
		if got := ContextOf(as); got != 1_000_000 {
			t.Errorf("ContextOf(%q) = %d", as, got)
		}
	}

	d := Describe(id, "")
	if d.Name != "Claude Mythos 5.1" || d.Context != 1_000_000 || d.Output != 128_000 ||
		!d.Reasoning || !slices.Equal(d.Efforts, want) || !d.Images || d.Released != "2026-09-01" ||
		d.Temperature == nil || *d.Temperature || d.Price != nil {
		t.Errorf("Describe = %+v", d)
	}
	if d := Describe(id, "Mythos (account)"); d.Name != "Mythos (account)" {
		t.Errorf("Describe kept no list name: %q", d.Name)
	}
}

// A model a provider lists keeps what providers say of it: its lab file
// adds nothing to it, and without a lab catalog the catalog answers as
// before.
func TestLabsLeaveListedModels(t *testing.T) {
	useCatalogs(t, claudeAPI10_27, "")
	want := map[string]int{}
	for _, m := range Provider("anthropic") {
		want[m.ID] = ContextOf(m.ID)
	}
	if ContextOf("claude-mythos-5-1") != 0 {
		t.Fatal("Mythos 5.1 known without a lab catalog")
	}
	useCatalogs(t, claudeAPI10_27, claudeLabs10_27)
	for id, w := range want {
		if got := ContextOf(id); got != w {
			t.Errorf("%s: window %d with labs, %d without", id, got, w)
		}
	}
}

// Sync keeps models.dev's lab catalog beside its provider catalog, and a
// lab fetch that fails keeps the one it had.
func TestSyncKeepsLabs(t *testing.T) {
	api, _ := os.ReadFile(claudeAPI10_27)
	labs, _ := os.ReadFile(claudeLabs10_27)
	labsDown := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api.json":
			w.Write(api)
		case "/models.json":
			if labsDown {
				http.Error(w, "down", http.StatusBadGateway)
				return
			}
			w.Write(labs)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	useCatalogs(t, "", "")
	was := modelsDevURL
	modelsDevURL = srv.URL + "/api.json"
	t.Cleanup(func() { modelsDevURL = was; labsTried.Store(false) })

	labsTried.Store(false)
	if err := Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := ContextOf("claude-mythos-5-1"); got != 1_000_000 {
		t.Fatalf("after Sync, Mythos 5.1's window = %d", got)
	}
	if Stale() {
		t.Error("a catalog just synced with its labs is stale")
	}

	labsDown = true
	if err := Sync(context.Background()); err != nil {
		t.Fatal("a lab fetch failing failed Sync:", err)
	}
	if got := ContextOf("claude-mythos-5-1"); got != 1_000_000 {
		t.Errorf("a failed lab fetch lost the kept labs: window %d", got)
	}

	// a cache kept before uni-switch read labs is fetched again at once
	os.Remove(LabsPath())
	labsTried.Store(false)
	if !Stale() {
		t.Error("a catalog without its labs isn't stale")
	}
	labsDown = false
	if err := Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if Stale() {
		t.Error("stale after the labs came")
	}
}
