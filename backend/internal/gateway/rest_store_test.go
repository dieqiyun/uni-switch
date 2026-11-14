package gateway

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// restartRests is a uni-switch started again: what restingUntil held is
// gone, and the file is read as ListenAndServe reads it.
func restartRests() {
	restingUntil.Lock()
	restingUntil.m, restingUntil.note = map[string]time.Time{}, map[string]Rest{}
	restingUntil.Unlock()
	restsLoaded.Lock()
	restsLoaded.from = map[string]bool{}
	restsLoaded.Unlock()
	restsSaving.Lock()
	restsSaving.path, restsSaving.last = "", ""
	restsSaving.Unlock()
	loadRests()
}

// keyedOpenAI saves a provider with one key, as the user has it, and is
// that key as a candidate.
func keyedOpenAI(t *testing.T) candidate {
	t.Helper()
	p := provider.Provider{ID: "openai", Name: "OpenAI", Key: "sk-x", Chat: "http://127.0.0.1:1/v1", Models: []string{"gpt-x"}}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	return candidate{p: p, model: "gpt-x", rest: "openai"}
}

// PaulT on Discord: a restart of uni-switch started an out-of-credit key's
// ladder over at 30 minutes, and lifted its rest. It now rests out what
// is left, and the next refusal climbs on from its rung.
func TestKeyCreditRestOutlivesRestart(t *testing.T) {
	fresh(t)
	s := New()
	key := keyedOpenAI(t)
	id := key.restKey()
	refuse := func() Rest {
		return s.restAfter(key, http.StatusPaymentRequired, http.Header{}, []byte(openAICreditsGone))
	}
	refuse() // 30m
	expire(id)
	saveRests()
	r := refuse() // 2h
	if r.Failures != 2 {
		t.Fatalf("second refusal: failures %d", r.Failures)
	}

	restartRests()
	got, ok := restOf(id)
	if !ok {
		t.Fatal("after a restart the key rests no more")
	}
	if !got.Until.Equal(r.Until) || got.Failures != 2 || got.Why != failCredit || got.By != "credit" || got.Key != id {
		t.Fatalf("after a restart: %+v, want until %v at rung 2", got, r.Until)
	}
	// refused again while it still rests: the same rung, as before it
	if r := refuse(); r.Failures != 2 {
		t.Fatalf("refused while resting after a restart: failures %d", r.Failures)
	}
	// its rest ended, uni-switch started again, and refused: a rung up, 8h
	expire(id)
	saveRests()
	restartRests()
	if _, ok := restOf(id); ok {
		t.Fatal("an ended rest rests again after a restart")
	}
	if r := refuse(); r.Failures != 3 || time.Until(r.Until) < creditLadder[2]-time.Minute {
		t.Fatalf("refused after a restart: failures %d, rests %v, want rung 3 (%v)", r.Failures, time.Until(r.Until).Round(time.Second), creditLadder[2])
	}

	// an answer starts it over, after a restart too
	servedCandidate(key, 1)
	restartRests()
	if r := refuse(); r.Failures != 1 {
		t.Fatalf("after an answer and a restart: failures %d", r.Failures)
	}
	// "Try now" too
	expire(id)
	saveRests()
	refuse() // rung 2
	s.Unrest(id)
	restartRests()
	if _, ok := restOf(id); ok {
		t.Fatal("Try now's lift came back after a restart")
	}
	if r := refuse(); r.Failures != 1 {
		t.Fatalf("after Try now and a restart: failures %d", r.Failures)
	}
}

// A key removed while uni-switch was off is let go, and one ended over a
// month ago starts over; a rest that isn't the ladder's isn't kept.
func TestKeyCreditRestFileForgets(t *testing.T) {
	fresh(t)
	s := New()
	key := keyedOpenAI(t)
	s.restAfter(key, http.StatusPaymentRequired, http.Header{}, []byte(openAICreditsGone))
	// a rate limit on another key is no ladder rest
	other := candidate{p: provider.Provider{ID: "openai"}, model: "gpt-y", rest: "openai#other"}
	s.restAfter(other, http.StatusTooManyRequests, http.Header{"Retry-After": {"600"}}, []byte(`{"error":{"message":"rate limited"}}`))
	saveRests()
	if err := provider.Delete("openai"); err != nil {
		t.Fatal(err)
	}
	restartRests()
	if _, ok := restOf(key.restKey()); ok {
		t.Fatal("a removed key rests after a restart")
	}
	if _, ok := restOf(other.restKey()); ok {
		t.Fatal("a rate limit's rest was kept over a restart")
	}

	key = keyedOpenAI(t)
	s.restAfter(key, http.StatusPaymentRequired, http.Header{}, []byte(openAICreditsGone))
	restingUntil.Lock()
	restingUntil.m[key.restKey()] = time.Now().Add(-restsKeep - time.Hour)
	restingUntil.Unlock()
	restsSaving.Lock()
	restsSaving.last = "" // written as it is now
	restsSaving.Unlock()
	saveRests()
	restartRests()
	if r := s.restAfter(key, http.StatusPaymentRequired, http.Header{}, []byte(openAICreditsGone)); r.Failures != 1 {
		t.Fatalf("a rest ended over a month ago climbed on: failures %d", r.Failures)
	}
}

// A file cut short, or not JSON, rests no one and doesn't stop the
// start; the next change writes it anew.
func TestKeyCreditRestFileCorrupt(t *testing.T) {
	for _, bad := range []string{`{"openai":{"why":"credit","by":"credit","failures":3,"until":"`, "not json", `[1,2]`, `{"openai":{"why":"credit","by":"credit","failures":3,"until":"never"}}`} {
		fresh(t)
		s := New()
		key := keyedOpenAI(t)
		if err := os.WriteFile(restsPath(), []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		restartRests()
		restingUntil.Lock()
		n := len(restingUntil.m)
		restingUntil.Unlock()
		if n != 0 {
			t.Fatalf("%q: %d rests read from a bad file", bad, n)
		}
		if r := s.restAfter(key, http.StatusPaymentRequired, http.Header{}, []byte(openAICreditsGone)); r.Failures != 1 {
			t.Fatalf("%q: failures %d after a bad file", bad, r.Failures)
		}
		restartRests()
		if r, ok := restOf(key.restKey()); !ok || r.Failures != 1 {
			t.Fatalf("%q: the file wasn't written anew: %+v %v", bad, r, ok)
		}
	}
}

// providers.json that can't be read says nothing of which keys are there:
// no rest is read on a guess.
func TestKeyCreditRestProvidersUnreadable(t *testing.T) {
	fresh(t)
	s := New()
	key := keyedOpenAI(t)
	s.restAfter(key, http.StatusPaymentRequired, http.Header{}, []byte(openAICreditsGone))
	// a folder where the file was: read, it fails
	if err := os.Remove(provider.Path()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(provider.Path(), 0o700); err != nil {
		t.Fatal(err)
	}
	restartRests()
	restingUntil.Lock()
	n := len(restingUntil.m)
	restingUntil.Unlock()
	if n != 0 {
		t.Fatalf("%d rests read with providers.json unreadable", n)
	}
}
