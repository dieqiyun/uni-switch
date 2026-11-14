package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// archivedOf reads every object in b back, by the call's provider.
func archivedOf(t *testing.T, b *memBucket) map[string][]Archived {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	out := map[string][]Archived{}
	for name, data := range b.objs {
		var a Archived
		if err := json.Unmarshal(data, &a); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Contains(string(data), archiveKey) || strings.Contains(string(data), "uni-switch-gateway-key") || strings.Contains(string(data), "Bearer k") {
			t.Errorf("a secret in %s:\n%s", name, data)
		}
		out[a.Call.Provider] = append(out[a.Call.Provider], a)
	}
	return out
}

// With the request archive on, a System One request an app sends uni-switch
// (/v1/systemone) is kept as a chat request is, and its route, its Recent
// call and its usage row name where (wangb on Discord: 请求存档开启, 但是
// systemone 的请求没有记录下来). Off, nothing is kept.
func TestArchiveKeepsSystemOne(t *testing.T) {
	fresh(t)
	jev := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"jev-latest","answers":{"intent":{"type":"choice","choice":"bug","confidence":0.9}},"usage":{"input_tokens":10,"output_tokens":2}}`)
	}))
	defer jev.Close()
	if err := provider.Save(provider.Provider{ID: "load-a", Name: "A", Key: "ka-secret", Decide: jev.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	b := &memBucket{objs: map[string][]byte{}}
	archiveTo(t, b)
	s := New()
	post := func() {
		t.Helper()
		body := `{"model":"load-a/jev-latest","questions":{"intent":{"type":"choice","criteria":{"bug":"a bug report"}}},"state":{"message":"it crashes, token ` + archiveKey + `"}}`
		req := httptest.NewRequest("POST", "/v1/systemone", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer uni-switch-gateway-key")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"bug"`) {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		archivePending.Wait()
	}

	post()
	if len(b.objs) != 0 || s.Recent()[0].Archive != "" {
		t.Fatalf("archived while off: %v", keys(b.objs))
	}

	settings.Save(settings.Settings{RequestArchive: true})
	post()
	got := archivedOf(t, b)["load-a"]
	if len(got) != 1 || len(b.objs) != 1 {
		t.Fatalf("uploaded %v", keys(b.objs))
	}
	a := got[0]
	c := s.Recent()[0] // the newest first
	r := s.trace.routes[len(s.trace.routes)-1]
	if c.Archive == "" || c.Archive != a.ID || r.Archive != a.ID || c.Route != r.ID {
		t.Fatalf("call %q, route %q (%d/%d), archived %q", c.Archive, r.Archive, c.Route, r.ID, a.ID)
	}
	if a.Request.Path != "/v1/systemone" || !strings.Contains(a.Request.Body, "it crashes") || a.Request.Headers["Authorization"][0] != "[REDACTED]" ||
		a.Response.Status != 200 || !strings.Contains(a.Response.Body, `"bug"`) || a.Call.Model != "load-a/jev-latest" || a.Call.Status != 200 {
		t.Errorf("archived %+v", a)
	}
	n := 0
	for _, row := range usage.Load(time.Time{}) {
		if row.Provider == "load-a" && row.Archive != "" {
			if row.Archive != a.ID {
				t.Errorf("usage row names %q, archived %q", row.Archive, a.ID)
			}
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d usage rows name the archive", n)
	}
}
