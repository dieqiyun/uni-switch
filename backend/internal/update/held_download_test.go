package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// releaseHost serves body as GitHub's asset host does, Range and 416
// included (http.ServeContent), and records each request's Range.
func releaseHost(t *testing.T, body []byte, first func(w http.ResponseWriter) bool) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var ranges []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		n := len(ranges)
		mu.Unlock()
		if n == 1 && first != nil && first(w) {
			return
		}
		http.ServeContent(w, r, "uni-switch.exe", time.Time{}, bytes.NewReader(body))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(ranges)
	}
}

func asset(url string, body []byte) Asset {
	sum := sha256.Sum256(body)
	return Asset{URL: url, Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}
}

// A try that got every byte and lost the connection after (the reply said
// it had more to come) leaves the whole file: the next try asks for
// nothing past its end, which no host can send (416, what mycf_711's
// every update got after the first), and the file is the release's.
func TestDownloadWholeFileAsksNoRangePastTheEnd(t *testing.T) {
	quickRetries(t)
	body := bytes.Repeat([]byte("uni-switch!"), 40<<10)
	srv, ranges := releaseHost(t, body, func(w http.ResponseWriter) bool {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)+100))
		w.Write(body)
		w.(http.Flusher).Flush()
		conn, _, _ := w.(http.Hijacker).Hijack()
		conn.Close() // after the last byte
		return true
	})
	var done, total int64
	ctx := WithProgress(context.Background(), func(d, t int64) { done, total = d, t })
	path := filepath.Join(t.TempDir(), "uni-switch.exe.new")
	if err := download(ctx, asset(srv.URL, body), path); err != nil {
		t.Fatalf("download: %v (asked %q)", err, ranges())
	}
	if got := ranges(); !slices.Equal(got, []string{""}) {
		t.Fatalf("ranges asked = %q, want the first request alone", got)
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, body) {
		t.Fatalf("file has %d bytes, not the release's", len(b))
	}
	if done != int64(len(body)) || total != int64(len(body)) {
		t.Fatalf("progress = %d/%d, want %d/%d", done, total, len(body), len(body))
	}
}

// A file as long as the release but with other bytes (what was kept isn't
// it) is fetched again from the start, not asked past its end.
func TestDownloadWholeFileOfOtherBytesStartsOver(t *testing.T) {
	quickRetries(t)
	body := bytes.Repeat([]byte("uni-switch!"), 40<<10)
	srv, ranges := releaseHost(t, body, func(w http.ResponseWriter) bool {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)+100))
		w.Write(bytes.Repeat([]byte("x"), len(body)))
		w.(http.Flusher).Flush()
		conn, _, _ := w.(http.Hijacker).Hijack()
		conn.Close()
		return true
	})
	path := filepath.Join(t.TempDir(), "uni-switch.exe.new")
	if err := download(context.Background(), asset(srv.URL, body), path); err != nil {
		t.Fatalf("download: %v (asked %q)", err, ranges())
	}
	if got := ranges(); !slices.Equal(got, []string{"", ""}) {
		t.Fatalf("ranges asked = %q, want two whole requests", got)
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, body) {
		t.Fatalf("file has %d bytes, not the release's", len(b))
	}
}

// A host that answers a resumed try 416 (what was kept isn't the start of
// the file it has now) has the file fetched once more from the start;
// one that answers 416 again is an error, not a loop.
func TestDownload416StartsOverOnce(t *testing.T) {
	quickRetries(t)
	body := bytes.Repeat([]byte("0123456789"), 30<<10)
	var mu sync.Mutex
	var ranges []string
	always := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		n := len(ranges)
		mu.Unlock()
		switch {
		case n == 1:
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.Write(body[:1000])
			w.(http.Flusher).Flush()
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
		case r.Header.Get("Range") != "" || always:
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		default:
			w.Write(body)
		}
	}))
	defer srv.Close()
	a := asset(srv.URL, body)
	a.Size = 0 // the feed doesn't always say
	path := filepath.Join(t.TempDir(), "uni-switch.exe.new")
	if err := download(context.Background(), a, path); err != nil {
		t.Fatalf("download: %v (asked %q)", err, ranges)
	}
	if want := []string{"", "bytes=1000-", ""}; !slices.Equal(ranges, want) {
		t.Fatalf("ranges asked = %q, want %q", ranges, want)
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, body) {
		t.Fatalf("file has %d bytes, not the release's", len(b))
	}

	ranges, always = nil, true
	err := download(context.Background(), a, filepath.Join(t.TempDir(), "uni-switch.exe.new"))
	if err == nil || !strings.Contains(err.Error(), "416") {
		t.Fatalf("err = %v, want the 416", err)
	}
	if len(ranges) != 3 {
		t.Fatalf("ranges asked = %q, want the first try, its resume and one start over", ranges)
	}
}

