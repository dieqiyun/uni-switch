package library

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// slowMCP is a streamable HTTP server that takes its time over each step,
// as xiaozhu1337's remote servers do (#1467): wait[method] before it
// answers, forever for a negative one. lists counts its tools/list calls.
func slowMCP(wait map[string]time.Duration, lists *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(204)
			return
		}
		var m struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		json.NewDecoder(r.Body).Decode(&m)
		if m.Method == "tools/list" {
			lists.Add(1)
		}
		if d := wait[m.Method]; d < 0 {
			<-r.Context().Done()
			return
		} else if d > 0 {
			select {
			case <-time.After(d):
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		switch m.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "s-1")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2025-06-18","capabilities":{}}}`, *m.ID)
		case "notifications/initialized":
			w.WriteHeader(202)
		case "tools/list":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"a"}]}}`, *m.ID)
		default:
			io.WriteString(w, `{}`)
		}
	}
}

// A slow but healthy remote server is ok: each step has its own budget, so
// one that takes most of it at every step still lists its tools. With one
// budget for the whole check, as before, it was "no answer".
func TestCheckSlowRemoteServer(t *testing.T) {
	if remoteStep < 60*time.Second || stdioStep >= remoteStep {
		t.Errorf("budgets: a URL's step %s, a command's %s; want 60s or more for a URL, less for a command", remoteStep, stdioStep)
	}
	short(t, 2*time.Second)
	var lists atomic.Int32
	// 2.9s in all, each step 0.6s or more inside its 2s
	srv := httptest.NewServer(slowMCP(map[string]time.Duration{
		"initialize":                1200 * time.Millisecond,
		"notifications/initialized": 300 * time.Millisecond,
		"tools/list":                1400 * time.Millisecond,
	}, &lists))
	defer srv.Close()
	h := CheckServer(context.Background(), &Server{Name: "slow", Transport: "http", URL: srv.URL})
	if h.State != "ok" || h.Tools != 1 {
		t.Fatalf("got %+v, want ok with 1 tool", h)
	}
}

// A server that doesn't answer says which step it didn't answer, and how
// long it was waited for.
func TestCheckTimeoutNamesTheStep(t *testing.T) {
	short(t, 500*time.Millisecond)
	for _, step := range []string{"initialize", "notifications/initialized", "tools/list"} {
		t.Run(step, func(t *testing.T) {
			var lists atomic.Int32
			srv := httptest.NewServer(slowMCP(map[string]time.Duration{step: -1}, &lists))
			defer srv.Close()
			h := CheckServer(context.Background(), &Server{Name: "slow", Transport: "http", URL: srv.URL})
			if h.State != "error" || h.Why != "timeout" || h.Step != step || h.Waited != 500 {
				t.Fatalf("got %+v, want a timeout at %s after 500ms", h, step)
			}
		})
	}
}

// A server that timed out isn't kept as "no answer" for the session: the
// next look asks it again, and finds it answering once it does.
func TestCheckTimeoutAskedAgain(t *testing.T) {
	sandbox(t)
	checked.Lock()
	checked.m = map[string]checkedServer{}
	checked.Unlock()
	short(t, 500*time.Millisecond)
	var lists atomic.Int32
	wait := map[string]time.Duration{"tools/list": -1}
	var slow atomic.Bool
	slow.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if slow.Load() {
			slowMCP(wait, &lists)(w, r)
			return
		}
		slowMCP(nil, &lists)(w, r)
	}))
	defer srv.Close()
	if _, err := SaveServer("", Server{Name: "slow", Transport: "http", URL: srv.URL, Agents: []string{}}); err != nil {
		t.Fatal(err)
	}
	m, err := CheckServers(context.Background(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if m["slow"].Why != "timeout" || lists.Load() != 1 {
		t.Fatalf("first check %+v, %d lists", m, lists.Load())
	}
	slow.Store(false)
	m, _ = CheckServers(context.Background(), nil, false)
	if m["slow"].State != "ok" || lists.Load() != 2 {
		t.Fatalf("second check %+v, %d lists: the timeout was kept", m, lists.Load())
	}
	// an answer is kept, as before
	CheckServers(context.Background(), nil, false)
	if lists.Load() != 2 {
		t.Fatalf("third check asked again: %d lists", lists.Load())
	}
	// asked afresh, it times out: the next look asks again rather than
	// show the answer from before
	slow.Store(true)
	if m, _ = CheckServers(context.Background(), nil, true); m["slow"].Why != "timeout" || lists.Load() != 3 {
		t.Fatalf("fresh check %+v, %d lists", m, lists.Load())
	}
	slow.Store(false)
	if m, _ = CheckServers(context.Background(), nil, false); m["slow"].State != "ok" || lists.Load() != 4 {
		t.Fatalf("after a fresh timeout %+v, %d lists: the answer from before was shown", m, lists.Load())
	}
}
