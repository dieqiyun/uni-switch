package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// slowVendor streams a reply's first chunk at once and its end only when
// the test lets that request go (or its caller hangs up), counting how many
// it has out at once and the order they came in.
type slowVendor struct {
	mu      sync.Mutex
	order   []string
	out     int
	most    int
	release map[string]chan struct{}
	quit    chan struct{} // closed when the test ends, so nothing is left waiting
}

func (v *slowVendor) gate(tag string) chan struct{} {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.release[tag] == nil {
		v.release[tag] = make(chan struct{})
	}
	return v.release[tag]
}

func (v *slowVendor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var req struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	json.Unmarshal(b, &req)
	tag := req.Messages[0].Content
	v.mu.Lock()
	v.order = append(v.order, tag)
	v.out++
	v.most = max(v.most, v.out)
	v.mu.Unlock()
	defer func() {
		v.mu.Lock()
		v.out--
		v.mu.Unlock()
	}()
	gate := v.gate(tag)
	w.Header().Set("Content-Type", "text/event-stream")
	chunk := func(s string) {
		fmt.Fprintf(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%q}}]}\n\n", s)
		w.(http.Flusher).Flush()
	}
	chunk("from " + tag)
	select {
	case <-gate:
	case <-v.quit:
		return
	case <-r.Context().Done():
		return
	}
	chunk(" done")
	io.WriteString(w, "data: {\"id\":\"x\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
}

func (v *slowVendor) seen() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return slices.Clone(v.order)
}

// within waits for cond, failing the test after a few seconds.
func within(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A provider's MaxConcurrency (Discord, Lemon) is how many requests are out
// at the vendor at once: with 2, five sent together never have more than
// two out, the rest go in the order they came as each one before them is
// read to its end, an agent that hangs up while waiting leaves the queue
// without taking a slot or reaching the vendor, and one that hangs up
// mid-stream gives its slot back.
func TestMaxConcurrency(t *testing.T) {
	fresh(t)
	v := &slowVendor{release: map[string]chan struct{}{}, quit: make(chan struct{})}
	up := httptest.NewServer(v)
	t.Cleanup(up.Close)
	two := 2
	if err := provider.Save(provider.Provider{ID: "slow", Name: "Slow", Key: "k", Models: []string{"m"}, Chat: up.URL + "/v1", MaxConcurrency: &two}); err != nil {
		t.Fatal(err)
	}
	s := New()
	gw := httptest.NewServer(s.Handler())
	t.Cleanup(gw.Close)
	t.Cleanup(func() { close(v.quit) }) // runs first: a failed test leaves no handler waiting

	// a provider with a key has a lane per key
	lane := func() Lane { return s.Lanes()["slow#"+provider.KeyID("k")] }
	type reply struct {
		tag, body string
		err       error
	}
	replies := make(chan reply, 10)
	send := func(ctx context.Context, tag string) {
		body := `{"model":"slow/m","stream":true,"messages":[{"role":"user","content":"` + tag + `"}]}`
		go func() {
			req, _ := http.NewRequestWithContext(ctx, "POST", gw.URL+"/v1/chat/completions", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				replies <- reply{tag: tag, err: err}
				return
			}
			b, err := io.ReadAll(res.Body)
			res.Body.Close()
			replies <- reply{tag, string(b), err}
		}()
	}

	// two out at once, three waiting in the order they came
	tags := []string{"a", "b", "c", "d", "e"}
	for i, tag := range tags {
		send(context.Background(), tag)
		if i < 2 {
			within(t, tag+" at the vendor", func() bool { return len(v.seen()) == i+1 })
		} else {
			within(t, tag+" queued", func() bool { return lane().Waiting == i-1 })
		}
	}
	if l := lane(); l.Busy != 2 || l.Waiting != 3 || l.Limit != 2 {
		t.Fatalf("lane = %+v, want 2 out, 3 waiting", l)
	}

	// one more, gone while it waits: out of the queue, never sent, no slot
	ctx, cancel := context.WithCancel(context.Background())
	send(ctx, "gone")
	within(t, "gone queued", func() bool { return lane().Waiting == 4 })
	cancel()
	within(t, "gone out of the queue", func() bool { return lane().Waiting == 3 })
	if r := <-replies; r.tag != "gone" || r.err == nil {
		t.Fatalf("the canceled request answered: %+v", r)
	}
	if l := lane(); l.Busy != 2 {
		t.Fatalf("lane after a cancel in the queue = %+v, want 2 out", l)
	}

	// each one done lets the next in, first come first served
	for i, tag := range tags {
		close(v.gate(tag))
		r := <-replies
		if r.err != nil || r.tag != tag || !strings.Contains(r.body, "from "+tag) || !strings.Contains(r.body, "[DONE]") {
			t.Fatalf("reply %d = %+v, want %s's whole stream", i, r, tag)
		}
		if next := i + 2; next < len(tags) {
			within(t, tags[next]+" at the vendor", func() bool { return len(v.seen()) == next+1 })
		}
	}
	if got := v.seen(); !slices.Equal(got, tags) {
		t.Fatalf("vendor saw %v, want %v in order", got, tags)
	}
	if v.most != 2 {
		t.Fatalf("most out at once = %d, want 2", v.most)
	}
	within(t, "every slot given back", func() bool { return len(s.Lanes()) == 0 })

	// an agent hanging up mid-stream gives its slot back
	ctx, cancel = context.WithCancel(context.Background())
	body := `{"model":"slow/m","stream":true,"messages":[{"role":"user","content":"hangup"}]}`
	req, _ := http.NewRequestWithContext(ctx, "POST", gw.URL+"/v1/chat/completions", strings.NewReader(body))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(res.Body).ReadString('\n')
	if !strings.Contains(line, "from hangup") {
		t.Fatalf("first line = %q", line)
	}
	if l := lane(); l.Busy != 1 {
		t.Fatalf("lane mid-stream = %+v, want 1 out", l)
	}
	cancel()
	res.Body.Close()
	within(t, "the hung-up slot given back", func() bool { return len(s.Lanes()) == 0 })

	// and the next is served at once
	send(context.Background(), "after")
	within(t, "after at the vendor", func() bool { return slices.Contains(v.seen(), "after") })
	close(v.gate("after"))
	if r := <-replies; r.err != nil || !strings.Contains(r.body, "[DONE]") {
		t.Fatalf("after = %+v", r)
	}
}

// A queued request's wait is told in its route's try, and a limit lifted
// lets whoever waits go at once.
func TestLanesLimitLifted(t *testing.T) {
	var l lanes
	r1, ok := l.acquire(context.Background(), "p", 1)
	if !ok {
		t.Fatal("first slot refused")
	}
	got := make(chan func(), 1)
	go func() {
		r, _ := l.acquire(context.Background(), "p", 1)
		got <- r
	}()
	within(t, "second queued", func() bool {
		l.mu.Lock()
		defer l.mu.Unlock()
		return l.m["p"] != nil && len(l.m["p"].queue) == 1
	})
	r3, ok := l.acquire(context.Background(), "p", 0)
	if !ok {
		t.Fatal("no limit refused")
	}
	select {
	case r2 := <-got:
		r2()
	case <-time.After(2 * time.Second):
		t.Fatal("lifting the limit left the queue waiting")
	}
	r1()
	r1() // twice is once
	r3()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.m) != 0 {
		t.Fatalf("lanes left: %+v", l.m["p"])
	}
}

