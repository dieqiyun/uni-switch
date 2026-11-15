package netproxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// #1518: uni-switch started at login, before the proxy app. The system has no
// proxy yet, so requests go direct; once the proxy app sets one, uni-switch
// takes it within a few seconds, not the 15 a set proxy is kept for.
func TestSystemProxySetAfterStartIsTakenSoon(t *testing.T) {
	var p atomic.Value
	p.Store(Proxy{})
	oldRead, oldNone := readSystem, noneKeep
	readSystem = func() (Proxy, bool) { return p.Load().(Proxy), true }
	noneKeep = 50 * time.Millisecond
	sysCache.Lock()
	sysCache.at, sysCache.p = time.Time{}, Proxy{}
	sysCache.Unlock()
	t.Cleanup(func() {
		readSystem, noneKeep = oldRead, oldNone
		sysCache.Lock()
		sysCache.at, sysCache.p = time.Time{}, Proxy{}
		sysCache.Unlock()
	})

	if got := System(); got.URL != "" {
		t.Fatalf("no proxy yet, read %+v", got)
	}
	p.Store(Proxy{URL: "http://127.0.0.1:7890"})
	time.Sleep(200 * time.Millisecond)
	if got := System(); got.URL != "http://127.0.0.1:7890" {
		t.Fatalf("the proxy app set the system proxy, but uni-switch still reads %+v", got)
	}
	// A proxy that is set is kept for its own while.
	p.Store(Proxy{})
	time.Sleep(200 * time.Millisecond)
	if got := System(); got.URL != "http://127.0.0.1:7890" {
		t.Fatalf("a set proxy was read again within %v: %+v", systemKeep, got)
	}
}

// A request made with a held context never reaches the server, whatever
// the proxy: through http.DefaultClient (Dispatch over Func), and through
// a provider's own proxy.
func TestHeldRequestNeverLeaves(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = Func
	c := &http.Client{Transport: Dispatch(base)}
	defer c.CloseIdleConnections()
	why := errors.New("not asked")
	for _, ctx := range []context.Context{
		Hold(context.Background(), why),
		With(Hold(context.Background(), why), "direct"),
		Hold(With(context.Background(), "http://127.0.0.1:9"), why),
	} {
		req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
		if resp, err := c.Do(req); err == nil {
			resp.Body.Close()
			t.Fatalf("a held request was sent")
		} else if !errors.Is(err, why) {
			t.Fatalf("held request failed with %v, not its hold", err)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("server got %d held requests", hits.Load())
	}
	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if hits.Load() != 1 {
		t.Fatal("a request not held didn't reach the server")
	}
}
