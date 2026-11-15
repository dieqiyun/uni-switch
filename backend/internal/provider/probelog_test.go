package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/probelog"
)

// probeRows catches what probelog writes down while the test runs, through
// the HTTP client's transport as netproxy.Install sets it.
func probeRows(t *testing.T) func() []probelog.Row {
	t.Helper()
	var mu sync.Mutex
	var rows []probelog.Row
	sink, tr := probelog.Sink, http.DefaultClient.Transport
	probelog.Sink = func(r probelog.Row) { mu.Lock(); rows = append(rows, r); mu.Unlock() }
	next := tr
	if next == nil {
		next = http.DefaultTransport
	}
	http.DefaultClient.Transport = probelog.Transport(next)
	t.Cleanup(func() { probelog.Sink, http.DefaultClient.Transport = sink, tr })
	return func() []probelog.Row {
		mu.Lock()
		defer mu.Unlock()
		return append([]probelog.Row(nil), rows...)
	}
}

func probeHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
}

// The editor's Test writes each request it sends down once: the model list
// as a model list, each endpoint's test as a test, with its model and the
// status the vendor answered (neiko, gys619 on Discord: a test left no
// trace in the request log).
func TestTestWritesItsRequestsDown(t *testing.T) {
	probeHome(t)
	rows := probeRows(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Method == http.MethodGet {
			_, _ = rw.Write([]byte(`{"data":[{"id":"gpt-6-sol"}]}`))
			return
		}
		if r.URL.Path == "/v1/messages" {
			rw.WriteHeader(http.StatusUnauthorized)
			_, _ = rw.Write([]byte(`{"error":{"message":"bad key"}}`))
			return
		}
		_, _ = rw.Write([]byte(`{}`))
	}))
	defer srv.Close()
	p := Provider{ID: "relay", Name: "Relay", Chat: srv.URL + "/v1", Anthropic: srv.URL,
		Key: "sk-chat", KeyProtocol: Chat, Models: []string{"gpt-6-sol"},
		Keys: []KeyAccount{{Key: "sk-anthropic", Protocol: Anthropic}}}
	p.Test(context.Background())
	got := rows()
	if int(hits.Load()) != len(got) {
		t.Fatalf("%d requests, %d rows: %+v", hits.Load(), len(got), got)
	}
	var lines []string
	for _, r := range got {
		if r.Provider != "relay" || r.Host != strings.TrimPrefix(srv.URL, "http://") {
			t.Errorf("row %+v", r)
		}
		lines = append(lines, r.Kind+" "+r.Endpoint+" "+r.Model+" "+http.StatusText(r.Status))
	}
	sort.Strings(lines)
	s := strings.Join(lines, "\n")
	for _, want := range []string{"model_list GET /v1/models  OK", "provider_test /v1/chat/completions gpt-6-sol OK", "provider_test /v1/messages "} {
		if !strings.Contains(s, want) {
			t.Errorf("no %q in rows:\n%s", want, s)
		}
	}
	if !strings.Contains(s, "Unauthorized") {
		t.Errorf("the refused test's status is lost:\n%s", s)
	}
}

// Detect APIs writes its requests down as a detection; a request sent
// with no mark (an agent's turn) is written nowhere.
func TestDetectWritesItsRequestsDown(t *testing.T) {
	probeHome(t)
	rows := probeRows(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Method == http.MethodGet {
			_, _ = rw.Write([]byte(`{"data":[{"id":"m1"}]}`))
			return
		}
		_, _ = rw.Write([]byte(`{}`))
	}))
	defer srv.Close()
	if _, err := (Provider{ID: "relay", Key: "k"}).Detect(context.Background(), srv.URL, "m1"); err != nil {
		t.Fatal(err)
	}
	got := rows()
	if len(got) == 0 || int(hits.Load()) != len(got) {
		t.Fatalf("%d requests, %d rows: %+v", hits.Load(), len(got), got)
	}
	for _, r := range got {
		if r.Kind != probelog.KindDetect && r.Kind != probelog.KindList || r.Provider != "relay" {
			t.Errorf("row %+v", r)
		}
		if r.Kind == probelog.KindDetect && !strings.HasPrefix(r.Endpoint, "GET ") && r.Model != "m1" {
			t.Errorf("a detection's row has no model: %+v", r)
		}
	}
	before := len(got)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions", strings.NewReader(`{}`))
	res, err := (Provider{ID: "relay"}).Do(http.DefaultClient, req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if len(rows()) != before {
		t.Fatal("an agent's request was written down as uni-switch's own")
	}
}

// A model's failed test is written down with why it failed: the vendor's
// error for a refusal, and a stream's error after its 200, not a bare
// status or a 200 that reads as answered (ysm119 on Discord: a model "didn't
// answer", and the request log had no error to show).
func TestFailedModelTestIsWrittenWithItsError(t *testing.T) {
	probeHome(t)
	rows := probeRows(t)
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		switch {
		case strings.Contains(string(b), "refused-model"):
			rw.WriteHeader(http.StatusBadRequest)
			_, _ = rw.Write([]byte(`{"error":{"message":"AI Model Not Found: refused-model"}}`))
		case strings.Contains(string(b), "stream-fails"):
			rw.Header().Set("Content-Type", "text/event-stream")
			_, _ = rw.Write([]byte("data: {\"error\":{\"message\":\"the stream broke off\"}}\n\n"))
		default:
			rw.Header().Set("Content-Type", "text/event-stream")
			_, _ = rw.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"))
		}
	}))
	defer srv.Close()
	p := Provider{ID: "relay", Name: "Relay", Chat: srv.URL + "/v1", Key: "sk", KeyProtocol: Chat,
		Models: []string{"refused-model", "stream-fails", "fine"}, Account: &Account{Stream: true}} // a plugin's: streamed
	res := p.TestModels(context.Background(), []string{"refused-model", "stream-fails", "fine"})
	errs := map[string]string{}
	for _, r := range res {
		errs[r.Model] = r.Error
	}
	byModel := map[string]probelog.Row{}
	for _, r := range rows() {
		if r.Kind == probelog.KindTest {
			if _, twice := byModel[r.Model]; twice {
				t.Errorf("%s written twice", r.Model)
			}
			byModel[r.Model] = r
		}
	}
	for model, want := range map[string]string{"refused-model": "AI Model Not Found", "stream-fails": "the stream broke off", "fine": ""} {
		r, ok := byModel[model]
		if !ok {
			t.Errorf("%s: no row in %+v", model, rows())
			continue
		}
		if want == "" && r.Error != "" || !strings.Contains(r.Error, want) || r.Error != errs[model] {
			t.Errorf("%s: row error %q, the test's %q, want %q", model, r.Error, errs[model], want)
		}
	}
}
