package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// #1598 (wuwei0727): five keys, the first in key/keyName and four in
// keys. The vendor's list didn't answer the first key, the other four
// listed every model, so the first was taken to see none of them and was
// left out of every request as unlisted. A key whose list uni-switch doesn't
// have is unknown, not "sees nothing": it serves until a list says it
// doesn't. A key whose list did answer still serves only that list.
func TestKeyWithNoListStillServes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer sk-xx1":
			http.Error(rw, `{"error":{"message":"rate limited"}}`, http.StatusTooManyRequests)
		case "Bearer sk-xx5":
			_, _ = rw.Write([]byte(`{"data":[{"id":"gpt-5.5"}]}`))
		default:
			_, _ = rw.Write([]byte(`{"data":[{"id":"gpt-5.5"},{"id":"claude-opus-4-7"}]}`))
		}
	}))
	defer srv.Close()
	// the reporter's shape: the first key outside keys, the rest in it
	p := Provider{ID: "xx1", Name: "tokenone-xx1", Chat: srv.URL + "/v1", Key: "sk-xx1", KeyName: "xx1",
		Keys: []KeyAccount{{Name: "xx2", Key: "sk-xx2"}, {Name: "xx3", Key: "sk-xx3"}, {Name: "xx4", Key: "sk-xx4"}, {Name: "xx5", Key: "sk-xx5"}}}
	if _, err := p.fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := p.first()
	if first.Key != "sk-xx1" || first.Name != "xx1" {
		t.Fatalf("first key %+v", first)
	}
	for _, m := range []string{"gpt-5.5", "claude-opus-4-7"} {
		if !p.Serves(first, m) {
			t.Errorf("the first key, whose list didn't answer, is left out of %s", m)
		}
	}
	if p.Serves(p.Keys[3], "claude-opus-4-7") {
		t.Error("a key whose list answered without the model serves it")
	}
	if !p.Serves(p.Keys[3], "gpt-5.5") || !p.Serves(p.Keys[0], "claude-opus-4-7") {
		t.Error("a key serves what its own list has")
	}
	// the keys are left as the user saved them
	if p.Key != "sk-xx1" || p.KeyName != "xx1" || len(p.Keys) != 4 {
		t.Errorf("keys changed: %q %q %+v", p.Key, p.KeyName, p.Keys)
	}
}
