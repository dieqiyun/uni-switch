package provider

import (
	"context"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"
)

// RIck on Discord: a failing test took too long to say so. A host whose
// connection is never had (here a TLS handshake nothing answers) fails
// each request at connectWait, and the run's other models there at once,
// with the same words: no more than the first four (TestModels' own
// concurrency) reach it.
func TestTestModelsUnreachedHostFailsOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var accepted atomic.Int32
	var mu sync.Mutex
	var conns []net.Conn
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			mu.Lock()
			conns = append(conns, c) // held open, never answered
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		<-done
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		mu.Unlock()
	})
	was := connectWait
	connectWait = 200 * time.Millisecond
	t.Cleanup(func() { connectWait = was })

	p := Provider{ID: "dead", Name: "Dead", Chat: "https://" + ln.Addr().String() + "/v1", Key: "sk-dead", KeyProtocol: Chat}
	models := []string{"m1", "m2", "m3", "m4", "m5", "m6", "m7", "m8"}
	rs := p.TestModels(context.Background(), models)
	want := "couldn't connect to " + ln.Addr().String() + " in 200ms"
	for i, r := range rs {
		if r.OK || r.Error != want || r.Model != models[i] {
			t.Errorf("%s: %+v, want the error %q", models[i], r, want)
		}
	}
	if n := accepted.Load(); n > 4 {
		t.Errorf("%d connections were tried, each model's own; the first wave's (4 at most) should have told the rest", n)
	}
}

// A stream broken off (or held open past the probe's wait) before the
// model said a word is no answer: it used to read as answered.
func TestStreamAnswerBrokenOff(t *testing.T) {
	r := io.MultiReader(strings.NewReader(": keep-alive\n\ndata: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n"),
		iotest.ErrReader(errors.New("context deadline exceeded")))
	if msg, failed := streamAnswer(r); !failed || !strings.Contains(msg, "context deadline exceeded") {
		t.Fatalf("got %q, %v", msg, failed)
	}
	if _, failed := streamAnswer(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")); failed {
		t.Fatal("a stream that answered is failed")
	}
	if _, failed := streamAnswer(strings.NewReader("data: [DONE]\n\n")); failed {
		t.Fatal("a stream that ended cleanly is failed")
	}
}
