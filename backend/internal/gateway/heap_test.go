package gateway

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// The heap profile is a program's on this computer alone: whatever a
// normal route refuses is refused (a foreign page, Origin null, a rebound
// Host, another machine), and so is what a normal route lets through but
// a profile has no use for — a page on localhost, a listed page with its
// gateway key, another machine with one, a proxy's client. An agent-like
// call to 127.0.0.1 or localhost gets a gzipped pprof heap profile, and
// the runtime's numbers beside it.
func TestHeapProfileOnlyToThisComputer(t *testing.T) {
	fresh(t)
	t.Setenv("UNI_SWITCH_ADDR", "")
	t.Setenv("UNI_SWITCH_PUBLIC_URL", "")
	_, secrets := newCaller(t, "Page") // the gateway shared, with a key
	s := settings.Load()
	s.CORSOrigins = []string{"https://listed.example"}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	key := secrets[0]
	srv := New()
	call := func(guarded bool, remote, host string, hdr ...string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", HeapPath, nil)
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
			if w.Code != http.StatusForbidden && w.Code != http.StatusUnauthorized || w.Header().Get(HeapStatsHeader) != "" || bytes.HasPrefix(w.Body.Bytes(), []byte{0x1f, 0x8b}) {
				t.Errorf("%s (lanGuard %v): %d %q", c.name, guarded, w.Code, w.Body.String())
			}
		}
	}

	for _, host := range []string{"127.0.0.1:3425", "localhost:3425", "[::1]:3425"} {
		w := call(true, "127.0.0.1:50123", host, "User-Agent", "Go-http-client/1.1")
		if w.Code != http.StatusOK {
			t.Fatalf("uni-switch heap at %s: %d %q", host, w.Code, w.Body.String())
		}
		zr, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes()))
		if err != nil {
			t.Fatalf("at %s: not gzip: %v", host, err)
		}
		raw, err := io.ReadAll(zr)
		if err != nil || len(raw) == 0 {
			t.Fatalf("at %s: %v, %d bytes", host, err, len(raw))
		}
		// a pprof heap profile's string table names its sample types
		for _, want := range []string{"alloc_space", "inuse_space", "bytes"} {
			if !bytes.Contains(raw, []byte(want)) {
				t.Errorf("at %s: the profile names no %s", host, want)
			}
		}
		var st HeapStats
		if err := json.Unmarshal([]byte(w.Header().Get(HeapStatsHeader)), &st); err != nil {
			t.Fatalf("at %s: stats %q: %v", host, w.Header().Get(HeapStatsHeader), err)
		}
		if st.PID != os.Getpid() || st.HeapSys == 0 || st.Sys < st.HeapSys || st.HeapInuse == 0 || st.LiveHeap == 0 || st.NumGC == 0 || st.Goroutines < 1 {
			t.Errorf("at %s: stats %+v", host, st)
		}
		if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
			t.Errorf("at %s: cacheable: %v", host, w.Header())
		}
	}
}

// profileRefusal is a caller a profile of this process is refused to.
type profileRefusal struct {
	name         string
	remote, host string
	hdr          []string
}

// profileCallersRefused are the callers every profile of this process
// (heap, CPU, goroutines) is refused to, key a gateway key they may carry.
func profileCallersRefused(key string) []profileRefusal {
	chrome := []string{"User-Agent", "Mozilla/5.0 (Macintosh) AppleWebKit/605.1.15 Version/27.2 Safari/605.1.15", "Sec-Fetch-Site", "cross-site", "Sec-Fetch-Mode", "cors"}
	return []profileRefusal{
		{"a foreign page", "127.0.0.1:50123", "127.0.0.1:3425", append([]string{"Origin", "https://evil.example"}, chrome...)},
		{"Origin null", "127.0.0.1:50123", "127.0.0.1:3425", append([]string{"Origin", "null"}, chrome...)},
		{"Origin null with a key", "127.0.0.1:50123", "127.0.0.1:3425", append([]string{"Origin", "null", "Authorization", "Bearer " + key}, chrome...)},
		{"a page on localhost", "127.0.0.1:50123", "127.0.0.1:3425", append([]string{"Origin", "http://localhost:5173"}, chrome...)},
		{"a localhost subdomain", "127.0.0.1:50123", "127.0.0.1:3425", append([]string{"Origin", "http://evil.localhost"}, chrome...)},
		{"a listed page with its key", "127.0.0.1:50123", "127.0.0.1:3425", append([]string{"Origin", "https://listed.example", "Authorization", "Bearer " + key}, chrome...)},
		{"a rebound page's GET", "127.0.0.1:50123", "rebind.attacker.example:3425", chrome},
		{"a rebound page with a key", "127.0.0.1:50123", "rebind.attacker.example:3425", append([]string{"Authorization", "Bearer " + key}, chrome...)},
		{"a call to another name", "127.0.0.1:50123", "rebind.attacker.example:3425", []string{"User-Agent", "curl/8"}},
		{"a localhost subdomain as Host", "127.0.0.1:50123", "wails.localhost:3425", []string{"User-Agent", "curl/8"}},
		{"another machine", "192.168.1.20:50123", "192.168.1.10:3425", nil},
		{"another machine with a key", "192.168.1.20:50123", "192.168.1.10:3425", []string{"Authorization", "Bearer " + key}},
		{"a proxy's client", "127.0.0.1:50123", "127.0.0.1:3425", []string{"X-Forwarded-For", "203.0.113.9"}},
		{"a proxy's client with a key", "127.0.0.1:50123", "127.0.0.1:3425", []string{"Cf-Connecting-IP", "203.0.113.9", "Authorization", "Bearer " + key}},
	}
}
