package gateway

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// proxyCut is how long a proxy between Codex and uni-switch let a request go
// with nothing back before it hung up: about 10s (mrten_95615 on Discord,
// Windows, a Clash-style app's system proxy, which Codex's reqwest takes
// for 127.0.0.1 too). Every turn whose vendor took longer to its first
// event ended 499, as uni-switch held the stream's headers for 15s.
const proxyCut = 10 * time.Second

// A vendor slow to answer at all leaves its agent neither headers nor
// bytes for as long as a proxy in between waits: the 200 goes within a
// few seconds, then a comment every few more, at the defaults, and the
// reply follows in the same stream.
func TestHeldStreamHeardBeforeAProxyCuts(t *testing.T) {
	fresh(t)
	release := make(chan struct{})
	waited := make(chan time.Duration, 1)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		began := time.Now()
		select {
		case <-release:
		case <-time.After(2 * proxyCut):
		case <-r.Context().Done():
			return
		}
		waited <- time.Since(began)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, quietCreated+quietDelta("pong")+quietDone)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Models: []string{"gpt-test"}, Responses: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(New().Handler())
	t.Cleanup(gw.Close)
	began := time.Now()
	resp, err := http.Post(gw.URL+"/v1/responses", "application/json", strings.NewReader(`{"model":"relay/gpt-test","stream":true,"input":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// room for a busy machine, under the cut
	room := proxyCut - 2*time.Second
	if d := time.Since(began); d >= room || resp.StatusCode != 200 {
		t.Fatalf("headers %d after %v; a proxy cutting at %v hangs up first", resp.StatusCode, d, proxyCut)
	}
	rd := bufio.NewReader(resp.Body)
	var body strings.Builder
	last, kept := time.Now(), 0
	for {
		line, err := rd.ReadString('\n')
		if gap := time.Since(last); line != "" && gap >= room && !strings.Contains(body.String(), "pong") {
			t.Fatalf("nothing for %v while the vendor was quiet: %q", gap, body.String())
		}
		last = time.Now()
		body.WriteString(line)
		if strings.HasPrefix(line, ": keepalive") {
			if kept++; kept == 2 {
				close(release)
			}
		}
		if err != nil {
			break
		}
	}
	if w := <-waited; w >= 2*proxyCut {
		t.Fatalf("the vendor was never released (%v): %q", w, body.String())
	}
	if !strings.Contains(body.String(), "pong") || !strings.Contains(body.String(), "response.completed") {
		t.Fatalf("the reply didn't follow the comments: %q", body.String())
	}
}
