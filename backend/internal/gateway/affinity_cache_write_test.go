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

// cacheWriter is an Anthropic Messages upstream that answers as each key,
// reporting the cache as Anthropic does: a first turn writes the prompt
// (cache_creation_input_tokens) and reads none of it.
type cacheWriter struct {
	mu          sync.Mutex
	tried       []string
	read, wrote int
	hour        int // of wrote, how much for an hour (cache_creation's split)
}

func (c *cacheWriter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.ReadAll(r.Body)
	key := r.Header.Get("x-api-key")
	if key == "" {
		key = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	c.mu.Lock()
	c.tried = append(c.tried, key)
	read, wrote, hour := c.read, c.wrote, c.hour
	c.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	usage := `"cache_read_input_tokens":` + itoa(read) + `,"cache_creation_input_tokens":` + itoa(wrote)
	if hour > 0 {
		usage += `,"cache_creation":{"ephemeral_5m_input_tokens":` + itoa(wrote-hour) + `,"ephemeral_1h_input_tokens":` + itoa(hour) + `}`
	}
	for _, ev := range []string{
		`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"usage":{"input_tokens":12,"output_tokens":1,` + usage + `}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"from ` + key + `"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
		`{"type":"message_stop"}`,
	} {
		var t struct {
			Type string `json:"type"`
		}
		json.Unmarshal([]byte(ev), &t)
		io.WriteString(w, "event: "+t.Type+"\ndata: "+ev+"\n\n")
	}
}

// What the last reply wrote to the vendor's cache counts with what it
// read: the first turn of a conversation reads nothing and writes the
// whole prompt, and its second turn stays to read it (vilicvane on
// Discord: 「会话保持 → 自动」 moved the second turn as "no-cache", and the
// cache just built was thrown away). Kept across a restart too, as the
// file has it; a reply that neither read nor wrote enough still moves.
func TestAffinityKeepsACacheJustWritten(t *testing.T) {
	const (
		first = `{"model":"cw/m","messages":[{"role":"user","content":"hi"}]}`
		again = `{"model":"cw/m","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"yo"},{"role":"user","content":"more"}]}`
	)
	for _, c := range []struct {
		name        string
		read, wrote int
		restart     bool
		why         string
	}{
		{"written", 0, 3000, false, "cache"},
		{"written, restarted", 0, 3000, true, "cache"},
		{"read and written together", 600, 600, false, "cache"},
		{"too little either way", 300, 300, false, "no-cache"},
	} {
		fresh(t)
		v := &cacheWriter{read: c.read, wrote: c.wrote}
		up := httptest.NewServer(v)
		t.Cleanup(up.Close)
		p := provider.Provider{ID: "cw", Name: "CW", Key: "k1", Models: []string{"m"}, Anthropic: up.URL + "/v1",
			Keys: []provider.KeyAccount{{Key: "k2"}}}
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
		s := New()
		if code, body := postAs(t, s, "s1", first); code != 200 {
			t.Fatalf("%s: first turn %d %s", c.name, code, body)
		}
		if c.restart {
			sticks.Lock()
			sticks.m, sticks.from, sticks.mod = map[string]stick{}, "", time.Time{}
			sticks.Unlock()
			s = New()
		}
		if code, body := postAs(t, s, "s1", again); code != 200 {
			t.Fatalf("%s: second turn %d %s", c.name, code, body)
		}
		a := s.trace.routes[len(s.trace.routes)-1].Affinity
		if a == nil || a.Why != c.why || a.Kept != (c.why == "cache") {
			t.Fatalf("%s: affinity %+v, want %s", c.name, a, c.why)
		}
		if a.CacheRead != c.read || a.CacheWrite != c.wrote {
			t.Fatalf("%s: trace tells read %d wrote %d, want %d and %d", c.name, a.CacheRead, a.CacheWrite, c.read, c.wrote)
		}
	}
}

// A conversation whose last reply wrote Anthropic's 1-hour cache stays past
// five minutes, until the hour is up (#2302, j3r0lin: Stays: Auto called it
// "cold" at 5 minutes and moved it off the cache it could still read). A
// 5-minute write is cold at 5 minutes as before, an hour's is cold after
// the hour, and the hour is kept across a restart, as the file has it.
func TestAffinityKeepsAnHourCachePastFiveMinutes(t *testing.T) {
	const (
		first = `{"model":"cw/m","messages":[{"role":"user","content":"hi"}]}`
		again = `{"model":"cw/m","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"yo"},{"role":"user","content":"more"}]}`
	)
	for _, c := range []struct {
		name        string
		wrote, hour int
		ago         time.Duration
		restart     bool
		why         string
	}{
		{"an hour's write, 20 minutes on", 3000, 3000, 20 * time.Minute, false, "cache"},
		{"an hour's write, 59 minutes on, restarted", 3000, 3000, 59 * time.Minute, true, "cache"},
		{"an hour's write, 61 minutes on", 3000, 3000, 61 * time.Minute, false, "cold"},
		{"5 minutes' write, 6 minutes on", 3000, 0, 6 * time.Minute, false, "cold"},
		{"too little for an hour, 20 minutes on", 3000, 500, 20 * time.Minute, false, "cold"},
	} {
		fresh(t)
		v := &cacheWriter{wrote: c.wrote, hour: c.hour}
		up := httptest.NewServer(v)
		t.Cleanup(up.Close)
		p := provider.Provider{ID: "cw", Name: "CW", Key: "k1", Models: []string{"m"}, Anthropic: up.URL + "/v1",
			Keys: []provider.KeyAccount{{Key: "k2"}}}
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
		s := New()
		if code, body := postAs(t, s, "s1", first); code != 200 {
			t.Fatalf("%s: first turn %d %s", c.name, code, body)
		}
		sticks.Lock()
		if len(sticks.m) != 1 {
			t.Fatalf("%s: %d conversations remembered, want 1", c.name, len(sticks.m))
		}
		for k, st := range sticks.m {
			if want := c.hour; (want >= cacheWorth) != (st.hour.tokens >= cacheWorth) {
				t.Errorf("%s: kept for an hour %d, want %d", c.name, st.hour.tokens, want)
			}
			st.at = st.at.Add(-c.ago)
			st.hour.at = st.hour.at.Add(-c.ago)
			sticks.m[k] = st
		}
		sticks.Unlock()
		if c.restart {
			saveSticks()
			sticks.Lock()
			sticks.m, sticks.from, sticks.mod = map[string]stick{}, "", time.Time{}
			sticks.Unlock()
			s = New()
		}
		if code, body := postAs(t, s, "s1", again); code != 200 {
			t.Fatalf("%s: second turn %d %s", c.name, code, body)
		}
		a := s.trace.routes[len(s.trace.routes)-1].Affinity
		if a == nil || a.Why != c.why || a.Kept != (c.why == "cache") {
			t.Fatalf("%s: affinity %+v, want %s", c.name, a, c.why)
		}
	}
}

// What is kept for an hour: a 1-hour write with what was read before it; a
// read of all of it refreshes the hour; a shorter read, or a reply that
// wrote for 5 minutes only and read nothing of it, leaves none.
func TestHourCacheAfter(t *testing.T) {
	at := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	kept := hourCache{tokens: 5000, at: at}
	for _, c := range []struct {
		name string
		h    hourCache
		u    Usage
		now  time.Time
		want hourCache
	}{
		{"written for an hour", hourCache{}, Usage{CacheRead: 2000, CacheWrite: 3000, CacheWrite1h: 3000}, at, hourCache{5000, at}},
		{"read whole, 40 minutes on", kept, Usage{CacheRead: 5200, CacheWrite: 300}, at.Add(40 * time.Minute), hourCache{5000, at.Add(40 * time.Minute)}},
		{"read short", kept, Usage{CacheRead: 1000, CacheWrite: 4000}, at.Add(time.Minute), hourCache{}},
		{"read after the hour", kept, Usage{CacheRead: 5200}, at.Add(61 * time.Minute), hourCache{}},
		{"5 minutes only", hourCache{}, Usage{CacheWrite: 3000}, at, hourCache{}},
	} {
		if got := c.h.after(c.u, c.now); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
	if !kept.warm(at.Add(59*time.Minute)) || kept.warm(at.Add(61*time.Minute)) || (hourCache{500, at}).warm(at) {
		t.Error("warm: an hour, of cacheWorth or more")
	}
}