// A key with no limit on requests at once still has its requests out
// counted (chentuoyan on Discord: the Providers page's pill says how many
// run), as a Lane with Limit 0 and nobody waiting, gone once they are done.
func TestLanesCountWithNoLimit(t *testing.T) {
	fresh(t)
	v := &slowVendor{release: map[string]chan struct{}{}, quit: make(chan struct{})}
	up := httptest.NewServer(v)
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "slow", Name: "Slow", Key: "k", Models: []string{"m"}, Chat: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	s := New()
	gw := httptest.NewServer(s.Handler())
	t.Cleanup(gw.Close)
	t.Cleanup(func() { close(v.quit) })
	lane := func() Lane { return s.Lanes()["slow#"+provider.KeyID("k")] }
	replies := make(chan error, 3)
	for _, tag := range []string{"a", "b", "c"} {
		body := `{"model":"slow/m","stream":true,"messages":[{"role":"user","content":"` + tag + `"}]}`
		go func() {
			res, err := http.Post(gw.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
			if err == nil {
				_, err = io.ReadAll(res.Body)
				res.Body.Close()
			}
			replies <- err
		}()
	}
	within(t, "three at the vendor", func() bool { return len(v.seen()) == 3 })
	if l := lane(); l != (Lane{Busy: 3}) {
		t.Fatalf("lane = %+v, want 3 out, none waiting, no limit", l)
	}
	for _, tag := range []string{"a", "b", "c"} {
		close(v.gate(tag))
		if err := <-replies; err != nil {
			t.Fatal(err)
		}
	}
	within(t, "every count given back", func() bool { return len(s.Lanes()) == 0 })
}

// Concurrency is the user's setting, else none; 0 set is none, and a
// negative one saved is 0.
func TestProviderConcurrency(t *testing.T) {
	n := 3
	if c := (provider.Provider{MaxConcurrency: &n}).Concurrency(); c != 3 {
		t.Fatalf("Concurrency = %d, want 3", c)
	}
	if c := (provider.Provider{}).Concurrency(); c != 0 {
		t.Fatalf("Concurrency unset = %d, want 0", c)
	}
	fresh(t)
	neg := -4
	if err := provider.Save(provider.Provider{ID: "neg", Name: "Neg", Key: "k", Chat: "https://example.invalid/v1", MaxConcurrency: &neg}); err != nil {
		t.Fatal(err)
	}
	p, err := provider.Find("neg")
	if err != nil {
		t.Fatal(err)
	}
	if p.MaxConcurrency == nil || *p.MaxConcurrency != 0 {
		t.Fatalf("saved %v, want 0", p.MaxConcurrency)
	}
}
