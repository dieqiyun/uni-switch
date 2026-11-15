package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// decideFleet is System One APIs whose every key answers as status says
// (200 when unset), and the keys asked in turn.
type decideFleet struct {
	mu     sync.Mutex
	status map[string]int
	asked  []string
}

func (f *decideFleet) set(key string, code int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status[key] = code
}

func (f *decideFleet) took() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.asked
	f.asked = nil
	return out
}

func (f *decideFleet) serve() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" {
			http.NotFound(w, r)
			return
		}
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		var q struct{ Model string }
		json.NewDecoder(r.Body).Decode(&q)
		f.mu.Lock()
		f.asked = append(f.asked, key)
		code := f.status[key]
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if code != 0 && code != 200 {
			w.WriteHeader(code)
			io.WriteString(w, `{"detail":{"message":"no capacity for `+key+`"}}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"model": q.Model, "answers": map[string]any{
			"levels": map[string]any{"type": "noul", "noul": 0},
			"intent": map[string]any{"type": "choice", "choice": "bug", "confidence": 0.9}},
			"usage": map[string]int{"input_tokens": 10, "output_tokens": 2}})
	}))
}

// One decision provider of several keys (fflake33 on Discord: TypeSafe Jev,
// Smart, a key out of quota) is asked over its keys in its routing's
// order, at /v1/systemone and as a group's classifier, as a decision
// group's members are: the key refused rests, the next answers, and the
// next request asks the one that answered first.
func TestDecisionProviderKeysFailOver(t *testing.T) {
	for _, routing := range []string{"", provider.Ordered} {
		t.Run("routing="+routing, func(t *testing.T) {
			fresh(t)
			f := &decideFleet{status: map[string]int{}}
			up := f.serve()
			defer up.Close()
			for _, p := range []provider.Provider{
				{ID: "jev", Name: "TypeSafe Jev", Key: "k1", Keys: []provider.KeyAccount{{Key: "k2"}}, Decide: up.URL + "/v1", Routing: routing},
				{ID: "judge", Name: "Judge", Key: "c1", Keys: []provider.KeyAccount{{Key: "c2"}}, Decide: up.URL + "/v1", Routing: routing},
			} {
				if err := provider.Save(p); err != nil {
					t.Fatal(err)
				}
			}
			s := New()
			call := func() *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/v1/systemone", strings.NewReader(`{"model":"jev/jev-latest","state":{"message":"hi"},"questions":{"intent":{"type":"choice"}}}`))
				req.Header.Set("Authorization", "Bearer uni-switch")
				s.Handler().ServeHTTP(rec, req)
				return rec
			}
			// the first key out of quota: the second answers
			f.set("k1", 429)
			if rec := call(); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"choice":"bug"`) {
				t.Fatalf("systemone: %d %s", rec.Code, rec.Body)
			}
			if got := strings.Join(f.took(), ","); got != "k1,k2" {
				t.Fatalf("asked %s, want k1,k2", got)
			}
			r := s.trace.routes[len(s.trace.routes)-1]
			if r.Model != "jev/jev-latest" || r.Group != nil || !r.Done || r.Status != 200 || len(r.Tries) != 2 || r.Tries[0].Rest == nil || r.Tries[1].Rest != nil {
				t.Fatalf("route %+v", r)
			}
			if len(r.Usage) != 1 || r.Usage[0].Provider != "jev" {
				t.Fatalf("usage %+v", r.Usage)
			}
			// it rests: the next request asks the second alone
			if rec := call(); rec.Code != 200 {
				t.Fatalf("again: %d %s", rec.Code, rec.Body)
			}
			if got := strings.Join(f.took(), ","); got != "k2" {
				t.Fatalf("again asked %s, want k2", got)
			}

			// as a group's classifier, the same
			f.set("c1", 429)
			if v, err := s.askClassifier("judge/jev-latest", []string{"feature", "bug"}, before{}, false, "fix this"); err != nil || v.Intent != "bug" {
				t.Fatalf("classify: %+v %v", v, err)
			}
			asked := f.took()
			if len(asked) < 2 || asked[0] != "c1" || asked[len(asked)-1] != "c2" {
				t.Fatalf("classifier asked %v, want c1 then c2", asked)
			}
			if _, err := s.askClassifier("judge/jev-latest", []string{"feature", "bug"}, before{}, false, "fix this"); err != nil {
				t.Fatal(err)
			}
			if asked := f.took(); len(asked) == 0 || asked[0] != "c2" {
				t.Fatalf("classifier after the rest asked %v, want c2 first", asked)
			}
		})
	}
}

// A decision model's keys share the one classifier's wait, as Settings
// says it is given, its failover included: a key that never answers
// spends it, and the next is not asked with a whole wait of its own. Each
// try given the whole wait held a request a wait per key before routing
// it (Jason on Discord, held 2m and stopped with a 504).
func TestDecisionTriesShareTheClassifierWait(t *testing.T) {
	fresh(t)
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
		// never answers: the body read, so a client gone ends r's context
		io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-stop:
		}
	}))
	t.Cleanup(up.Close)
	t.Cleanup(func() { close(stop) }) // runs before up.Close
	if err := provider.Save(provider.Provider{ID: "jw", Name: "JW", Key: "w1", Keys: []provider.KeyAccount{{Key: "w2"}, {Key: "w3"}}, Decide: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	s := New()
	_, err := s.askClassifier("jw/jev-latest", []string{"feature", "bug"}, before{}, false, "fix this")
	if err == nil || !strings.Contains(err.Error(), "no answer in 200ms") {
		t.Fatalf("a decision model that never answers: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 1 {
		t.Fatalf("asked %v in one classifier's wait, want the first key alone", asked)
	}
}
