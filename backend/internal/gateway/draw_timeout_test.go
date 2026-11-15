package gateway

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// slowDrawer is a vendor whose images API answers after wait, as one does
// only once it has drawn, with Settings' Image generation timeout set to
// minutes (0 for the default).
func slowDrawer(t *testing.T, minutes int, wait time.Duration) *Server {
	t.Helper()
	fresh(t)
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy"} {
		t.Setenv(k, "")
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(wait):
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"created":1,"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(pngBytes) + `"}]}`))
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "slow", Name: "Slow", Chat: up.URL + "/v1", Key: "key", Models: []string{"gpt-image-1"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("slow", up.URL+"/v1", []catalog.Model{{ID: "gpt-image-1"}}); err != nil {
		t.Fatal(err)
	}
	if minutes > 0 {
		s := settings.Load()
		s.ImageTimeout = minutes
		if err := settings.Save(s); err != nil {
			t.Fatal(err)
		}
	}
	return New()
}

// deadlines records the deadline each request to a vendor goes out with,
// and whether it went as a drawing, on its way to the real transport.
type deadlines struct {
	next http.RoundTripper
	mu   sync.Mutex
	got  []time.Duration
	draw []bool
}

func (d *deadlines) RoundTrip(r *http.Request) (*http.Response, error) {
	dl, _ := r.Context().Deadline()
	_, drawn := drawWaitOf(r.Context())
	d.mu.Lock()
	d.got, d.draw = append(d.got, time.Until(dl)), append(d.draw, drawn)
	d.mu.Unlock()
	return d.next.RoundTrip(r)
}

// neighbads on X: a slow image endpoint needed longer than the 5 minutes
// drawing was given. Settings' Image generation timeout of 30 minutes
// is what a drawing's request goes to the vendor with, through the
// gateway's own transport; with none set it is 5 minutes, as before.
func TestImageTimeoutSettingReachesTheVendor(t *testing.T) {
	for _, c := range []struct {
		minutes int
		want    time.Duration
	}{{30, 30 * time.Minute}, {0, 5 * time.Minute}, {60, time.Hour}} {
		s := slowDrawer(t, c.minutes, 0)
		rec := &deadlines{next: s.client.Transport}
		s.client = &http.Client{Transport: rec}
		code, _, raw := postImages(t, s, "/v1/images/generations", "application/json", `{"model":"slow/gpt-image-1","prompt":"a uni-switch"}`)
		if code != 200 {
			t.Fatalf("%d minutes: %d %s", c.minutes, code, raw)
		}
		if len(rec.got) != 1 || !rec.draw[0] || rec.got[0] > c.want || rec.got[0] < c.want-time.Minute {
			t.Fatalf("%d minutes: the vendor was asked with %v left (as a drawing: %v), want %v", c.minutes, rec.got, rec.draw, c.want)
		}
	}
}

// The same, with the minutes made short: a vendor that takes past the
// default 5 to begin answering, and past the wait any other request is
// given to begin (upstreamHeaderWait), draws when Settings gives it 30;
// with the default it is said not to have answered in 5.
func TestLongImageTimeoutLetsASlowVendorFinish(t *testing.T) {
	wasUnit, wasHead := drawUnit, upstreamHeaderWait
	drawUnit, upstreamHeaderWait = 20*time.Millisecond, 100*time.Millisecond // 5 minutes 100ms, 30 600ms, 10 minutes 100ms
	t.Cleanup(func() { drawUnit, upstreamHeaderWait = wasUnit, wasHead })
	const slow = 300 * time.Millisecond

	s := slowDrawer(t, 30, slow)
	code, a, raw := postImages(t, s, "/v1/images/generations", "application/json", `{"model":"slow/gpt-image-1","prompt":"a uni-switch"}`)
	if code != 200 || len(a.Data) != 1 {
		t.Fatalf("given 30: %d %s", code, raw)
	}
	if b, _ := base64.StdEncoding.DecodeString(a.Data[0].B64); string(b) != string(pngBytes) {
		t.Fatalf("image %q", b)
	}

	s = slowDrawer(t, 0, slow)
	code, _, raw = postImages(t, s, "/v1/images/generations", "application/json", `{"model":"slow/gpt-image-1","prompt":"a uni-switch"}`)
	if code != 504 || !strings.Contains(raw, "Slow didn't answer in 100ms") {
		t.Fatalf("given the default: %d %s", code, raw)
	}
}

// An image a vendor answers with as a link is downloaded within the same
// minutes: a long setting isn't cut by the 5-minute download cap.
func TestImageDownloadGetsTheSetMinutes(t *testing.T) {
	slowDrawer(t, 0, 0)
	if got := imageLongest(); got != ImageLongest {
		t.Fatalf("unset: %v", got)
	}
	slowDrawer(t, 45, 0)
	if got := imageLongest(); got != 45*time.Minute {
		t.Fatalf("given 45: %v", got)
	}
}

// A request that isn't a drawing still has upstreamHeaderWait to begin
// answering: only drawings wait longer.
func TestOnlyDrawingsWaitPastTheHeaderWait(t *testing.T) {
	was := upstreamHeaderWait
	upstreamHeaderWait = 100 * time.Millisecond
	t.Cleanup(func() { upstreamHeaderWait = was })
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(400 * time.Millisecond):
		}
	}))
	t.Cleanup(up.Close)
	c := &http.Client{Transport: upstreamTransport()}
	req, _ := http.NewRequest(http.MethodGet, up.URL, nil)
	if res, err := c.Do(req); err == nil || !strings.Contains(err.Error(), "timeout awaiting response headers") {
		if res != nil {
			res.Body.Close()
		}
		t.Fatalf("a chat's request waited past the header wait: %v", err)
	}
	ctx, cancel := drawWithin(t.Context(), 5*time.Second)
	defer cancel()
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, up.URL, nil)
	res, err := c.Do(req)
	if err != nil {
		t.Fatalf("a drawing's request was cut: %v", err)
	}
	res.Body.Close()
}
