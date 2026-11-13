package gateway

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// MOMO on Discord asked for "Zen-Space-Bunny", the name a picker showed,
// and got a 404 that named neither the id meant nor how to name one.
func TestUnknownModelSaysWhatToAsk(t *testing.T) {
	fresh(t)
	serveOn(t, "opencode-zen-free", "k", []string{"big-pickle", "space-bunny-free", "exo-free"}, &keyed{})
	serveOn(t, "opencode-go", "kg", []string{"glm-5.3", "kimi-k3"}, &keyed{})
	if err := provider.SetModelName("opencode-zen-free/space-bunny-free", "Space Bunny Free"); err != nil {
		t.Fatal(err)
	}
	s := New()
	code, body := postAs(t, s, "", `{"model":"Zen-Space-Bunny","messages":[{"role":"user","content":"hi"}]}`)
	if code != http.StatusNotFound {
		t.Fatalf("%d %s", code, body)
	}
	if !strings.Contains(body, "did you mean opencode-zen-free/space-bunny-free") {
		t.Fatalf("the id meant isn't named first: %s", body)
	}
	for _, form := range []string{"provider/model", "routing group's id or name", "bare model id", "Providers order"} {
		if !strings.Contains(body, form) {
			t.Fatalf("doesn't say %q: %s", form, body)
		}
	}
	// the provider's name alone matches nothing
	if _, body := postAs(t, s, "", `{"model":"zen-nothing","messages":[{"role":"user","content":"hi"}]}`); strings.Contains(body, "did you mean") {
		t.Fatalf("a provider's name alone was a match: %s", body)
	}
}

// A model's display name counts before an id that spells the same words.
func TestClosestByNameFirst(t *testing.T) {
	fresh(t)
	serveOn(t, "a", "ka", []string{"m1"}, &keyed{})
	serveOn(t, "b", "kb", []string{"space-bunny-old"}, &keyed{})
	if err := provider.SetModelName("a/m1", "Space Bunny"); err != nil {
		t.Fatal(err)
	}
	got := provider.Closest("space bunny", 3)
	if len(got) != 2 || got[0] != "a/m1" || got[1] != "b/space-bunny-old" {
		t.Fatalf("Closest = %v", got)
	}
}
