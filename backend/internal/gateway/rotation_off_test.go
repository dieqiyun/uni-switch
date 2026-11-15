package gateway

import (
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/routes"
)

// The router strategy point with plugins/rotation on and off: a provider
// in turn sends each new conversation one key further along; with the
// plugin off, no strategy serves "rotate", so its keys are tried in their
// listed order — every conversation goes to the first — and no request is
// marked as one's turn. Smart is the core's and serves either way.
func TestRotationOnAndOff(t *testing.T) {
	firsts := func(routing string) ([]string, bool) {
		t.Helper()
		fresh(t)
		v := &keyed{}
		serveOn(t, "ra", "k1", []string{"m"}, v, "k2", "k3")
		if err := provider.SetRouting("ra", routing); err != nil {
			t.Fatal(err)
		}
		routes.Forget("ra")
		s := New()
		turn := false
		for _, sess := range []string{"s1", "s2", "s3"} {
			if code, body := postAs(t, s, sess, `{"model":"ra/m","messages":[{"role":"user","content":"hi `+sess+`"}]}`); code != 200 {
				t.Fatalf("%s: %d %s", sess, code, body)
			}
			for _, w := range s.trace.routes[len(s.trace.routes)-1].Order {
				turn = turn || w.Turn
			}
		}
		return v.tried, turn
	}
	if got, turn := firsts(provider.Rotate); !slices.Equal(got, []string{"k1", "k2", "k3"}) || !turn {
		t.Fatalf("on, in turn: %v, turn marked %v", got, turn)
	}

	withoutRotation(t)
	if got, turn := firsts(provider.Rotate); !slices.Equal(got, []string{"k1", "k1", "k1"}) || turn {
		t.Fatalf("off, in turn: %v (want the listed order), turn marked %v", got, turn)
	}
	if got, _ := firsts(""); len(got) != 3 {
		t.Fatalf("off, Smart: %v", got)
	}
}
