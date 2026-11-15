package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"slices"

	"testing"
)

// A group uni-switch found is listed as auto/<model> (ZeroClover on X), and
// served by that id and by its old group/auto-<slug> alike: one group, one
// route, one name in the usage log, whichever an agent sent.
func TestFoundGroupServedByBothIDs(t *testing.T) {
	fresh(t)
	a, b := &keyed{}, &keyed{}
	serveOn(t, "a", "ka", []string{"glm-5.3"}, a)
	serveOn(t, "b", "kb", []string{"glm-5.3"}, b)
	s := New()

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models", nil))
	var list struct{ Data []struct{ ID string } }
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range list.Data {
		ids = append(ids, m.ID)
	}
	if slices.Contains(ids, "auto/glm-5.3") || !slices.Contains(ids, "a/glm-5.3") || !slices.Contains(ids, "b/glm-5.3") || slices.Contains(ids, "group/auto-glm-5-3") {
		t.Fatalf("/v1/models: %v", ids)
	}

	for _, model := range []string{"auto/glm-5.3", "group/auto-glm-5-3"} {
		code, body := postAs(t, s, "", `{"model":"`+model+`","messages":[{"role":"user","content":"hi"}]}`)
		if code != 404 {
			t.Fatalf("retired %s: %d %s", model, code, body)
		}
	}
	code, body := postAs(t, s, "", `{"model":"glm-5.3","messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 {
		t.Fatalf("bare model: %d %s", code, body)
	}
	route := s.trace.routes[len(s.trace.routes)-1]
	if route.Group != nil {
		t.Fatalf("created group: %+v", route)
	}
	if u := lastUsage(t); u.Requested != "glm-5.3" {
		t.Fatalf("recorded %q", u.Requested)
	}
}
