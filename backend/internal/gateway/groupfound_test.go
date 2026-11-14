package gateway

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// An agent set to a group uni-switch found keeps it while the user switches
// one of its two providers off for a while: the request is the group's,
// answered by the provider still on, not refused as a model uni-switch
// doesn't know, and the other is asked again once it is back on.
func TestFoundGroupWithAProviderOff(t *testing.T) {
	fresh(t)
	a, b := &keyed{}, &keyed{}
	serveOn(t, "a", "ka", []string{"m"}, a)
	serveOn(t, "b", "kb", []string{"m"}, b)
	s := New()
	if err := provider.SetOff("a", true); err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"group/auto-m", "m"} {
		a.tried, b.tried = nil, nil
		code, body := postAs(t, s, "", `{"model":"`+model+`","messages":[{"role":"user","content":"hi"}]}`)
		if code != 200 || !strings.Contains(body, "from kb") || len(a.tried) != 0 {
			t.Fatalf("%s with a off: %d %s, a tried %v", model, code, body, a.tried)
		}
		if r := s.trace.routes[len(s.trace.routes)-1]; r.Group == nil {
			t.Fatalf("%s with a off, not routed as the group: %+v", model, r)
		}
	}
	if err := provider.SetOff("a", false); err != nil {
		t.Fatal(err)
	}
	// another conversation, so b's answer to the one above doesn't hold it
	if code, body := postAs(t, s, "", `{"model":"group/auto-m","messages":[{"role":"user","content":"a new one"}]}`); code != 200 || !strings.Contains(body, "from ka") {
		t.Fatalf("a on again: %d %s", code, body)
	}
}
