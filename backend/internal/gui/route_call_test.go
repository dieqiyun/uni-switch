package gui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A request on the Routing page opens to its own request and response
// (Fate on Discord): /api/gateway/call?route= finds the recent call by the
// route's id, each its own, and none for a route the ring doesn't have.
func TestRoutingRequestOpensItsBodies(t *testing.T) {
	sandboxHome(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		said := "reply-one"
		if strings.Contains(string(b), "second") {
			said = "reply-two"
		}
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"`+said+`"}}]}`)
	}))
	defer up.Close()
	if err := provider.Save(provider.Provider{ID: "routed", Name: "Routed", Chat: up.URL, Key: "fixture"}); err != nil {
		t.Fatal(err)
	}
	s := gateway.New()
	was := served.Load()
	served.Store(s)
	t.Cleanup(func() { served.Store(was) })
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	ask := func(content string) {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"routed/m","messages":[{"role":"user","content":"`+content+`"}]}`))
		r.Header.Set("User-Agent", "codex")
		s.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("gateway: %d %.200s", w.Code, w.Body)
		}
	}
	ask("first question")
	ask("second question")
	routes := s.Trace(context.Background(), 0, 0).Routes
	if len(routes) != 2 {
		t.Fatalf("%d routes, want 2", len(routes))
	}
	get := func(route int64) (int, map[string]any) {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/gateway/call?route="+strconv.FormatInt(route, 10), nil))
		var got map[string]any
		json.Unmarshal(w.Body.Bytes(), &got)
		return w.Code, got
	}
	for _, rt := range routes {
		if !rt.Done {
			t.Fatalf("route %d not done", rt.ID)
		}
		code, got := get(rt.ID)
		if code != http.StatusOK {
			t.Fatalf("route %d: %d", rt.ID, code)
		}
		req, _ := got["requestBody"].(string)
		res, _ := got["responseBody"].(string)
		second := strings.Contains(req, "second question")
		if !second && !strings.Contains(req, "first question") {
			t.Fatalf("route %d's request body: %.200s", rt.ID, req)
		}
		if want := map[bool]string{false: "reply-one", true: "reply-two"}[second]; !strings.Contains(res, want) {
			t.Fatalf("route %d's response is not its own: %.200s", rt.ID, res)
		}
	}
	for _, id := range []int64{0, routes[0].ID + routes[1].ID} {
		if code, _ := get(id); code != http.StatusNotFound {
			t.Fatalf("route %d the ring hasn't: %d", id, code)
		}
	}
}
