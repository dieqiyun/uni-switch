package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A provider's extra body fields (#1604) are saved from the editor, shown
// back to it, kept by a save that doesn't send them, cleared by {}, and a
// save naming a field uni-switch sets itself is refused with why.
func TestProviderExtraBodySaved(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	post := func(action, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/"+action, strings.NewReader(body)))
		return w
	}
	stored := func() string {
		t.Helper()
		p, err := provider.Find("relay")
		if err != nil {
			t.Fatal(err)
		}
		if p.ExtraBody == nil {
			return ""
		}
		b, _ := json.Marshal(p.ExtraBody)
		return string(b)
	}
	const u = "http://127.0.0.1:1/v1"
	if w := post("save", `{"id":"relay","name":"Relay","key":"sk-a","responses":"`+u+`","icon":"generic","new":true,"extraBody":{"prompt_cache_key":"${sessionId}","!service_tier":"flex"}}`); w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	if got := stored(); got != `{"!service_tier":"flex","prompt_cache_key":"${sessionId}"}` {
		t.Fatalf("stored %s", got)
	}

	// the editor is given them
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/providers", nil))
	if !strings.Contains(w.Body.String(), `"extraBody":{"!service_tier":"flex","prompt_cache_key":"${sessionId}"}`) {
		t.Errorf("not shown: %s", w.Body)
	}

	// a save without them keeps them
	if w := post("save", `{"id":"relay","from":"relay","name":"Relay","responses":"`+u+`","icon":"generic"}`); w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	if got := stored(); !strings.Contains(got, "prompt_cache_key") {
		t.Fatalf("a save without them lost them: %s", got)
	}

	// one naming the model, or the conversation, is refused and changes nothing
	for _, bad := range []string{`{"model":"x"}`, `{"!messages":[]}`, `{"input":"x"}`} {
		w := post("save", `{"id":"relay","from":"relay","name":"Relay","responses":"`+u+`","icon":"generic","extraBody":`+bad+`}`)
		if w.Code == 200 || !strings.Contains(w.Body.String(), "can't set") {
			t.Errorf("%s: %d %s", bad, w.Code, w.Body)
		}
	}
	if got := stored(); !strings.Contains(got, "prompt_cache_key") {
		t.Fatalf("a refused save changed them: %s", got)
	}

	// {} clears them
	if w := post("save", `{"id":"relay","from":"relay","name":"Relay","responses":"`+u+`","icon":"generic","extraBody":{}}`); w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	if got := stored(); got != "" {
		t.Fatalf("{} left %s", got)
	}
}
