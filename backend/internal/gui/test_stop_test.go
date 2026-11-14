package gui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// RIck on Discord: a test couldn't be stopped. The window's Stop ends a
// test by its run name (teststop), and the request to the vendor with it,
// though the test's own request isn't ended: WebView2 doesn't end it when
// the page aborts its fetch, and httptest's request here never ends.
func TestProviderTestStops(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	arrived, ended := make(chan struct{}, 8), make(chan struct{}, 8)
	up := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost { // the endpoint test's Fetch
			_, _ = rw.Write([]byte(`{"data":[{"id":"m1"}]}`))
			return
		}
		// read whole, or the server never notices the client went
		// away and the context below never ends
		_, _ = io.Copy(io.Discard, r.Body)
		arrived <- struct{}{}
		<-r.Context().Done() // a vendor that never answers
		ended <- struct{}{}
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "slow", Name: "Slow", Chat: up.URL + "/v1", Key: "sk-slow", KeyProtocol: provider.Chat, Icon: "generic"}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	post := func(action, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/"+action, strings.NewReader(body)))
		return w
	}
	for _, c := range []struct{ name, body string }{
		{"models", `{"id":"slow","test":["m1"],"run":"r-models"}`},
		{"endpoints", `{"id":"slow","run":"r-endpoints"}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := make(chan *httptest.ResponseRecorder, 1)
			go func() { got <- post("test", c.body) }()
			select {
			case <-arrived:
			case w := <-got:
				t.Fatalf("the vendor was never asked: %d %s", w.Code, w.Body)
			}
			var run struct{ Run string }
			_ = json.Unmarshal([]byte(c.body), &run)
			if w := post("teststop", `{"run":"`+run.Run+`"}`); w.Code != http.StatusNoContent {
				t.Fatalf("teststop: %d %s", w.Code, w.Body)
			}
			select {
			case w := <-got:
				if w.Code != 200 || !strings.Contains(w.Body.String(), "canceled") {
					t.Errorf("the stopped test answered %d %s", w.Code, w.Body)
				}
			case <-time.After(15 * time.Second): // the probe's own wait is 20s
				t.Fatal("the test went on after Stop")
			}
			select { // the request to the vendor ended with the test
			case <-ended:
			case <-time.After(15 * time.Second):
				t.Fatal("the vendor's request went on after Stop")
			}
		})
	}
}
