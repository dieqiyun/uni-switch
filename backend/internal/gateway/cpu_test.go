package gateway

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// The CPU profile goes to the same callers as the heap profile and no
// others: a foreign page, Origin null, a page on localhost, a listed page
// with its key, a rebound Host, another machine with or without a key and
// a proxy's client are refused with lanGuard and without it, before a
// second is recorded.
func TestCPUProfileOnlyToThisComputer(t *testing.T) {
	fresh(t)
	t.Setenv("UNI_SWITCH_ADDR", "")
	t.Setenv("UNI_SWITCH_PUBLIC_URL", "")
	_, secrets := newCaller(t, "Page")
	s := settings.Load()
	s.CORSOrigins = []string{"https://listed.example"}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	key := secrets[0]
	old := cpuSecond
	cpuSecond = time.Millisecond
	t.Cleanup(func() { cpuSecond = old })
	srv := New()
	call := func(guarded bool, remote, host string, hdr ...string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", CPUPath+"?seconds=5", nil)
		r.RemoteAddr = remote
		r.Host = host
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		w := httptest.NewRecorder()
		h := srv.Handler()
		if guarded {
			h = localGuard(h)
		}
		h.ServeHTTP(w, r)
		return w
	}
	refused := profileCallersRefused(key)
	for _, c := range refused {
		for _, guarded := range []bool{true, false} {
			w := call(guarded, c.remote, c.host, c.hdr...)
			// lanGuard asks another machine without a key for one (401)
			if w.Code != http.StatusForbidden && w.Code != http.StatusUnauthorized || w.Header().Get(CPUStatsHeader) != "" || bytes.HasPrefix(w.Body.Bytes(), []byte{0x1f, 0x8b}) {
				t.Errorf("%s (lanGuard %v): %d %q", c.name, guarded, w.Code, w.Body.String())
			}
		}
	}

	for _, host := range []string{"127.0.0.1:3425", "localhost:3425", "[::1]:3425"} {
		w := call(true, "127.0.0.1:50123", host, "User-Agent", "Go-http-client/1.1")
		if w.Code != http.StatusOK {
			t.Fatalf("uni-switch cpu at %s: %d %q", host, w.Code, w.Body.String())
		}
		zr, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes()))
		if err != nil {
			t.Fatalf("at %s: not gzip: %v", host, err)
		}
		raw, err := io.ReadAll(zr)
		if err != nil || len(raw) == 0 {
			t.Fatalf("at %s: %v, %d bytes", host, err, len(raw))
		}
		// a pprof CPU profile's string table names its sample types
		for _, want := range []string{"samples", "cpu", "nanoseconds"} {
			if !bytes.Contains(raw, []byte(want)) {
				t.Errorf("at %s: the profile names no %s", host, want)
			}
		}
		var st CPUStats
		if err := json.Unmarshal([]byte(w.Header().Get(CPUStatsHeader)), &st); err != nil {
			t.Fatalf("at %s: stats %q: %v", host, w.Header().Get(CPUStatsHeader), err)
		}
		if st.PID != os.Getpid() || st.Seconds <= 0 || st.Goroutines < 1 || st.NumCPU < 1 || st.GOMAXPROCS < 1 || !st.CPUKnown || st.CPUTotal <= 0 || st.CPU < 0 || st.CPUTotal < st.CPU {
			t.Errorf("at %s: stats %+v", host, st)
		}
		if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
			t.Errorf("at %s: cacheable: %v", host, w.Header())
		}
	}
}

// seconds out of 5..120, or not a number, is refused before anything is
// recorded.
func TestCPUProfileSecondsBounded(t *testing.T) {
	srv := New()
	for _, v := range []string{"4", "121", "0", "-5", "x", "30s", "1e2"} {
		r := httptest.NewRequest("GET", CPUPath+"?seconds="+v, nil)
		r.RemoteAddr = "127.0.0.1:50123"
		r.Host = "127.0.0.1:3425"
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest || w.Header().Get(CPUStatsHeader) != "" {
			t.Errorf("seconds=%s: %d %q", v, w.Code, w.Body.String())
		}
	}
}

// While one CPU profile records, the gateway goes on answering other
// calls, and a second profile is told one is recording (409), not left to
// runtime/pprof, which keeps one at a time. A caller that hangs up ends
// its recording, and the next profile can start.
func TestCPUProfileOneAtATime(t *testing.T) {
	fresh(t)
	t.Setenv("UNI_SWITCH_ADDR", "")
	srv := New()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan error, 1)
	go func() {
		// 120 real seconds: it ends when the test hangs up
		req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+CPUPath+"?seconds=120", nil)
		res, err := http.DefaultClient.Do(req)
		if err == nil {
			res.Body.Close()
		}
		first <- err
	}()
	// wait for the first to hold the recording, not for a duration
	deadline := time.Now().Add(30 * time.Second)
	for !cpuRecording.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the first profile never started")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// the gateway answers meanwhile
	// a minute: far less than the 120 seconds recorded, and room for a busy
	// machine
	c := &http.Client{Timeout: time.Minute}
	res, err := c.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("the gateway didn't answer while a profile recorded: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET / while recording: %s", res.Status)
	}

	// a second profile is refused, at once
	res, err = c.Get(ts.URL + CPUPath + "?seconds=5")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusConflict || !strings.Contains(string(body), "recording a CPU profile already") || res.Header.Get(CPUStatsHeader) != "" {
		t.Fatalf("second profile: %s %q", res.Status, body)
	}

	// hanging up ends the first, and frees the recording
	cancel()
	select {
	case <-first:
	case <-time.After(30 * time.Second):
		t.Fatal("the first call didn't end")
	}
	deadline = time.Now().Add(30 * time.Second)
	for cpuRecording.Load() {
		if time.Now().After(deadline) {
			t.Fatal("a hung-up profile kept recording")
		}
		time.Sleep(5 * time.Millisecond)
	}
	old := cpuSecond
	cpuSecond = time.Millisecond
	t.Cleanup(func() { cpuSecond = old })
	res, err = c.Get(ts.URL + CPUPath + "?seconds=5")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the next profile: %s", res.Status)
	}
}
