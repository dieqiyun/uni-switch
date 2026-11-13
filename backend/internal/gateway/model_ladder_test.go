package gateway

import (
	"net/http"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #2241 (weizsw): on an account serving several models, a sibling's answer
// cleared the account's failure count, so a model that kept failing
// between them rested a minute every time, never longer. Its own count
// climbs now, cleared only by its own answer, and it rests alone while
// the account's other models go on answering; an account whose models
// all fail, with no answer between, still rests whole, as before.
func TestModelLadderClimbsPastSiblingAnswers(t *testing.T) {
	old := allowances
	defer func() { allowances = old }()
	allowances = func(string) map[string]provider.Allowance { return nil }
	s := &Server{}
	p := provider.Provider{ID: "antigravity", Account: &provider.Account{Agent: "antigravity", User: "seat@example.com"}}
	sick := candidate{p: p, model: "gemini-3.5-flash-lite", rest: "antigravity#seat"}
	well := candidate{p: p, model: "gemini-3.8-flash", rest: "antigravity#seat"}
	t.Cleanup(func() {
		for _, id := range []string{sick.restKey(), sick.restID(), well.restID()} {
			clearRest(id)
			routed.Lock()
			delete(routed.failures, id)
			routed.Unlock()
		}
	})
	body := []byte(`{"error":{"message":"the reply ended before it was complete","type":"api_error"}}`)
	near := func(d, want time.Duration) bool { return d > want-5*time.Second && d <= want }
	expire := func() {
		restingUntil.Lock()
		for _, id := range []string{sick.restKey(), sick.restID()} {
			if _, ok := restingUntil.m[id]; ok {
				restingUntil.m[id] = time.Now().Add(-time.Second)
			}
		}
		restingUntil.Unlock()
	}

	// the first failure says nothing of whose it is: the account rests
	r := s.restAfter(sick, http.StatusBadGateway, http.Header{}, body)
	if r.By != "backoff" || r.Failures != 1 || r.Key != sick.restKey() || !near(time.Until(r.Until), time.Minute) {
		t.Fatalf("first failure: %+v, %v", r, time.Until(r.Until))
	}
	// then the sibling answers, and the sick model fails again each time
	for i, want := range []time.Duration{2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 10 * time.Minute, 10 * time.Minute} {
		servedCandidate(well, 1)
		expire()
		r := s.restAfter(sick, http.StatusBadGateway, http.Header{}, body)
		if r.Failures != i+2 || !near(time.Until(r.Until), want) {
			t.Fatalf("failure #%d after a sibling's answer: %d failures, rests %v; want %v", i+2, r.Failures, time.Until(r.Until), want)
		}
		if r.Key != sick.restID() || r.Model != sick.model {
			t.Fatalf("failure #%d rests %q, want the model alone (%q)", i+2, r.Key, sick.restID())
		}
		// the account's other models are not set aside by it
		if resting(well) {
			t.Fatalf("failure #%d rested the sibling", i+2)
		}
		if !resting(sick) {
			t.Fatalf("failure #%d: the sick model isn't resting", i+2)
		}
		// and a sibling's answer doesn't end the model's own rest
		servedCandidate(well, 1)
		if !resting(sick) {
			t.Fatalf("failure #%d: a sibling's answer ended the model's rest", i+2)
		}
	}

	// its own answer starts it over
	servedCandidate(sick, 1)
	if resting(sick) {
		t.Fatal("still resting after its own answer")
	}
	if r := s.restAfter(sick, http.StatusBadGateway, http.Header{}, body); r.Failures != 1 || r.Key != sick.restKey() || !near(time.Until(r.Until), time.Minute) {
		t.Fatalf("after its own answer: %+v, %v", r, time.Until(r.Until))
	}

	// every model failing with no answer between: the account's ladder,
	// the account resting, as before
	expire()
	if r := s.restAfter(well, http.StatusBadGateway, http.Header{}, body); r.Failures != 2 || r.Key != sick.restKey() || !near(time.Until(r.Until), 2*time.Minute) {
		t.Fatalf("whole account failing: %+v, %v", r, time.Until(r.Until))
	}
}
