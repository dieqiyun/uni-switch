package catalog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// TestSyncHonorsContextLockWait verifies that when Sync is already in progress,
// a second caller waiting for the lock respects its context cancellation.
func TestSyncHonorsContextLockWait(t *testing.T) {
	// Acquire lock
	syncCh <- struct{}{}
	defer func() { <-syncCh }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Sync(ctx)
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context.DeadlineExceeded, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Sync blocked past its context deadline; lock wait does not honour context")
	}
}

// TestSyncIfMissingSkipsFetchWhenCacheAppearsWhileLocked verifies that if a
// cache file appears while SyncIfMissing is waiting for the sync lock, it
// returns nil immediately upon acquiring the lock without making a network request.
func TestSyncIfMissingSkipsFetchWhenCacheAppearsWhileLocked(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Cleanup(Reset)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"openai":{"id":"openai","models":{}}}`))
	}))
	defer srv.Close()

	was := modelsDevURL
	modelsDevURL = srv.URL
	t.Cleanup(func() { modelsDevURL = was })

	// Acquire lock first
	syncCh <- struct{}{}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- SyncIfMissing(ctx)
	}()

	// While SyncIfMissing is blocked waiting on syncCh, write CachePath()
	p := CachePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"openai":{"id":"openai","models":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Release lock so SyncIfMissing can proceed
	<-syncCh

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("SyncIfMissing failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("SyncIfMissing timed out")
	}

	if h := hits.Load(); h != 0 {
		t.Fatalf("SyncIfMissing made %d network requests; want 0 when cache appeared while waiting for lock", h)
	}
}
