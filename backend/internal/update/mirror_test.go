package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func updateHome(t *testing.T) {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy"} {
		t.Setenv(k, "")
	}
}

// A release's file comes through a GitHub download mirror only when one is
// given (uni-switch update --mirror, or Settings' UpdateMirror): its prefix
// before the github.com URL. The feed, with the file's SHA-256, is never
// asked through it, and a file the mirror changed is refused (akic404 on
// Discord: install and update without a proxy).
func TestUpdateThroughAMirror(t *testing.T) {
	updateHome(t)
	body := []byte("new uni-switch")
	sum := sha256.Sum256(body)
	asset := Asset{URL: "https://github.com/dieqiyun/uni-switch/releases/download/v9.9.9/uni-switch-cli-linux-amd64", SHA256: hex.EncodeToString(sum[:])}

	var mu sync.Mutex
	var seen []string
	tamper := false
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Path)
		bad := tamper
		mu.Unlock()
		if bad {
			w.Write([]byte("not uni-switch"))
			return
		}
		w.Write(body)
	}))
	defer mirror.Close()
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Release{Version: "9.9.9", Assets: map[string]Asset{BinaryAsset(): asset}})
	}))
	defer feed.Close()
	t.Setenv("UNI_SWITCH_UPDATE_FEED", feed.URL+"/api/latest")

	// none unless given: no mirror is uni-switch's own
	if m := Mirror(context.Background()); m != "" {
		t.Fatalf("a mirror by default: %q", m)
	}
	ctx := WithMirror(context.Background(), mirror.URL)
	rel, err := Latest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "uni-switch.new")
	if err := download(ctx, rel.Assets[BinaryAsset()], path); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != string(body) {
		t.Fatalf("downloaded %q", b)
	}
	mu.Lock()
	if want := []string{"/" + asset.URL}; !slices.Equal(seen, want) {
		t.Fatalf("the mirror was asked %v, want %v (the feed never)", seen, want)
	}
	seen, tamper = nil, true
	mu.Unlock()

	// what the mirror changed doesn't match the feed's checksum
	os.Remove(path)
	err = download(ctx, asset, path)
	if err == nil || !strings.Contains(err.Error(), "does not match its checksum") || !strings.Contains(err.Error(), "mirror") {
		t.Fatalf("a tampered file: %v", err)
	}
	if _, serr := os.Stat(path); !os.IsNotExist(serr) {
		t.Fatal("the tampered file was kept")
	}

	// Settings' mirror is the app's too; off turns it off for one run
	mu.Lock()
	tamper, seen = false, nil
	mu.Unlock()
	if err := settings.Save(settings.Settings{UpdateMirror: mirror.URL + "/"}); err != nil {
		t.Fatal(err)
	}
	if m := Mirror(context.Background()); m != mirror.URL+"/" {
		t.Fatalf("Settings' mirror: %q", m)
	}
	if err := download(context.Background(), asset, path); err != nil {
		t.Fatal(err)
	}
	if m := Mirror(WithMirror(context.Background(), "off")); m != "" {
		t.Fatalf("off: %q", m)
	}
	// a file not on GitHub goes as it is
	if got := mirrored(mirror.URL, feed.URL+"/x"); got != feed.URL+"/x" {
		t.Fatalf("a non-GitHub URL mirrored: %q", got)
	}
	if err := settings.Save(settings.Settings{UpdateMirror: "ftp://nope"}); err == nil {
		t.Fatal("an ftp mirror was saved")
	}
}

