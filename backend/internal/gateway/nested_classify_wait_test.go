package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A decision model's key cut off by what a request had left of the
// classifier wait isn't rested for it: it was given less than its own
// wait, so the next request asks it first again.
func TestDecisionKeyCutShortIsNotRested(t *testing.T) {
	fresh(t)
	resetClassified(t)
	was := classifyUnit
	classifyUnit = 25 * time.Millisecond // the 8 seconds unset are 200ms
	t.Cleanup(func() { classifyUnit = was })
	var mu sync.Mutex
	var asked []string
	stop := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		mu.Unlock()
		io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-stop:
		}
	}))
	t.Cleanup(up.Close)
	t.Cleanup(func() { close(stop) })
	if err := provider.Save(provider.Provider{ID: "jw", Name: "JW", Key: "w1", Keys: []provider.KeyAccount{{Key: "w2"}}, Decide: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	s := New()
	_, err := s.askClassifierIn(50*time.Millisecond, "jw/jev-latest", []string{"feature", "bug"}, before{}, false, "fix this")
	if err == nil || !strings.Contains(err.Error(), "left of the request's classifier wait") {
		t.Fatalf("cut short: %v", err)
	}
	if _, err := s.askClassifier("jw/jev-latest", []string{"feature", "bug"}, before{}, false, "fix this"); err == nil {
		t.Fatal("a decision model that never answers answered")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 2 || asked[0] != "w1" || asked[1] != "w1" {
		t.Fatalf("asked %v: the key cut short was rested", asked)
	}
}
