package gateway

import (
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestDirectProviderKeyFailoverWithoutGroups(t *testing.T) {
	fresh(t)
	v := &keyed{fail: map[string]int{"first": http.StatusTooManyRequests}}
	serveOn(t, "relay", "first", []string{"m"}, v, "second")
	code, body := postAs(t, New(), "qa-direct", `{"model":"relay/m","messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 || !strings.Contains(body, "from second") {
		t.Fatalf("reply: %d %s", code, body)
	}
	if !slices.Equal(v.tried, []string{"first", "second"}) {
		t.Fatalf("keys: %v", v.tried)
	}
}
func TestRetiredGroupCannotReachProvider(t *testing.T) {
	fresh(t)
	v := &keyed{}
	serveOn(t, "relay", "synthetic", []string{"m"}, v)
	code, _ := postAs(t, New(), "", `{"model":"group/legacy","messages":[{"role":"user","content":"hi"}]}`)
	if code < 400 || len(v.tried) > 0 {
		t.Fatalf("legacy request: status %d tries %v", code, v.tried)
	}
	if _, _, ok := provider.Resolve("relay/m"); !ok {
		t.Fatal("direct provider unavailable")
	}
}
