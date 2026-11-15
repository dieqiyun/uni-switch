package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/budget"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// keyPageGateway is a shared gateway with a plan of two models and two
// keys, each with calls of its own: "Alice" held to plan/m1 and called
// once, "Bob" held to a daily token limit and called twice.
func keyPageGateway(t *testing.T) (http.Handler, []access.Key, []string) {
	t.Helper()
	fresh(t)
	t.Setenv("UNI_SWITCH_ADDR", "")
	budget.Forget()
	pinLimitClock(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":30,"completion_tokens":5}}`)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "plan", Name: "Plan", Key: "upstream-secret", Chat: up.URL + "/v1", Models: []string{"m1", "m2"}}); err != nil {
		t.Fatal(err)
	}
	keys, secrets := newCaller(t, "Alice", "Bob")
	if _, err := access.Update("models-key", access.Change{Key: keys[0].ID, Models: []string{"plan/m1"}}); err != nil {
		t.Fatal(err)
	}
	setLimit(t, keys[1].ID, &access.Limit{Period: "day", Tokens: 1000})
	h := localGuard(New().Handler())
	for _, secret := range []string{secrets[0], secrets[1], secrets[1]} {
		r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(chatReq))
		r.RemoteAddr = "127.0.0.1:5000"
		r.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal("call", w.Code, w.Body.String())
		}
	}
	return h, keys, secrets
}

type keyPageReq struct {
	method, path, from, host string
	header                   map[string]string
}

