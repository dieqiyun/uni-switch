package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestURLs(t *testing.T) {
	t.Setenv("UNI_SWITCH_MIRRORS", "on")
	t.Setenv("UNI_SWITCH_NPM_REGISTRY", "")
	t.Setenv("UNI_SWITCH_GITHUB_MIRROR", "")

	got := URLs("https://registry.npmjs.org/@scope/pkg/latest?x=1")
	want := []string{
		"https://registry.npmjs.org/@scope/pkg/latest?x=1",
		"https://registry.npmmirror.com/@scope/pkg/latest?x=1",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("npm URLs = %q, want %q", got, want)
	}

	got = URLs("https://github.com/owner/repo/releases/download/v1/file")
	want = []string{
		"https://github.com/owner/repo/releases/download/v1/file",
		"https://gh-proxy.com/https://github.com/owner/repo/releases/download/v1/file",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("github URLs = %q, want %q", got, want)
	}

	t.Setenv("UNI_SWITCH_NPM_REGISTRY", "https://npm.example/")
	t.Setenv("UNI_SWITCH_GITHUB_MIRROR", "https://gh.example")
	got = URLs("https://registry.npmjs.org/pkg")
	if got[1] != "https://npm.example/pkg" {
		t.Fatalf("npm mirror = %q", got[1])
	}
	got = URLs("https://api.github.com/repos/o/r")
	if got[1] != "https://gh.example/https://api.github.com/repos/o/r" {
		t.Fatalf("github mirror = %q", got[1])
	}
}

func TestURLsDefaultOff(t *testing.T) {
	t.Setenv("UNI_SWITCH_MIRRORS", "")
	got := URLs("https://github.com/o/r")
	if len(got) != 1 || got[0] != "https://github.com/o/r" {
		t.Fatalf("URLs by default = %q", got)
	}
}

func TestURLsOff(t *testing.T) {
	t.Setenv("UNI_SWITCH_MIRRORS", "off")
	got := URLs("https://github.com/o/r")
	if len(got) != 1 || got[0] != "https://github.com/o/r" {
		t.Fatalf("URLs with mirrors off = %q", got)
	}
}

func TestDoFallsBack(t *testing.T) {
	var mirrorHits atomic.Int32
	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer official.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorHits.Add(1)
		io.WriteString(w, "from mirror")
	}))
	defer mirror.Close()

	req, _ := http.NewRequest("GET", official.URL+"/thing", nil)
	resp, err := do(http.DefaultClient, req, []string{official.URL + "/thing", mirror.URL + "/thing"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "from mirror" || mirrorHits.Load() != 1 {
		t.Fatalf("body %q, mirror hits %d", body, mirrorHits.Load())
	}
}

func TestDoFallsBackOnRateLimit(t *testing.T) {
	var mirrorHits atomic.Int32
	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		http.Error(w, "limited", http.StatusForbidden)
	}))
	defer official.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorHits.Add(1)
		io.WriteString(w, "from mirror")
	}))
	defer mirror.Close()

	req, _ := http.NewRequest("GET", official.URL+"/thing", nil)
	resp, err := do(http.DefaultClient, req, []string{official.URL + "/thing", mirror.URL + "/thing"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if mirrorHits.Load() != 1 {
		t.Fatalf("mirror hits %d", mirrorHits.Load())
	}
}

func TestCredentialsStayOfficial(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/o/r", nil)
	if !mirrorable(req) {
		t.Fatal("a request without credentials can't use a mirror")
	}
	req.Header.Set("Authorization", "Bearer secret")
	if mirrorable(req) {
		t.Fatal("a request with Authorization can use a mirror")
	}
	req.Header.Del("Authorization")
	req.Header.Set("Cookie", "session=secret")
	if mirrorable(req) {
		t.Fatal("a request with a cookie can use a mirror")
	}
	req.Header.Del("Cookie")
	req.URL.User = url.UserPassword("user", "secret")
	if mirrorable(req) {
		t.Fatal("a request with URL userinfo can use a mirror")
	}
	req.URL.User = nil
	q := req.URL.Query()
	q.Set("access_token", "secret")
	req.URL.RawQuery = q.Encode()
	if mirrorable(req) {
		t.Fatal("a request with an access_token can use a mirror")
	}
}

func TestDoOfficialSkipsMirror(t *testing.T) {
	var mirrorHits atomic.Int32
	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "official")
	}))
	defer official.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorHits.Add(1)
		io.WriteString(w, "mirror")
	}))
	defer mirror.Close()
	t.Setenv("UNI_SWITCH_GITHUB_MIRROR", mirror.URL)

	req, _ := http.NewRequest("GET", official.URL+"/thing", nil)
	resp, err := DoOfficial(http.DefaultClient, req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if mirrorHits.Load() != 0 {
		t.Fatalf("mirror hits %d", mirrorHits.Load())
	}
}

