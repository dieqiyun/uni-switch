package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
)

func TestCallerKeyManagement(t *testing.T) {
	sandboxHome(t)
	mux := http.NewServeMux()
	callerKeyRoutes(mux)
	type state struct {
		Keys   []access.Key `json:"keys"`
		Secret string       `json:"secret"`
	}
	request := func(method, path, body string, out any) string {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		if out != nil {
			if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
				t.Fatal(err)
			}
		}
		return w.Body.String()
	}
	var s state
	request("POST", "/api/caller-keys/add-key", `{"name":"Laptop"}`, &s)
	secret, laptop := s.Secret, s.Keys[0].ID
	request("POST", "/api/caller-keys/add-key", `{"name":"Server"}`, &s)
	list := request("GET", "/api/caller-keys", "", &s)
	if strings.Contains(list, secret) || strings.Contains(list, `"secret"`) {
		t.Fatal("list leaked credentials")
	}
	request("POST", "/api/caller-keys/rename-key", `{"key":"`+laptop+`","name":"Main"}`, &s)
	request("POST", "/api/caller-keys/rotate-key", `{"key":"`+laptop+`"}`, &s)
	if s.Secret == "" || s.Secret == secret || s.Keys[0].ID != laptop || s.Keys[0].Name != "Main" {
		t.Fatal("rotation lost identity")
	}
	if _, ok := access.Authenticate(secret); ok {
		t.Fatal("rotated key still authenticates")
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/caller-keys/off-key", strings.NewReader(`{"key":"missing"}`)))
	if w.Code != 400 {
		t.Fatal("unknown key accepted", w.Code)
	}
}
