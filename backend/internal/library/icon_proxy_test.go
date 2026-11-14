package library

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// A market icon is asked for through io.uni-switch.desktop's icon proxy, never from
// the host its author picked, which would see the user's IP.
func TestIconGoesThroughTheProxy(t *testing.T) {
	h := sandbox(t)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	var asked []string
	proxy := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Query().Get("url"))
		rw.Header().Set("Content-Type", "image/png")
		rw.Write(png)
	}))
	defer proxy.Close()
	t.Setenv("UNI_SWITCH_ICON_PROXY", proxy.URL+"/api/icon")

	// a server the registry showed, its icon on its author's host; .invalid
	// never resolves, so reaching it straight fails the test
	const avatar = "https://author.invalid/logo.png?v=2"
	seenServers.Lock()
	seenServers.m["io.example/proxy-test"] = MarketServer{Icon: avatar}
	seenServers.Unlock()
	t.Cleanup(func() {
		seenServers.Lock()
		delete(seenServers.m, "io.example/proxy-test")
		seenServers.Unlock()
	})
	b, ct, err := Icon(avatar)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != string(png) || ct != "image/png" {
		t.Fatalf("Icon = %q %q", b, ct)
	}
	if len(asked) != 1 || asked[0] != avatar {
		t.Fatalf("the proxy was asked for %q, want [%s]", asked, avatar)
	}
}
