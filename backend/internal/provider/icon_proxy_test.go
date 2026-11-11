package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// A plugin's or an import link's icon is asked for through io.uni-switch.desktop's
// icon proxy, so the host its author picked never sees the user's IP.
func TestFetchIconGoesThroughTheProxy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	var asked []string
	proxy := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path+" "+r.URL.Query().Get("url"))
		rw.Header().Set("Content-Type", "image/png")
		rw.Write(png)
	}))
	defer proxy.Close()
	t.Setenv("UNI_SWITCH_ICON_PROXY", proxy.URL+"/api/icon")
	client := iconClient
	iconClient = proxy.Client
	t.Cleanup(func() { iconClient = client })

	// .invalid never resolves: reaching it straight fails the test
	const pic = "https://author.invalid/logo.png?v=2"
	ref, err := FetchIcon(context.Background(), pic)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(ref, ".png") {
		t.Fatalf("ref = %q", ref)
	}
	if len(asked) != 1 || asked[0] != "/api/icon "+pic {
		t.Fatalf("the proxy was asked %q", asked)
	}
	// the import link's own checks still come first
	if _, err := FetchIcon(context.Background(), "http://author.invalid/logo.png"); err == nil || len(asked) != 1 {
		t.Fatalf("an http icon went out: %v, %q", err, asked)
	}
}