func TestDoJoinsErrors(t *testing.T) {
	official := "http://127.0.0.1:1/thing"
	mirror := "http://127.0.0.1:2/thing"
	req, _ := http.NewRequest("GET", official, nil)
	_, err := do(http.DefaultClient, req, []string{official, mirror})
	if err == nil {
		t.Fatal("both sources failed but no error")
	}
	if !strings.Contains(err.Error(), official) || !strings.Contains(err.Error(), mirror) {
		t.Fatalf("error %q doesn't name both sources", err)
	}
}

func TestDoKeepsAnAnswer(t *testing.T) {
	var mirrorHits atomic.Int32
	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer official.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorHits.Add(1)
	}))
	defer mirror.Close()

	req, _ := http.NewRequest("GET", official.URL+"/thing", nil)
	resp, err := do(http.DefaultClient, req, []string{official.URL + "/thing", mirror.URL + "/thing"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || mirrorHits.Load() != 0 {
		t.Fatalf("status %d, mirror hits %d", resp.StatusCode, mirrorHits.Load())
	}
}

func TestDoBodyOutlivesCandidateTimeout(t *testing.T) {
	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer official.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(1200 * time.Millisecond)
		io.WriteString(w, "slow mirror")
	}))
	defer mirror.Close()

	req, _ := http.NewRequest("GET", official.URL+"/thing", nil)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := do(client, req, []string{official.URL + "/thing", mirror.URL + "/thing"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "slow mirror" {
		t.Fatalf("body = %q", body)
	}
}

type recordedTransport struct {
	requests []string
	auth     []string // each request's Authorization
}

func (rt *recordedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.requests = append(rt.requests, req.URL.String())
	rt.auth = append(rt.auth, req.Header.Get("Authorization"))
	status := http.StatusBadGateway
	if strings.HasPrefix(req.URL.String(), "https://gh.example/") {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

// A request to GitHub whose one credential is the user's token reaches a
// mirror without it (#3923): the official address is asked with it, the
// mirror never sees it. Any other credential, or a host other than
// GitHub's, keeps the request official.
func TestDoKeepsCredentialsOfficial(t *testing.T) {
	t.Setenv("UNI_SWITCH_MIRRORS", "on")
	t.Setenv("UNI_SWITCH_GITHUB_MIRROR", "https://gh.example")
	t.Setenv("UNI_SWITCH_NPM_REGISTRY", "https://gh.example/npm")
	send := func(raw string, h map[string]string, do func(*http.Client, *http.Request) (*http.Response, error)) (*recordedTransport, *http.Response) {
		t.Helper()
		rt := &recordedTransport{}
		req, _ := http.NewRequest("GET", raw, nil)
		for k, v := range h {
			req.Header.Set(k, v)
		}
		resp, err := do(&http.Client{Transport: rt}, req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if req.Header.Get("Authorization") != h["Authorization"] {
			t.Fatalf("the caller's request lost its token: %q", req.Header.Get("Authorization"))
		}
		return rt, resp
	}

	rt, resp := send("https://api.github.com/repos/o/r", map[string]string{"Authorization": "Bearer secret"}, Do)
	if fmt.Sprint(rt.requests) != "[https://api.github.com/repos/o/r https://gh.example/https://api.github.com/repos/o/r]" {
		t.Fatalf("requests = %q", rt.requests)
	}
	if rt.auth[0] != "Bearer secret" || rt.auth[1] != "" {
		t.Fatalf("Authorization = %q, want the token official only", rt.auth)
	}
	if resp.StatusCode != http.StatusOK || resp.Request.Header.Get("Authorization") != "" {
		t.Fatalf("answer %d, sent with %q", resp.StatusCode, resp.Request.Header.Get("Authorization"))
	}

	for _, c := range []struct {
		raw string
		h   map[string]string
	}{
		{"https://api.github.com/repos/o/r", map[string]string{"Authorization": "Bearer secret", "Cookie": "s=1"}},
		{"https://api.github.com/repos/o/r?access_token=x", map[string]string{"Authorization": "Bearer secret"}},
		{"https://registry.npmjs.org/pkg", map[string]string{"Authorization": "Bearer secret"}},
	} {
		if rt, _ := send(c.raw, c.h, Do); len(rt.requests) != 1 || rt.requests[0] != c.raw {
			t.Fatalf("%s %v: requests = %q", c.raw, c.h, rt.requests)
		}
	}
}

// budgetTransport answers the first (official) address only when its
// candidate context ends, and holds the last (mirror) address until its
// context ends too, recording how each one ended. Nothing here depends on
// how long a request takes to be scheduled: the assertions read which
// context fired and when, not whether a sleep finished in time (#2608).
type budgetTransport struct {
	official, mirror string
	beforeMirror     time.Duration

	mu          sync.Mutex
	officialErr error
	mirrorErr   error
	mirrorAt    time.Time
	mirrorEnd   time.Time
	mirrorDL    time.Time
	mirrorHasDL bool
}

func (rt *budgetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	switch req.URL.String() {
	case rt.official:
		<-ctx.Done()
		rt.mu.Lock()
		rt.officialErr = ctx.Err()
		rt.mu.Unlock()
		return nil, ctx.Err()
	case rt.mirror:
		if rt.beforeMirror > 0 {
			time.Sleep(rt.beforeMirror)
		}
		at := time.Now()
		dl, ok := ctx.Deadline()
		<-ctx.Done()
		rt.mu.Lock()
		rt.mirrorAt, rt.mirrorEnd = at, time.Now()
		rt.mirrorDL, rt.mirrorHasDL = dl, ok
		rt.mirrorErr = ctx.Err()
		rt.mu.Unlock()
		return nil, ctx.Err()
	}
	return nil, fmt.Errorf("unexpected request %s", req.URL)
}

func checkLastSourceBudget(t *testing.T, beforeMirror time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	rt := &budgetTransport{
		official:     "https://official.example/thing",
		mirror:       "https://mirror.example/thing",
		beforeMirror: beforeMirror,
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", rt.official, nil)
	resp, err := do(&http.Client{Transport: rt}, req, []string{rt.official, rt.mirror})
	if err == nil {
		resp.Body.Close()
		t.Fatal("do answered although both candidates failed")
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	// The first candidate is cut by its share of the budget, a second
	// before the parent's deadline, not by the deadline itself.
	if !errors.Is(rt.officialErr, context.Canceled) {
		t.Fatalf("official request ended with %v, want its candidate budget (context.Canceled)", rt.officialErr)
	}
	// The last candidate keeps the parent's deadline: its context carries
	// that deadline and ends only when the parent's does.
	if !rt.mirrorHasDL || !rt.mirrorDL.Equal(deadline) {
		t.Fatalf("mirror deadline = %v (set %v), want the parent's %v", rt.mirrorDL, rt.mirrorHasDL, deadline)
	}
	if !errors.Is(rt.mirrorErr, context.DeadlineExceeded) || rt.mirrorEnd.Before(deadline) {
		t.Fatalf("mirror request ended with %v %v before the parent's deadline (started %v before it); the last source must get the remaining budget",
			rt.mirrorErr, deadline.Sub(rt.mirrorEnd), deadline.Sub(rt.mirrorAt))
	}
}

func TestDoLastSourceUsesRemainingBudget(t *testing.T) {
	checkLastSourceBudget(t, 0)
}

// A runner that is slow to send the mirror request (the 350ms pause #2608
// injected) still sees the mirror keep the parent's remaining deadline.
func TestDoLastSourceUsesRemainingBudgetWhenLate(t *testing.T) {
	checkLastSourceBudget(t, 350*time.Millisecond)
}