// uni-switch update --proxy takes the feed and the download through the proxy
// given, over Settings' and the environment's.
func TestUpdateThroughAProxyGiven(t *testing.T) {
	updateHome(t)
	body := []byte("new uni-switch")
	sum := sha256.Sum256(body)
	var mu sync.Mutex
	var seen []string
	px := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.String())
		mu.Unlock()
		if r.URL.Path == "/asset" {
			w.Write(body)
			return
		}
		json.NewEncoder(w).Encode(Release{Version: "9.9.9", Assets: map[string]Asset{
			BinaryAsset(): {URL: "http://releases.uni-switch.invalid/asset", SHA256: hex.EncodeToString(sum[:])},
		}})
	}))
	defer px.Close()
	if err := settings.Save(settings.Settings{Proxy: "direct"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UNI_SWITCH_UPDATE_FEED", "http://feed.uni-switch.invalid/api/latest")
	ctx := WithProxy(context.Background(), px.URL)
	rel, err := Latest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := download(ctx, rel.Assets[BinaryAsset()], filepath.Join(t.TempDir(), "uni-switch.new")); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := []string{"http://feed.uni-switch.invalid/api/latest", "http://releases.uni-switch.invalid/asset"}; !slices.Equal(seen, want) {
		t.Fatalf("through the proxy: %v, want %v", seen, want)
	}
	for _, bad := range []string{"ftp://127.0.0.1:21", "socks4://127.0.0.1:1080"} {
		if CheckProxy(bad) == nil {
			t.Errorf("%s taken as a proxy", bad)
		}
	}
	for _, good := range []string{"127.0.0.1:7890", "http://127.0.0.1:7890", "socks5://127.0.0.1:1080", "socks5h://u:p@127.0.0.1:1080", "direct"} {
		if err := CheckProxy(good); err != nil {
			t.Errorf("%s: %v", good, err)
		}
	}
}

// #1886: CheckProxy's error, shown in the GUI and the CLI, names the
// proxy without its user and password.
func TestCheckProxyErrorShowsNoCredentials(t *testing.T) {
	for _, c := range []struct{ proxy, shown string }{
		{"ftp://alice:s3cret@10.0.0.2:21", `"ftp://10.0.0.2:21"`},
		{"socks4://ali%40ce:s3%3Acret@10.0.0.2:1080", `"socks4://10.0.0.2:1080"`},
		{"socks4://alice@10.0.0.2:1080", `"socks4://10.0.0.2:1080"`},
		{"http://alice:s3cret@", ""},
		{"alice:s3cret%zz@10.0.0.2:8080", ""},
	} {
		err := CheckProxy(c.proxy)
		if err == nil {
			t.Fatalf("%s taken as a proxy", c.proxy)
		}
		if c.shown != "" && !strings.Contains(err.Error(), c.shown) {
			t.Errorf("%s: %v, want it to name %s", c.proxy, err, c.shown)
		}
		for _, secret := range []string{"ali", "s3"} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("%s: %v shows %q", c.proxy, err, secret)
			}
		}
	}
}

// A download that fails through a mirror says it is the mirror's (#893):
// one that doesn't answer, answers 404, or sends another file, which is
// never kept; the GUI then offers GitHub itself. One from GitHub is no
// mirror's. Settings take a full https:// mirror only.
func TestMirrorFailuresAreTheMirrors(t *testing.T) {
	updateHome(t)
	body := []byte("new uni-switch")
	sum := sha256.Sum256(body)
	asset := Asset{URL: "https://github.com/dieqiyun/uni-switch/releases/download/v9.9.9/uni-switch-cli-linux-amd64", SHA256: hex.EncodeToString(sum[:])}
	tampered := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("not uni-switch")) }))
	defer tampered.Close()
	missing := httptest.NewServer(http.NotFoundHandler())
	defer missing.Close()
	// port 1 is refused at once and no test can listen on it, where a
	// closed server's port can be handed to the next server at once
	gone := "http://127.0.0.1:1/"
	for _, m := range []string{tampered.URL + "/", missing.URL + "/", gone} {
		path := filepath.Join(t.TempDir(), "uni-switch.new")
		err := fetch(WithMirror(context.Background(), m), asset, path)
		if err == nil || MirrorOf(err) != m || !strings.Contains(err.Error(), strings.TrimSuffix(m, "/")) {
			t.Errorf("through %s: %v (mirror %q)", m, err, MirrorOf(err))
		}
		if _, serr := os.Stat(path); !os.IsNotExist(serr) {
			t.Errorf("through %s the file was kept", m)
		}
	}
	// straight from a host that fails: no mirror's
	direct := Asset{URL: missing.URL + "/asset", SHA256: asset.SHA256}
	if err := fetch(context.Background(), direct, filepath.Join(t.TempDir(), "x")); err == nil || MirrorOf(err) != "" {
		t.Errorf("a direct failure: %v (mirror %q)", err, MirrorOf(err))
	}
	for _, bad := range []string{"", "http://mirror.example/", "mirror.example", "https://", "ftp://mirror.example/", "https://mirror.example/ x"} {
		if CheckMirrorURL(bad) == nil {
			t.Errorf("%q taken as a mirror", bad)
		}
	}
	for _, good := range []string{"https://mirror.example/", "https://mirror.example/gh", " https://mirror.example "} {
		if err := CheckMirrorURL(good); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
}
