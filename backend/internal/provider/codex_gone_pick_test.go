package provider

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// #4772: the owner picked Codex's models while ChatGPT listed gpt-5.5.
// ChatGPT then stopped listing it on Plus and Pro alike, and its backend
// answers every request for it 400 "The 'gpt-5.5' model is not supported
// when using Codex with a ChatGPT account". The pick stayed, and agents
// (Cindy) were offered codex/gpt-5.5 first. A pick the account's list
// doesn't have is not offered; it comes back when the list has it again.
func TestCodexPickTheListDroppedNotOffered(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	newFetches.Lock()
	newFetches.m = map[string]time.Time{}
	newFetches.Unlock()

	var dropped atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/codex/models" {
			w.WriteHeader(404)
			return
		}
		if dropped.Load() {
			// the Pro account's list on 11-01
			w.Write(codexListOf("gpt-6.1-sol", "gpt-6-astra", "gpt-6-sol", "gpt-5.6-luna"))
			return
		}
		w.Write(codexListOf("gpt-5.5", "gpt-6-astra", "gpt-6-sol", "gpt-5.6-luna"))
	}))
	defer srv.Close()
	codexListedAt(t, srv.URL)
	codexSignedIn(t, home)

	offered := func() []string {
		t.Helper()
		p, err := Find("codex")
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, m := range p.ExposedOf(p.Available()) {
			out = append(out, m.ID)
		}
		return out
	}
	refetch := func() {
		t.Helper()
		p, err := Find("codex")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.fetch(t.Context()); err != nil {
			t.Fatal(err)
		}
		forgetAccountCaches()
	}

	refetch()
	if err := SetModels("codex", []string{"gpt-5.5", "gpt-6-astra", "gpt-6-sol"}); err != nil {
		t.Fatal(err)
	}
	if got := offered(); !slices.Equal(got, []string{"gpt-5.5", "gpt-6-astra", "gpt-6-sol"}) {
		t.Fatalf("picks while listed: %v", got)
	}

	dropped.Store(true)
	refetch()
	if got := offered(); slices.Contains(got, "gpt-5.5") || !slices.Equal(got, []string{"gpt-6-astra", "gpt-6-sol"}) {
		t.Fatalf("after ChatGPT stopped listing gpt-5.5, offered %v; want gpt-6-astra and gpt-6-sol", got)
	}
	// the user's pick is kept, only not offered
	if p, _ := Find("codex"); !slices.Contains(p.Models, "gpt-5.5") {
		t.Fatalf("the pick itself was dropped: %v", p.Models)
	}

	dropped.Store(false)
	refetch()
	if got := offered(); !slices.Contains(got, "gpt-5.5") {
		t.Fatalf("listed again, gpt-5.5 not offered: %v", got)
	}
}
