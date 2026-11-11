package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func fetchCall(url string, more string) string {
	u, _ := json.Marshal(url)
	return `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"web_fetch","arguments":{"url":` + string(u) + more + `}}}`
}

func fetchText_(t *testing.T, h http.Handler, body string) (string, bool) {
	t.Helper()
	m := readMCP(t, mcpPost(h, "127.0.0.1:50000", body))
	if m.Error != nil || len(m.Result.Content) != 1 {
		t.Fatalf("web_fetch: %+v", m)
	}
	return m.Result.Content[0].Text, m.Result.IsError
}

// web_fetch (#2286, blackrion) reads the page web_search found: an HTML page
// as Markdown (its title, headings, lists, links made absolute, code; no
// script or style), decoded from its charset, a long one in parts read on
// by offset; a page that isn't text, or answers an error, is the tool's
// error. Here the site is a local test server the client is pointed at.
func TestWebFetchReadsAPage(t *testing.T) {
	fresh(t)
	gbk := []byte("<html><head><meta charset=\"gbk\"><title>\xd6\xd0\xce\xc4</title></head><body><p>\xc4\xe3\xba\xc3</p></body></html>") // 中文, 你好
	long := strings.Repeat("abcdefghij", 50)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(`<!doctype html><html><head><title> Go  1.27 notes </title><style>body{color:red}</style><script>alert("no")</script></head>
<body><nav><a href="/"><svg></svg></a></nav><h1> Release   notes</h1>
<p>Go 1.27 is <b>out</b>. See <a href="/dl/?v=1.27">the downloads</a> &amp; <a href="#top">top</a>.</p>
<ul><li> faster <code>go test</code></li><li>new <a href="https://pkg.go.dev/iter"> iter </a></li></ul>
<pre>go install golang.org/dl/go1.27@latest
go1.27 download</pre><div hidden>secret</div><noscript>enable js</noscript></body></html>`))
		case "/gbk":
			w.Header().Set("Content-Type", "text/html")
			w.Write(gbk)
		case "/long":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(long))
		case "/old":
			http.Redirect(w, r, "/page", http.StatusFound)
		case "/png":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("\x89PNG\r\n\x1a\n"))
		default:
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(404)
			w.Write([]byte("<h1>Not here</h1>"))
		}
	}))
	t.Cleanup(srv.Close)
	was := webFetchClient
	webFetchClient = srv.Client()
	t.Cleanup(func() { webFetchClient = was })
	h := localGuard(New().Handler())

	got, isErr := fetchText_(t, h, fetchCall(srv.URL+"/old", ""))
	want := "URL: " + srv.URL + "/page\nTitle: Go 1.27 notes\n\n" +
		"# Release notes\n\n" +
		"Go 1.27 is out. See [the downloads](" + srv.URL + "/dl/?v=1.27) & top.\n\n" +
		"- faster `go test`\n- new [iter](https://pkg.go.dev/iter)\n\n" +
		"```\ngo install golang.org/dl/go1.27@latest\ngo1.27 download\n```"
	if isErr || got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}

	if got, _ := fetchText_(t, h, fetchCall(srv.URL+"/gbk", "")); !strings.Contains(got, "Title: 中文") || !strings.HasSuffix(got, "你好") {
		t.Fatalf("gbk: %q", got)
	}

	// in parts: 200 characters, then on from where it says
	got, _ = fetchText_(t, h, fetchCall(srv.URL+"/long", `,"max_chars":200`))
	if !strings.Contains(got, "\n\n"+long[:200]+"\n\n[300 more characters: call web_fetch with offset=200 to read on]") {
		t.Fatalf("first part: %q", got)
	}
	got, _ = fetchText_(t, h, fetchCall(srv.URL+"/long", `,"max_chars":200,"offset":400`))
	if !strings.HasSuffix(got, "\n\n"+long[400:]) {
		t.Fatalf("last part: %q", got)
	}
	got, _ = fetchText_(t, h, fetchCall(srv.URL+"/long", `,"offset":900`))
	if !strings.Contains(got, "none from offset 900") {
		t.Fatalf("past the end: %q", got)
	}

	for path, says := range map[string]string{"/png": "image/png, not a page of text", "/gone": "404 Not Found: # Not here"} {
		got, isErr := fetchText_(t, h, fetchCall(srv.URL+path, ""))
		if !isErr || !strings.Contains(got, says) {
			t.Fatalf("%s: %v %q", path, isErr, got)
		}
	}
}

// web_fetch downloads only from the public internet, as a caller's image
// is: a URL at this computer, the user's network, the cloud metadata
// address, or a name that resolves to one of them is refused before any
// connection, and so is a URL that isn't http(s).
func TestWebFetchOnlyPublic(t *testing.T) {
	fresh(t)
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true; w.Write([]byte("private")) }))
	t.Cleanup(srv.Close)
	was := lookupIP
	lookupIP = func(_ context.Context, host string) ([]netip.Addr, error) {
		if host == "intranet.test" {
			return []netip.Addr{netip.MustParseAddr("10.1.2.3")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	t.Cleanup(func() { lookupIP = was })
	h := localGuard(New().Handler())
	for _, u := range []string{srv.URL + "/", "http://localhost:" + strings.Split(srv.URL, ":")[2] + "/", "http://intranet.test/", "http://169.254.169.254/latest/meta-data/", "http://[::1]/", "http://192.168.1.1/"} {
		got, isErr := fetchText_(t, h, fetchCall(u, ""))
		if !isErr || !strings.Contains(got, "private address") {
			t.Fatalf("%s: %v %q", u, isErr, got)
		}
	}
	for _, u := range []string{"file:///etc/passwd", "ftp://example.com/", "gopher://x/", "example.com", ""} {
		got, isErr := fetchText_(t, h, fetchCall(u, ""))
		if !isErr || !(strings.Contains(got, "only http:// and https://") || strings.Contains(got, "needs a url")) {
			t.Fatalf("%q: %v %q", u, isErr, got)
		}
	}
	if hit {
		t.Fatal("a private address was reached")
	}
}
