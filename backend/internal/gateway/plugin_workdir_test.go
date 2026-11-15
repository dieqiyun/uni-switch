package gateway

import (
	"net/http"
	"net/url"

	"testing"
)

// sawWorkPlace checks what one request told the plugin (X-Saw-Work-*, as
// the fake plugin echoes it) and that none of it reached the vendor.
func sawWorkPlace(t *testing.T, name string, h http.Header, wantDir, wantGit string) {
	t.Helper()
	for _, k := range []string{"X-Uni-Switch-Work-Dir", "X-Uni-Switch-Work-Git"} {
		if v := h.Values(k); len(v) > 0 {
			t.Errorf("%s: the vendor got %s: %q", name, k, v)
		}
	}
	dir := h.Get("X-Saw-Work-Dir")
	if dir != "" {
		d, err := url.PathUnescape(dir)
		if err != nil {
			t.Fatalf("%s: %q isn't percent-encoded: %v", name, dir, err)
		}
		dir = d
	}
	if dir != wantDir || h.Get("X-Saw-Work-Git") != wantGit {
		t.Errorf("%s: the plugin was told %q, git %q; want %q, %q", name, dir, h.Get("X-Saw-Work-Git"), wantDir, wantGit)
	}
}
