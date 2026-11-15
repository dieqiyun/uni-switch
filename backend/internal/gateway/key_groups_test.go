package gateway

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func TestAPIKeyGroupRequestsNeverCrossGroups(t *testing.T) {
	fresh(t)
	v := &keyedUpstream{refuse: map[string]bool{"first": true}}
	serveOn(t, "relay", "first", []string{"m"}, v, "second")
	p, err := provider.Find("relay")
	if err != nil {
		t.Fatal(err)
	}
	p.KeyGroups = true
	p.KeyName = "Basic"
	p.Keys[0].Name = "Premium"
	p.Fallback = []string{"relay/m"}
	if err := provider.Save(*p); err != nil {
		t.Fatal(err)
	}
	request := func(key string) (int, string) {
		return postAs(t, New(), "qa-groups", fmt.Sprintf(`{"model":"relay/~key/%s/m","messages":[{"role":"user","content":"hi"}]}`, provider.KeyID(key)))
	}
	code, body := request("second")
	if code != 200 || !strings.Contains(body, "from second") || !slices.Equal(v.asked(), []string{"second"}) {
		t.Fatalf("Premium request used another group's key: %d %s %v", code, body, v.asked())
	}
	v.reset()
	code, body = request("first")
	if code < 400 || len(v.asked()) == 0 || slices.ContainsFunc(v.asked(), func(k string) bool { return k != "first" }) {
		t.Fatalf("rate-limited group crossed to another key: %d %s %v", code, body, v.asked())
	}
}