// holdRemove has removeFile fail for path as Windows does for a file
// another program has open, until the returned func lets go.
func holdRemove(t *testing.T, path string) func() {
	t.Helper()
	var mu sync.Mutex
	held := true
	was := removeFile
	removeFile = func(p string) error {
		mu.Lock()
		h := held
		mu.Unlock()
		if h && filepath.Clean(p) == filepath.Clean(path) {
			return &os.PathError{Op: "remove", Path: p, Err: errSharingViolation}
		}
		return was(p)
	}
	t.Cleanup(func() { removeFile = was })
	return func() { mu.Lock(); held = false; mu.Unlock() }
}

// What a download can't clear (held open) is said so before any request,
// never gone on from: the reporter's 416 came from asking for the rest of
// a file the removal had silently left.
func TestDownloadSaysAHeldLeftover(t *testing.T) {
	body := []byte("MZ the next version")
	srv, ranges := releaseHost(t, body, nil)
	path := filepath.Join(t.TempDir(), "uni-switch.exe.new")
	os.WriteFile(path, body[:5], 0o644)
	holdRemove(t, path)
	err := download(context.Background(), asset(srv.URL, body), path)
	if err == nil || !strings.Contains(err.Error(), "another program has it open") || strings.Contains(err.Error(), "416") {
		t.Fatalf("err = %v, want the leftover said held", err)
	}
	if got := ranges(); len(got) != 0 {
		t.Fatalf("asked %q for a file it couldn't clear", got)
	}
}

// An earlier download that can't be removed (a scanner holding it for
// minutes, mycf_711 on Discord) doesn't stop the next: it is downloaded
// under a name of its own beside it, which the next start finds to
// remove, as it does the one held.
func TestStageStepsRoundAHeldLeftover(t *testing.T) {
	quickRetries(t)
	f := newDesktopFixture(t)
	srv, ranges := releaseHost(t, f.body, nil)
	f.asset = asset(srv.URL, f.body)
	d := cacheStage(f.exe)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	left := filepath.Join(d, "uni-switch.exe.new")
	os.WriteFile(left, []byte("MZ an older"), 0o755) // a download of before
	letGo := holdRemove(t, left)

	staged, err := stageBinaryFor(context.Background(), f.asset, f.exe)
	if err != nil {
		t.Fatalf("stage: %v (asked %q)", err, ranges())
	}
	if staged == left || filepath.Dir(staged) != d || !strings.HasPrefix(filepath.Base(staged), "uni-switch.exe.new-") {
		t.Fatalf("staged at %s, want a name of its own beside %s", staged, left)
	}
	if got := ranges(); !slices.Equal(got, []string{""}) {
		t.Fatalf("ranges asked = %q, want one whole request", got)
	}
	if b, _ := os.ReadFile(staged); !bytes.Equal(b, f.body) {
		t.Fatalf("staged %q", b)
	}
	if got := stagedFiles(f.exe); !slices.Contains(got, staged) || !slices.Contains(got, left) {
		t.Fatalf("stagedFiles = %q, want both %s and %s", got, staged, left)
	}

	// once let go, the next start clears both: they hold this version
	// over again, nothing newer
	letGo()
	os.WriteFile(staged, []byte("MZ this version"), 0o755)
	os.WriteFile(left, []byte("MZ this version"), 0o755)
	RemoveStaleNew(f.exe)
	for _, p := range []string{staged, left} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s kept: %v", filepath.Base(p), err)
		}
	}
}
