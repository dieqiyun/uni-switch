package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// parkedForTheDump waits on ch, as a goroutine stuck in uni-switch would: the
// dump must name it and what it waits on.
func parkedForTheDump(ch <-chan struct{}, parked chan<- struct{}) {
	close(parked)
	<-ch
}

// The goroutine dump goes to the same callers as the heap profile (a
// program on this computer over loopback, nothing a browser sends, no
// other Host) and is refused to the rest. What it answers is every
// goroutine's stack, a goroutine parked in uni-switch's code among them,
// named with what it waits on, and which process it is of (#3362: an app
// stuck for days could only be read by killing it).
func TestGoroutineDumpOnlyToThisComputer(t *testing.T) {
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
	srv := New()
	call := func(guarded bool, remote, host string, hdr ...string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", GoroutinesPath, nil)
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
	for _, c := range profileCallersRefused(key) {
		for _, guarded := range []bool{true, false} {
			w := call(guarded, c.remote, c.host, c.hdr...)
			// lanGuard asks another machine without a key for one (401)
			if w.Code != http.StatusForbidden && w.Code != http.StatusUnauthorized || w.Header().Get(GoroutineStatsHeader) != "" || bytes.HasPrefix(w.Body.Bytes(), []byte("goroutine ")) {
				t.Errorf("%s (lanGuard %v): %d %q", c.name, guarded, w.Code, w.Body.String())
			}
		}
	}

	release, parked := make(chan struct{}), make(chan struct{})
	defer close(release)
	go parkedForTheDump(release, parked)
	<-parked
	for _, host := range []string{"127.0.0.1:3425", "localhost:3425", "[::1]:3425"} {
		w := call(true, "127.0.0.1:50123", host, "User-Agent", "Go-http-client/1.1")
		if w.Code != http.StatusOK {
			t.Fatalf("uni-switch goroutines at %s: %d %q", host, w.Code, w.Body.String())
		}
		body := w.Body.String()
		if !strings.HasPrefix(body, "goroutine ") {
			t.Fatalf("at %s: not a goroutine dump: %.200q", host, body)
		}
		// the parked one, with its state and its frame in this file
		i := strings.Index(body, "gateway.parkedForTheDump(")
		if i < 0 {
			t.Fatalf("at %s: the parked goroutine isn't in the dump", host)
		}
		head := body[strings.LastIndex(body[:i], "goroutine "):i]
		if !strings.Contains(head, "[chan receive") {
			t.Errorf("at %s: the parked goroutine's state isn't its wait: %q", host, head)
		}
		if !strings.Contains(body[i:], "goroutines_test.go:") {
			t.Errorf("at %s: no source line for the parked goroutine", host)
		}
		var st GoroutineStats
		if err := json.Unmarshal([]byte(w.Header().Get(GoroutineStatsHeader)), &st); err != nil {
			t.Fatalf("at %s: stats %q: %v", host, w.Header().Get(GoroutineStatsHeader), err)
		}
		if st.PID != os.Getpid() || st.Goroutines < 2 || st.Uptime == "" {
			t.Errorf("at %s: stats %+v", host, st)
		}
		if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
			t.Errorf("at %s: headers %v", host, w.Header())
		}
	}
}

// stuckSave holds settings.json's lock as a save that never finishes
// would (a write held by another program, a disk that doesn't answer),
// until release is closed; held is closed once it holds it.
func stuckSave(t *testing.T, held, release chan struct{}) {
	err := settings.EditUsageAliases(func(m map[string]string) (map[string]string, error) {
		close(held)
		<-release
		return nil, errors.New("the test's save: nothing is written")
	})
	if err == nil {
		t.Error("the stuck save wrote settings.json")
	}
}

// The dump is asked for while uni-switch hangs, so it answers ahead of what
// the gateway's requests pass through first (dumpFirst): here a save
// holds settings.json's lock, which lanGuard's allow list reads under, so
// /v1/models on the gateway's own listener (Front) waits on it, and the
// dump still answers, naming the save that holds the lock and the request
// waiting on it. Routed behind lanGuard, as every other route is, the
// dump waits too and this is red.
func TestGoroutineDumpAnswersWhileRequestsWait(t *testing.T) {
	fresh(t)
	t.Setenv("UNI_SWITCH_ADDR", "")
	t.Setenv("UNI_SWITCH_PUBLIC_URL", "")
	ts := httptest.NewServer(New().Front())
	defer ts.Close()
	// room for a loaded machine; a dump stuck behind the lock never ends
	c := &http.Client{Timeout: 20 * time.Second}
	dump := func() string {
		t.Helper()
		res, err := c.Get(ts.URL + GoroutinesPath)
		if err != nil {
			t.Fatalf("the dump didn't answer while settings.json's lock was held: %v", err)
		}
		defer res.Body.Close()
		b, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != http.StatusOK {
			t.Fatalf("the dump: %d %v %.200q", res.StatusCode, err, b)
		}
		return string(b)
	}

	held, release := make(chan struct{}), make(chan struct{})
	saved := make(chan struct{})
	go func() {
		defer close(saved)
		stuckSave(t, held, release)
	}()
	released := false
	unstick := func() {
		if !released {
			released = true
			close(release)
			<-saved
		}
	}
	defer unstick()
	<-held

	models := make(chan error, 1)
	go func() {
		res, err := http.Get(ts.URL + "/v1/models")
		if err == nil {
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
		}
		models <- err
	}()

	// the dump until it shows /v1/models waiting at the allow list: each
	// one answered while the lock was held
	var body string
	for deadline := time.Now().Add(30 * time.Second); ; {
		body = dump()
		if strings.Contains(body, "gateway.allowList(") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("/v1/models never waited at the allow list; the last dump:\n%s", body)
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case err := <-models:
		t.Fatalf("/v1/models answered while settings.json's lock was held (%v): it isn't the lock it waits on", err)
	default:
	}
	if !strings.Contains(body, "gateway.stuckSave(") || !strings.Contains(body, "settings.EditUsageAliases(") {
		t.Errorf("the dump doesn't name the save holding the lock:\n%s", body)
	}

	unstick()
	select {
	case err := <-models:
		if err != nil {
			t.Errorf("/v1/models once the lock was let go: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("/v1/models never answered once the lock was let go")
	}
}
