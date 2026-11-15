package claudecode

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// keepRelease serves 2.1.287 as downloads.claude.ai does and counts what is
// asked: the latest version, and the build's download.
func keepRelease(t *testing.T, latestV string) (asked, downloads *atomic.Int32) {
	t.Helper()
	const body = "#!/bin/sh\necho 2.1.287 '(Claude Code)'\n"
	platform := thisPlatform(t)
	asked, downloads = new(atomic.Int32), new(atomic.Int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			asked.Add(1)
			fmt.Fprint(w, latestV)
		case "/2.1.287/manifest.json":
			fmt.Fprint(w, manifest(platform, body))
		case "/2.1.287/" + platform + "/" + exeName(runtime.GOOS):
			downloads.Add(1)
			fmt.Fprint(w, body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	oldBase, oldKeep := Base, keep
	Base = srv.URL
	keep.own = func() string { return "" }
	keep.latest = func(ctx context.Context) (Release, error) {
		return latest(ctx, runtime.GOOS, runtime.GOARCH, false)
	}
	t.Cleanup(func() { Base, keep = oldBase, oldKeep })
	t.Cleanup(func() { _ = Remove() })
	t.Setenv("DISABLE_AUTOUPDATER", "")
	return asked, downloads
}

// downloaded puts a Claude Code of version v where uni-switch keeps its own.
func downloaded(t *testing.T, v string) string {
	t.Helper()
	dir := filepath.Join(Root(), v)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, exeName(runtime.GOOS))
	if err := os.WriteFile(p, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestKeepCurrentUpdatesUniSwitchsDownload(t *testing.T) {
	asked, downloads := keepRelease(t, "2.1.287\n")
	old := downloaded(t, "2.1.200")

	said, err := keepOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(said, "2.1.200 to 2.1.287") {
		t.Errorf("said %q", said)
	}
	if p, v := Downloaded(); v != "2.1.287" || p == old {
		t.Fatalf("Downloaded() = %q, %q; want 2.1.287", p, v)
	}
	// a run started a moment ago may hold the old path: it stays for now
	if _, err := os.Stat(old); err != nil {
		t.Errorf("the replaced version went at once: %v", err)
	}

	// the next look removes it, and downloads nothing again
	if said, err := keepOnce(context.Background()); err != nil || said != "" {
		t.Fatalf("second look: %q, %v", said, err)
	}
	if _, err := os.Stat(filepath.Dir(old)); !os.IsNotExist(err) {
		t.Errorf("the replaced version is still there on the next look: %v", err)
	}
	if asked.Load() != 2 || downloads.Load() != 1 {
		t.Errorf("asked %d times, downloaded %d; want 2 and 1", asked.Load(), downloads.Load())
	}
}

func TestKeepCurrentLeavesWhatIsntUniSwitchsToUpdate(t *testing.T) {
	for name, set := range map[string]func(t *testing.T){
		"nothing downloaded": func(t *testing.T) {},
		"the machine's own": func(t *testing.T) {
			downloaded(t, "2.1.200")
			keep.own = func() string { return "/usr/local/bin/claude" }
		},
		"held by the user":      func(t *testing.T) { downloaded(t, "2.1.200"); t.Setenv("DISABLE_AUTOUPDATER", "1") },
		"already the newest":    func(t *testing.T) { downloaded(t, "2.1.287") },
		"newer than the latest": func(t *testing.T) { downloaded(t, "2.1.300") },
	} {
		t.Run(name, func(t *testing.T) {
			_, downloads := keepRelease(t, "2.1.287")
			set(t)
			_, before := Downloaded()
			if said, err := keepOnce(context.Background()); err != nil || said != "" {
				t.Fatalf("keepOnce = %q, %v", said, err)
			}
			if downloads.Load() != 0 {
				t.Errorf("downloaded %d times", downloads.Load())
			}
			if _, v := Downloaded(); v != before {
				t.Errorf("Downloaded() is %q, was %q", v, before)
			}
		})
	}
}

func TestKeepCurrentKeepsTheDownloadWhenTheReleaseCantBeRead(t *testing.T) {
	keepRelease(t, "<!doctype html><title>Unavailable</title>")
	old := downloaded(t, "2.1.200")
	if _, err := keepOnce(context.Background()); err == nil {
		t.Fatal("no error for a release that couldn't be read")
	}
	if p, v := Downloaded(); p != old || v != "2.1.200" {
		t.Errorf("Downloaded() = %q, %q; want the old one kept", p, v)
	}

	keep.latest = func(context.Context) (Release, error) { return Release{}, errors.New("offline") }
	if _, err := keepOnce(context.Background()); err == nil {
		t.Fatal("no error when offline")
	}
	if p, _ := Downloaded(); p != old {
		t.Errorf("Downloaded() = %q; want the old one kept", p)
	}
}

func TestKeepCurrentLooksWhileServingAndEndsWithIt(t *testing.T) {
	keepRelease(t, "2.1.287")
	downloaded(t, "2.1.200")
	oldFirst, oldEvery := keepFirst, keepEvery
	keepFirst, keepEvery = 0, time.Hour
	t.Cleanup(func() { keepFirst, keepEvery = oldFirst, oldEvery })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { KeepCurrent(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, v := Downloaded(); v == "2.1.287" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("KeepCurrent never updated the download")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("KeepCurrent didn't end with its context")
	}
}
