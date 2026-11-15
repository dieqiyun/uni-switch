package catalog

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// A model models.dev has just listed gets its price within a day, and
// sooner when a call's model has none (#224: gpt-6.1-sol, unpriced on a
// week-old cache).
func TestKeepFresh(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Cleanup(Reset)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(`{"openai":{"id":"openai","models":{"gpt-6.1-sol":{"id":"gpt-6.1-sol","cost":{"input":2,"output":10,"cache_read":0.1}}}}}`))
	}))
	defer srv.Close()
	was := modelsDevURL
	modelsDevURL = srv.URL
	t.Cleanup(func() { modelsDevURL = was; keeping.Store(false); lastFetch.Store(0) })

	p := CachePath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(`{"openai":{"id":"openai","models":{}}}`), 0o644)
	Reset()
	if Stale() {
		t.Fatal("a cache just written is stale")
	}
	old := time.Now().Add(-25 * time.Hour)
	os.Chtimes(p, old, old)
	if !Stale() {
		t.Fatal("a cache a day old isn't stale")
	}

	// not a process that keeps it fresh (a CLI command): nothing fetched
	Missing()
	time.Sleep(100 * time.Millisecond)
	if hits.Load() != 0 {
		t.Fatal("Missing fetched outside KeepFresh")
	}

	keeping.Store(true)
	Missing()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if pr, ok := PriceOf("openai", "gpt-6.1-sol"); ok && pr.Output == 10 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("gpt-6.1-sol not priced after Missing (%d fetches)", hits.Load())
		}
		time.Sleep(20 * time.Millisecond)
	}

	// fetched just now: another unpriced model asks nothing more
	Missing()
	Missing()
	time.Sleep(100 * time.Millisecond)
	if n := hits.Load(); n != 1 {
		t.Fatalf("%d fetches, want 1", n)
	}
}
