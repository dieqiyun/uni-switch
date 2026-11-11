package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"strings"
	"sync"
	"testing"
)

// A model's test asks the account a request for it goes to (#4992,
// scutken): a newapi pool's accounts list different models, and its
// grok-4.7, served by one account alone, was asked of the first and
// failed 503 "no available channel" while the gateway answered real
// requests on the account that serves it.

// askingRelay is a relay that answers one model only with one key, as a
// site whose groups serve different models does. It keeps what was
// asked, as key:model.
func askingRelay(t *testing.T, good, model string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	asked := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Model string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		mu.Lock()
		asked = append(asked, key+":"+in.Model)
		mu.Unlock()
		if key != good || in.Model != model {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"error":{"message":"no available channel for model %s under key %s"}}`, in.Model, key)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"hi"}}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), asked...)
	}
}

// A key the user lets serve a model (AccountModels by KeyID, #474) is the
// one its test goes with, as routing's, for a model's test and for the
// provider's Test, not the provider's first key.
func TestModelTestUsesTheKeyServingIt(t *testing.T) {
	probeHome(t)
	srv, asked := askingRelay(t, "sk-b", "m-b")
	if err := Save(Provider{ID: "relay", Name: "Relay", Chat: srv.URL + "/v1",
		Key: "sk-a", Keys: []KeyAccount{{Name: "b", Key: "sk-b"}}, Models: []string{"m-b"}}); err != nil {
		t.Fatal(err)
	}
	if err := SetAccountModels("relay", keyID("sk-a"), []string{"m-a"}); err != nil {
		t.Fatal(err)
	}
	p, err := Find("relay")
	if err != nil {
		t.Fatal(err)
	}
	if rs := p.TestModels(context.Background(), []string{"m-b"}); len(rs) != 1 || !rs[0].OK {
		t.Fatalf("m-b's test: %+v (asked %v)", rs, asked())
	}
	if rs := p.Test(context.Background()); len(rs) == 0 || !rs[0].OK || rs[0].Model != "m-b" {
		t.Fatalf("the provider's test: %+v (asked %v)", rs, asked())
	}
	for _, a := range asked() {
		// "sk-a:" alone is the model list the Test reads first, a GET
		if strings.HasPrefix(a, "sk-a:") && a != "sk-a:" {
			t.Errorf("asked the key the user kept off m-b: %v", asked())
		}
	}
}

// A model the user barred every account on from is not asked, as the
// gateway never routes it, rather than asked of the first account and
// failed for a reason the user already set.
func TestModelTestBarredModelIsNotAsked(t *testing.T) {
	probeHome(t)
	srv, asked := askingRelay(t, "sk-a", "m-a")
	p := Provider{ID: "pool", Name: "Pool", Chat: srv.URL + "/v1",
		Account:       &Account{Agent: "pool", User: "first"},
		Models:        []string{"m-a", "m-b"},
		AccountModels: map[string][]string{"first": {"m-a"}}}
	rs := p.TestModels(context.Background(), []string{"m-b"})
	if len(rs) != 1 || rs[0].OK || !strings.Contains(rs[0].Error, "set not to serve") {
		t.Fatalf("m-b's test: %+v", rs)
	}
	if got := asked(); len(got) != 0 {
		t.Errorf("asked anyway: %v", got)
	}
}

// Detect APIs asks each model with a key a request for it goes to, as a
// model's test does: a relay's key kept for other models is not asked.
func TestDetectModelsUsesTheKeyServingIt(t *testing.T) {
	probeHome(t)
	srv, asked := askingRelay(t, "sk-b", "m-b")
	if err := Save(Provider{ID: "relay", Name: "Relay", Chat: srv.URL + "/v1",
		Key: "sk-a", Keys: []KeyAccount{{Name: "b", Key: "sk-b"}}, Models: []string{"m-b"}}); err != nil {
		t.Fatal(err)
	}
	if err := SetAccountModels("relay", keyID("sk-a"), []string{"m-a"}); err != nil {
		t.Fatal(err)
	}
	p, err := Find("relay")
	if err != nil {
		t.Fatal(err)
	}
	ms, _, err := p.DetectModels(context.Background(), srv.URL+"/v1", []string{"m-b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || len(ms[0].Results) == 0 || !ms[0].Results[0].OK {
		t.Fatalf("m-b's detection: %+v (asked %v)", ms, asked())
	}
	for _, a := range asked() {
		if strings.HasPrefix(a, "sk-a:") {
			t.Errorf("asked the key the user kept off m-b: %v", asked())
		}
	}
}
