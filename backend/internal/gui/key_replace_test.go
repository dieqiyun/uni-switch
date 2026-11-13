package gui

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// keys/replace (#1869): a key's new value takes its place, a gateway key
// held to it is held to the new one, and its rests are lifted: by its old
// fingerprint, and the provider's own while it was the one key on.
func TestKeysReplaceKeepsTheKeyAndLiftsItsRest(t *testing.T) {
	sandboxHome(t)
	var lifted []string
	old := liftRests
	liftRests = func(keys []string) { lifted = append(lifted, keys...) }
	t.Cleanup(func() { liftRests = old })
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Chat: "http://127.0.0.1:1/v1", Key: "sk-a", KeyName: "Personal",
		Keys: []provider.KeyAccount{{Name: "Team", Key: "sk-b"}, {Name: "Off", Key: "sk-c", Off: true}}}); err != nil {
		t.Fatal(err)
	}
	a, b := provider.KeyID("sk-a"), provider.KeyID("sk-b")
	if _, err := access.Update("add-key", access.Change{Name: "ci"}); err != nil {
		t.Fatal(err)
	}
	ks, _ := access.List()
	if _, err := access.Update("accounts-key", access.Change{Key: ks[0].ID, Accounts: []string{"relay/" + b, "relay/" + a}}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	post := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/keys/replace", strings.NewReader(body)))
		return w
	}
	if w := post(`{"id":"relay","ref":"` + b + `","key":"sk-b2"}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	p, _ := provider.Find("relay")
	if p.Key != "sk-a" || len(p.Keys) != 2 || p.Keys[0] != (provider.KeyAccount{Name: "Team", Key: "sk-b2"}) {
		t.Fatalf("keys %q %+v", p.Key, p.Keys)
	}
	b2 := provider.KeyID("sk-b2")
	if ks, _ := access.List(); !slices.Equal(ks[0].Accounts, []string{"relay/" + b2, "relay/" + a}) {
		t.Fatalf("gateway key's accounts %v", ks[0].Accounts)
	}
	if !slices.Equal(lifted, []string{"relay#" + b}) {
		t.Fatalf("lifted %v", lifted)
	}

	// the one key on: the provider rests by its own id, lifted too
	if err := provider.SetKeyOn("relay", b2, false); err != nil {
		t.Fatal(err)
	}
	lifted = nil
	if w := post(`{"id":"relay","ref":"` + a + `","key":"sk-a2"}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if !slices.Equal(lifted, []string{"relay#" + a, "relay"}) {
		t.Fatalf("lifted %v", lifted)
	}

	// another key's value is refused, and nothing is lifted
	lifted = nil
	if w := post(`{"id":"relay","ref":"` + provider.KeyID("sk-c") + `","key":"sk-a2"}`); w.Code == 200 || lifted != nil {
		t.Fatalf("%d %s, lifted %v", w.Code, w.Body, lifted)
	}
}