func (q keyPageReq) do(h http.Handler) *httptest.ResponseRecorder {
	r := httptest.NewRequest(q.method, q.path, nil)
	r.RemoteAddr = q.from
	r.Host = "localhost:3425"
	if q.host != "" {
		r.Host = q.host
	}
	for k, v := range q.header {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

type keyPageInfo struct {
	Name      string                   `json:"name"`
	ReadOnly  bool                     `json:"readOnly"`
	Limits    []budget.Status          `json:"limits"`
	Usage     map[string]keyPageTotals `json:"usage"`
	TopModels []keyPageModel           `json:"topModels"`
	Models    []struct{ ID, Label string }
}

// zytszone on X: a department's people each have a gateway key and can't
// be given the admin's app. The key page's data tells a key what is its
// own: its name, limits, calls and models, from another machine, and
// nothing of the other key, the provider's key or either secret.
func TestKeyPageTellsAKeyItsOwn(t *testing.T) {
	h, keys, secrets := keyPageGateway(t)
	for i, secret := range secrets {
		w := keyPageReq{method: "GET", path: keyPageData, from: "127.0.0.1:5000", header: map[string]string{"Authorization": "Bearer " + secret}}.do(h)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
			t.Error("Cache-Control", cc)
		}
		body := w.Body.String()
		other := 1 - i
		for _, leak := range []string{keys[other].Name, keys[other].ID, secrets[other], secret, "upstream-secret", Token} {
			if strings.Contains(body, leak) {
				t.Errorf("key %s is told %q: %s", keys[i].Name, leak, body)
			}
		}
		var got keyPageInfo
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Name != keys[i].Name || got.ReadOnly {
			t.Errorf("name %q readOnly %v", got.Name, got.ReadOnly)
		}
		calls := []int{1, 2}[i]
		for _, p := range []string{"today", "7d", "30d"} {
			if u := got.Usage[p]; u.Calls != calls || u.Input != 30*calls || u.Output != 5*calls {
				t.Errorf("%s: %s usage %+v", keys[i].Name, p, u)
			}
		}
		if len(got.TopModels) != 1 || got.TopModels[0].ID != "m1" || got.TopModels[0].Calls != calls {
			t.Errorf("%s: top models %+v", keys[i].Name, got.TopModels)
		}
		var ids []string
		for _, m := range got.Models {
			ids = append(ids, m.ID)
		}
		if want := []string{"plan/m1", "plan/m1,plan/m2"}[i]; strings.Join(ids, ",") != want {
			t.Errorf("%s: models %v, want %s", keys[i].Name, ids, want)
		}
		if i == 0 && len(got.Limits) != 0 {
			t.Errorf("Alice has no limit: %+v", got.Limits)
		}
		if i == 1 && (len(got.Limits) != 1 || got.Limits[0].TokenLimit != 1000 || got.Limits[0].Tokens != 70) {
			t.Errorf("Bob's limit: %+v", got.Limits)
		}
	}
}

// No key, a wrong one, one turned off or removed, uni-switch's own token and
// a key in the address are all told nothing, from another machine and
// from this one; and a page from elsewhere (Origin null, a *.localhost
// name, another site, a rebound name) is refused as on every route.
func TestKeyPageRefusesWithoutItsKey(t *testing.T) {
	h, keys, secrets := keyPageGateway(t)
	if _, err := access.Update("off-key", access.Change{Key: keys[1].ID}); err != nil {
		t.Fatal(err)
	}
	remote, here := "127.0.0.1:5000", "127.0.0.1:5000"
	bearer := func(k string) map[string]string { return map[string]string{"Authorization": "Bearer " + k} }
	for _, c := range []struct {
		name string
		q    keyPageReq
		want int
	}{
		{"no key", keyPageReq{from: remote}, 401},
		{"wrong key", keyPageReq{from: remote, header: bearer("sk-uni-switch-not-a-key")}, 401},
		{"turned off", keyPageReq{from: remote, header: bearer(secrets[1])}, 401},
		{"turned off, x-api-key", keyPageReq{from: remote, header: map[string]string{"x-api-key": secrets[1]}}, 401},
		{"key in the address", keyPageReq{from: remote, path: keyPageData + "?key=" + secrets[0]}, 400},
		{"here, no key", keyPageReq{from: here}, 401},
		{"here, wrong key", keyPageReq{from: here, header: bearer("sk-uni-switch-not-a-key")}, 401},
		{"here, turned off", keyPageReq{from: here, header: bearer(secrets[1])}, 401},
		{"here, uni-switch's token", keyPageReq{from: here, header: bearer(Token)}, 401},
		{"here, key in the address", keyPageReq{from: here, path: keyPageData + "?key=" + secrets[0]}, 400},
		{"Origin null", keyPageReq{from: here, header: map[string]string{"Origin": "null"}}, 403},
		{"a localhost subdomain", keyPageReq{from: here, header: map[string]string{"Origin": "http://evil.localhost"}}, 403},
		{"another site, with the key", keyPageReq{from: here, header: map[string]string{"Origin": "https://evil.example", "Authorization": "Bearer " + secrets[0]}}, 403},
		{"another site, remote, with the key", keyPageReq{from: remote, header: map[string]string{"Origin": "https://evil.example", "Authorization": "Bearer " + secrets[0]}}, 403},
		{"rebound name", keyPageReq{from: here, host: "evil.example:3425", header: map[string]string{"Sec-Fetch-Site": "same-origin"}}, 403},
		{"local, Origin null, no key", keyPageReq{from: remote, header: map[string]string{"Origin": "null"}}, 403},
	} {
		c.q.method = "GET"
		if c.q.path == "" {
			c.q.path = keyPageData
		}
		w := c.q.do(h)
		if w.Code != c.want {
			t.Errorf("%s: %d, want %d: %s", c.name, w.Code, c.want, w.Body.String())
		}
		for _, k := range keys {
			if strings.Contains(w.Body.String(), k.Name) || strings.Contains(w.Body.String(), k.ID) {
				t.Errorf("%s: told %s: %s", c.name, k.Name, w.Body.String())
			}
		}
	}
	// and the key that works still does
	if w := (keyPageReq{method: "GET", path: keyPageData, from: remote, header: bearer(secrets[0])}).do(h); w.Code != 200 {
		t.Fatal("Alice", w.Code)
	}
}

// The local page opens before a key is typed. It cannot be framed, loads
// nothing from elsewhere, and never sends the key in an address. Remote
// callers and rebound names are refused.
func TestKeyPageOpensWithoutAKey(t *testing.T) {
	h, _, _ := keyPageGateway(t)
	w := keyPageReq{method: "GET", path: KeyPagePath, from: "127.0.0.1:5000", header: map[string]string{"User-Agent": "Mozilla/5.0"}}.do(h)
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatal(w.Code, w.Header())
	}
	csp := w.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "connect-src 'self'", "frame-ancestors 'none'", "form-action 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q lacks %q", csp, want)
		}
	}
	if w.Header().Get("X-Frame-Options") != "DENY" || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Error(w.Header())
	}
	page := w.Body.String()
	if strings.Contains(page, "?key=") || strings.Contains(page, "&key=") || !strings.Contains(page, `Authorization: "Bearer " + k`) {
		t.Error("the page sends its key some other way than the Authorization header")
	}
	// Local requests retain normal routing, including URL path decoding.
	for _, c := range []struct {
		request keyPageReq
		status  int
	}{
		{keyPageReq{method: "POST", path: KeyPagePath}, 404},
		{keyPageReq{method: "GET", path: KeyPagePath + "/x"}, 404},
		{keyPageReq{method: "GET", path: "/%6De"}, 200},
	} {
		q := c.request
		q.from = "127.0.0.1:5000"
		if w := q.do(h); w.Code != c.status {
			t.Errorf("%s %s: got %d, want %d", q.method, q.path, w.Code, c.status)
		}
	}
	if w := (keyPageReq{method: "GET", path: KeyPagePath, from: "127.0.0.1:5000", host: "evil.example:3425", header: map[string]string{"User-Agent": "Mozilla/5.0"}}).do(h); w.Code != 403 {
		t.Error("rebound", w.Code)
	}
	s := settings.Load()

	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	if w := (keyPageReq{method: "GET", path: KeyPagePath, from: "192.168.1.9:5000"}).do(h); w.Code != 403 {
		t.Error("not shared", w.Code)
	}
	if w := (keyPageReq{method: "GET", path: KeyPagePath, from: "127.0.0.1:5000"}).do(h); w.Code != 200 {
		t.Error("here", w.Code)
	}
}

// A read-only key opens the page and reads its own usage, and is told
// no models, as it may call none.
func TestKeyPageReadOnlyKey(t *testing.T) {
	h, keys, secrets := keyPageGateway(t)
	if _, err := access.Update("access-key", access.Change{Key: keys[0].ID, ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	bearer := map[string]string{"Authorization": "Bearer " + secrets[0]}
	if w := (keyPageReq{method: "GET", path: KeyPagePath, from: "127.0.0.1:5000", header: bearer}).do(h); w.Code != 200 {
		t.Fatal("page", w.Code, w.Body.String())
	}
	w := keyPageReq{method: "GET", path: keyPageData, from: "127.0.0.1:5000", header: bearer}.do(h)
	var got keyPageInfo
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	if !got.ReadOnly || len(got.Models) != 0 || got.Usage["30d"].Calls != 1 {
		t.Fatalf("%+v", got)
	}
}
