package supplier

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/contract"
)

type requestLog struct {
	mu    sync.Mutex
	paths []string
}

func (l *requestLog) add(path string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.paths = append(l.paths, path)
}
func (l *requestLog) values() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.paths...)
}

func fixture(t *testing.T, s string) object {
	t.Helper()
	d := json.NewDecoder(strings.NewReader(s))
	d.UseNumber()
	var m object
	if err := d.Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m
}
func connection(base string) contract.Connection {
	return contract.Connection{BaseURL: base, APIKey: "synthetic-provider-key"}
}
func TestDiscoveryAuthFallbackPathsAndFullCatalog(t *testing.T) {
	var log requestLog
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.add(r.URL.RequestURI() + ":" + r.Header.Get("Authorization") + ":" + r.Header.Get("x-api-key"))
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("x-api-key") == "" {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/prefix/models" {
			w.WriteHeader(404)
			return
		}
		if r.URL.Query().Get("after_id") == "" {
			fmt.Fprint(w, `{"object":"list","has_more":true,"last_id":"page+1","data":[{"id":"b","supported_endpoint_types":["anthropic","openai-response"]}]}`)
		} else {
			if r.URL.Query().Get("after_id") != "page+1" {
				t.Error("cursor not encoded")
			}
			fmt.Fprint(w, `{"object":"list","has_more":false,"data":[{"id":"a","supported_endpoint_types":["anthropic"]}]}`)
		}
	}))
	defer server.Close()
	r, err := New().Discover(context.Background(), connection(server.URL+"/prefix"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Protocol != "openai" || r.AuthMode != "x-api-key" || r.BaseURL != server.URL+"/prefix/v1" || len(r.Models) != 2 || r.Models[0].ID != "b" {
		t.Fatalf("unexpected discovery: %+v", r)
	}
	paths := log.values()
	if len(paths) != 4 {
		t.Fatalf("unexpected retries: %v", paths)
	}
}
func TestDiscoveryMessagesKeepsNativePrefixAndForcedProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"object":"list","data":[{"id":"claude-test","supported_endpoint_types":["anthropic","openai"]}]}`)
	}))
	defer server.Close()
	c := connection(server.URL + "/gateway")
	r, err := New().Discover(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if r.Protocol != "anthropic" || r.BaseURL != c.BaseURL {
		t.Fatalf("native prefix changed: %+v", r)
	}
	c.Protocol = ptr("openai")
	c.AuthMode = ptr("bearer")
	r, err = New().Discover(context.Background(), c)
	if err != nil || r.Protocol != "openai" {
		t.Fatalf("forced protocol lost: %+v %v", r, err)
	}
}
func TestDiscoveryRejectsIncompletePagesAndDoesNotRetryRateLimits(t *testing.T) {
	for _, test := range []struct {
		name, body, code string
		status           int
		calls            int
	}{
		{"rate", `secret-body`, "supplier_http", 429, 1},
		{"missing_cursor", `{"has_more":true,"data":["a"]}`, "model_response", 200, 1},
		{"repeated_cursor", `{"has_more":true,"last_id":"a","data":["a"]}`, "model_response", 200, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			r, err := New().Discover(context.Background(), connection(server.URL))
			if r != nil || err == nil || err.Code != test.code || int(calls.Load()) != test.calls || strings.Contains(err.Error(), "secret-body") {
				t.Fatalf("result=%+v err=%v calls=%d", r, err, calls.Load())
			}
		})
	}
}
func TestClientRejectsRedirectsOversizeAndTrailingJSON(t *testing.T) {
	var leaked atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer target.Close()
	for _, kind := range []string{"redirect", "large", "json"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case "redirect":
					http.Redirect(w, r, target.URL, 302)
				case "large":
					fmt.Fprint(w, strings.Repeat(" ", 2*1024*1024+1))
				case "json":
					fmt.Fprint(w, `{"data":["a"]} {"data":["b"]}`)
				}
			}))
			defer server.Close()
			u, _ := validateURL(server.URL)
			key := "synthetic-key"
			_, err := New().get(context.Background(), u, &key, nil, false, false, time.Second)
			if err == nil {
				t.Fatal("expected rejection")
			}
			if strings.Contains(err.Error(), key) {
				t.Fatal("credential exposed")
			}
		})
	}
	if leaked.Load() != 0 {
		t.Fatal("redirect followed")
	}
}
func TestDiscoveryCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := New().Discover(ctx, connection(server.URL))
	if err == nil || time.Since(start) > time.Second {
		t.Fatal("cancellation not propagated")
	}
}
func TestModelsPreserveReferenceMetadataAndRejectInvalidLists(t *testing.T) {
	value := fixture(t, `{"data":[{"id":"unknown-model","modalities":{"input":[]},"supported_endpoints":["/messages","/chat/completions"],"supported_reasoning_levels":["light","high"]}]}`)
	models, err := parseModels(value)
	if err != nil {
		t.Fatal(err)
	}
	m := models[0]
	if m.ContextWindow != nil || m.Capabilities["imageInput"] != false || m.Profile["toolCalls"] != nil || !reflect.DeepEqual(m.ReasoningEfforts, []string{"light", "high"}) {
		t.Fatalf("metadata lost: %+v", m)
	}
	if obj(m.Profile["endpoints"])["responses"] != false {
		t.Fatal("explicit endpoints not preserved")
	}
	bad := profile(fixture(t, `{"context_window":9999,"supports_image_input":true,"reasoning_efforts":["high"]}`))
	if len(bad) != 0 {
		t.Fatal("non-reference field guesses survived", bad)
	}
	for _, raw := range []string{`{"data":[]}`, `{"data":["bad\nmodel"]}`, `{"models":[{}]}`} {
		if _, e := parseModels(fixture(t, raw)); e == nil {
			t.Fatal("invalid model list accepted")
		}
	}
}
