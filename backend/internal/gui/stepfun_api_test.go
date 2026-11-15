package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A StepFun provider's Step Plan sign-in is its own (#2384): the editor is
// told each provider's, and signing one out, by its id, leaves the other
// on its site signed in. An id that isn't a StepFun provider's, a site's
// name among them, is no StepFun sign-in to post to.
func TestStepFunSignInPerProvider(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, p := range []provider.Provider{
		{ID: "stepfun", Name: "StepFun", Key: "k1", Chat: "https://api.stepfun.ai/step_plan/v1"},
		{ID: "stepfun-work", Name: "StepFun Work", Key: "k2", Chat: "https://api.stepfun.ai/step_plan/v1"},
		{ID: "relay", Name: "Relay", Key: "k3", Chat: "http://127.0.0.1:1/v1"},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	session := `{"access":"a.b.c","refresh":"a.b.c","webId":"web-1","account":"4161"}`
	auth := filepath.Join(filepath.Dir(provider.Path()), "stepfun-auth.json")
	if err := os.WriteFile(auth, []byte(`{"stepfun":`+session+`,"stepfun-work":`+session+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	signedIn := func() map[string]bool {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/providers", nil))
		var got struct {
			Providers []struct {
				ID       string
				StepPlan *struct{ SignedIn bool }
			}
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, p := range got.Providers {
			if p.StepPlan != nil {
				out[p.ID] = p.StepPlan.SignedIn
			}
		}
		return out
	}
	if got := signedIn(); !got["stepfun"] || !got["stepfun-work"] || len(got) != 2 {
		t.Fatalf("before: %v", got)
	}
	for _, id := range []string{"relay", "ai", "nobody"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/stepfun/"+id+"/signout", strings.NewReader("{}")))
		if w.Code != http.StatusNotFound {
			t.Errorf("signout for %s: %d, want 404", id, w.Code)
		}
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/stepfun/stepfun/signout", strings.NewReader("{}")))
	if w.Code != 200 {
		t.Fatalf("signout: %d %s", w.Code, w.Body)
	}
	if got := signedIn(); got["stepfun"] || !got["stepfun-work"] {
		t.Fatalf("after signing stepfun out: %v", got)
	}
}
