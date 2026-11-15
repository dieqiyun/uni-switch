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

// silentCache is a vendor that keeps the prompt cached by itself and reports
// nothing of it, as OpenAI's automatic prefix cache and Gemini do: a turn
// that read nothing tells neither a read nor a write, and one that read some
// of it tells the read (OpenAI's prompt_tokens_details.cached_tokens). usage
// false is a vendor that reports nothing of a turn's tokens at all.
type silentCache struct {
	mu    sync.Mutex
	tried []string
	usage bool
}

func (c *silentCache) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.ReadAll(r.Body)
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	c.mu.Lock()
	c.tried = append(c.tried, key)
	turn := len(c.tried)
	c.mu.Unlock()
	read := 0
	if turn > 1 {
		read = 2500 // of the cache the first turn built
	}
	told := ""
	if c.usage {
		told = `,"usage":{"prompt_tokens":3000,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":` + itoa(read) + `}}`
	}
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"from `+key+`"},"finish_reason":"stop"}]`+told+`}`)
}

// A vendor that caches without reporting a write (OpenAI's automatic prefix
// cache, Gemini) tells neither a read nor a write of the turn that built the
// cache, so the conversation's second turn moved as "no-cache": the whole
// prompt went afresh to another key and was paid for again. What such a
// vendor holds of the conversation after a turn is the prompt that turn was
// sent, which is what counts as written, so the second turn stays to read it.
// A vendor that reports no usage at all still moves: nothing there says a
// cache was built.
func TestAffinityKeepsACacheTheVendorKeptSilently(t *testing.T) {
	const (
		first = `{"model":"silent/m","messages":[{"role":"user","content":"hi"}]}`
		again = `{"model":"silent/m","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"yo"},{"role":"user","content":"more"}]}`
	)
	for _, c := range []struct {
		name  string
		usage bool
		why   string
	}{
		{"silent cache", true, "cache"},
		{"no usage at all", false, "no-cache"},
	} {
		fresh(t)
		v := &silentCache{usage: c.usage}
		serveOn(t, "silent", "k1", []string{"m"}, v, "k2")
		if err := provider.SetRouting("silent", provider.LeastUsed); err != nil {
			t.Fatal(err)
		}
		s := New()
		if code, body := postAs(t, s, "s1", first); code != 200 {
			t.Fatalf("%s: first turn %d %s", c.name, code, body)
		}
		if code, body := postAs(t, s, "s1", again); code != 200 {
			t.Fatalf("%s: second turn %d %s", c.name, code, body)
		}
		a := s.trace.routes[len(s.trace.routes)-1].Affinity
		if a == nil || a.Why != c.why || a.Kept != (c.why == "cache") {
			t.Fatalf("%s: affinity %+v, want %s", c.name, a, c.why)
		}
		if c.why == "cache" {
			if v.tried[1] != "k1" {
				t.Fatalf("%s: second turn went to %s, want k1 kept", c.name, v.tried[1])
			}
			if a.CacheRead != 0 || a.CacheWrite != 3000 {
				t.Fatalf("%s: trace tells read %d wrote %d, want the read 0 and this turn's prompt (3000) written", c.name, a.CacheRead, a.CacheWrite)
			}
		} else if v.tried[1] != "k2" {
			t.Fatalf("%s: second turn stayed on %s, want it moved to k2", c.name, v.tried[1])
		}
	}
}

// silentVendor is an upstream that caches a prompt by itself and never says
// it wrote one, as OpenAI's automatic prefix cache and Gemini's implicit
// cache report: the first request of a prompt reads none of it, a later one
// reads what an earlier one sent. It answers as each key, in OpenAI's Chat
// shape or Gemini's.
type silentVendor struct {
	mu     sync.Mutex
	gemini bool
	prompt int
	tried  []string
	seen   map[string]bool // keys that were sent this prompt before
}

func (c *silentVendor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.ReadAll(r.Body)
	key := r.Header.Get("x-goog-api-key")
	if key == "" {
		key = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	c.mu.Lock()
	c.tried = append(c.tried, key)
	read := 0
	if c.seen[key] {
		read = c.prompt * 5 / 6
	}
	if c.seen == nil {
		c.seen = map[string]bool{}
	}
	c.seen[key] = true
	prompt := c.prompt
	c.mu.Unlock()
	if c.gemini {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"from `+key+`"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":`+itoa(prompt)+`,"cachedContentTokenCount":`+itoa(read)+`,"candidatesTokenCount":3,"totalTokenCount":`+itoa(prompt+3)+`}}`,
		))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"id":"c1","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"from `+key+`"},"finish_reason":"stop"}],"usage":{"prompt_tokens":`+itoa(prompt)+`,"completion_tokens":3,"total_tokens":`+itoa(prompt+3)+`,"prompt_tokens_details":{"cached_tokens":`+itoa(read)+`}}}`)
}

// The same in Gemini's shape (cachedContentTokenCount), across a restart
// (the stick read back from affinity.json), and for a prompt too small for
// a vendor to cache, which still moves (#2230). Least used first puts the
// other key ahead after a turn, so a move is certain.
func TestAffinitySilentCacheGeminiRestartAndSmall(t *testing.T) {
	const (
		first = `{"model":"%s","messages":[{"role":"user","content":"hi"}]}`
		again = `{"model":"%s","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"yo"},{"role":"user","content":"more"}]}`
	)
	for _, c := range []struct {
		name    string
		gemini  bool
		prompt  int
		restart bool
		why     string
	}{
		{"openai", false, 3000, false, "cache"},
		{"openai, restarted", false, 3000, true, "cache"},
		{"gemini", true, 3000, false, "cache"},
		{"openai, prompt too small", false, 600, false, "no-cache"},
	} {
		fresh(t)
		v := &silentVendor{gemini: c.gemini, prompt: c.prompt}
		up := httptest.NewServer(v)
		t.Cleanup(up.Close)
		p := provider.Provider{ID: "silent", Name: "Silent", Key: "k1", Models: []string{"m"}, Keys: []provider.KeyAccount{{Key: "k2"}}}
		if c.gemini {
			p.Gemini = up.URL + "/v1beta"
		} else {
			p.Chat = up.URL + "/v1"
		}
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
		if err := provider.SetRouting("silent", provider.LeastUsed); err != nil {
			t.Fatal(err)
		}
		s := New()
		if code, body := postAs(t, s, "s1", strings.ReplaceAll(first, "%s", "silent/m")); code != 200 {
			t.Fatalf("%s: first turn %d %s", c.name, code, body)
		}
		if c.restart {
			sticks.Lock()
			sticks.m, sticks.from, sticks.mod = map[string]stick{}, "", time.Time{}
			sticks.Unlock()
			s = New()
		}
		if code, body := postAs(t, s, "s1", strings.ReplaceAll(again, "%s", "silent/m")); code != 200 {
			t.Fatalf("%s: second turn %d %s", c.name, code, body)
		}
		a := s.trace.routes[len(s.trace.routes)-1].Affinity
		if a == nil || a.Why != c.why || a.Kept != (c.why == "cache") {
			t.Fatalf("%s: second turn went to %v, affinity %+v, want %s", c.name, v.tried, a, c.why)
		}
		if a.CacheRead != 0 || a.CacheWrite != c.prompt {
			t.Fatalf("%s: trace tells read %d wrote %d, want 0 and the prompt sent, %d", c.name, a.CacheRead, a.CacheWrite, c.prompt)
		}
		if c.why == "cache" && (len(v.tried) != 2 || v.tried[1] != v.tried[0]) {
			t.Fatalf("%s: answered by %v, want the first turn's key again", c.name, v.tried)
		}
	}
}
