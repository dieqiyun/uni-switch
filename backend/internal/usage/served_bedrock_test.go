package usage

import (
	"os"
	"strings"
	"testing"
)

// Every Bedrock id models.dev lists is a geography (or none), a maker and
// the model's own name. A reply naming the model by its own name, or by
// maker.name, is that model: no maker of the list may be missing from
// vendorDot (#2199: xai, writer, moonshot and nvidia were).
func TestBedrockListedMakersAnsweredByOwnNameAreNoSwap(t *testing.T) {
	b, err := os.ReadFile("testdata/bedrock-ids-models.dev-2026-10-28.txt")
	if err != nil {
		t.Fatal(err)
	}
	geos := map[string]bool{"global": true, "us": true, "us-gov": true, "eu": true, "apac": true, "jp": true, "au": true, "ca": true, "in": true}
	n := 0
	for _, id := range strings.Fields(string(b)) {
		rest := id
		if g, r, ok := strings.Cut(rest, "."); ok && geos[g] {
			rest = r
		}
		maker, own, ok := strings.Cut(rest, ".")
		if !ok {
			t.Errorf("%s has no maker", id)
			continue
		}
		n++
		for _, served := range []string{own, maker + "." + own} {
			if Swapped(id, served) {
				t.Errorf("Swapped(%q, %q) = true: maker %q isn't stripped", id, served, maker)
			}
		}
	}
	if n < 100 {
		t.Fatalf("read %d ids, want the whole list", n)
	}
}
