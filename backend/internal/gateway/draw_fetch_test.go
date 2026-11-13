package gateway

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// bigPNG is as many bytes as #1576's 4K PNG (10.6 MB), a PNG by its
// signature.
func bigPNG() []byte {
	b := make([]byte, 10_600_000)
	r := rand.New(rand.NewPCG(1576, 2))
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	copy(b, "\x89PNG\r\n\x1a\n")
	return b
}

// fileProxy is the proxy a provider names, standing in front of the host
// its vendor keeps the images it draws on: it serves them, and keeps the
// URLs it was asked for.
type fileProxy struct {
	mu    sync.Mutex
	asked []string
	files map[string][]byte
}

func (f *fileProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.asked = append(f.asked, r.URL.String())
	f.mu.Unlock()
	b, ok := f.files[r.URL.Path]
	// the image's URL comes to the proxy pinned to the public address
	// files.test was checked at (publicTransport)
	if r.URL.Host != filesAddr || !ok {
		w.WriteHeader(404)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(b)
}

func (f *fileProxy) got() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...)
}

// filesAddr is the public address files.test resolves to in these tests.
const filesAddr = "93.184.216.34:80"

// relayed is a gateway whose provider "relay" answers images with URLs on
// files.test, a host reached only through the provider's own proxy.
func relayed(t *testing.T, vendor http.HandlerFunc) (*Server, *fileProxy) {
	t.Helper()
	fresh(t)
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy"} {
		t.Setenv(k, "")
	}
	up := httptest.NewServer(vendor)
	t.Cleanup(up.Close)
	px := &fileProxy{files: map[string][]byte{}}
	proxy := httptest.NewServer(px)
	t.Cleanup(proxy.Close)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Chat: up.URL + "/v1", Key: "key", Proxy: proxy.URL, Models: []string{"gpt-image-2.5-sunburst"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("relay", up.URL+"/v1", []catalog.Model{{ID: "gpt-image-2.5-sunburst"}}); err != nil {
		t.Fatal(err)
	}
	resolveTo(t, map[string]string{"files.test": "93.184.216.34"})
	return New(), px
}

type fetchedAnswer struct {
	Data []struct {
		B64  string `json:"b64_json"`
		URL  string `json:"url"`
		Mime string `json:"mime_type"`
	} `json:"data"`
}

// #1576: a relay answers a 4K image by URL, whatever response_format says.
// A caller that asked for b64_json (generate_image does) gets the image
// itself, fetched through the provider's own proxy, the way the relay was
// reached; one that can't be fetched is answered by its URL, not dropped;
// a caller that asked for nothing gets the URL as before.
func TestImageByURLIsFetchedForB64Caller(t *testing.T) {
	big := bigPNG()
	var asked []string
	var mu sync.Mutex
	s, px := relayed(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		asked = append(asked, string(b))
		mu.Unlock()
		name := "/4k.png"
		if strings.Contains(string(b), "gone") {
			name = "/gone.png"
		}
		io.WriteString(w, `{"created":1,"data":[{"url":"http://files.test`+name+`"}]}`)
	})
	px.files["/4k.png"] = big

	code, _, raw := postImages(t, s, "/v1/images/generations", "application/json", `{"model":"relay/gpt-image-2.5-sunburst","prompt":"a 4K city","size":"3840x2160","response_format":"b64_json"}`)
	var a fetchedAnswer
	json.Unmarshal([]byte(raw), &a)
	if code != 200 || len(a.Data) != 1 || a.Data[0].URL != "" || a.Data[0].Mime != "image/png" {
		t.Fatalf("%d %.300s", code, raw)
	}
	if b, _ := base64.StdEncoding.DecodeString(a.Data[0].B64); !bytes.Equal(b, big) {
		t.Fatalf("got %d bytes of %d", len(b), len(big))
	}
	if got := px.got(); len(got) != 1 || got[0] != "http://"+filesAddr+"/4k.png" {
		t.Fatalf("the provider's proxy was asked %v", got)
	}
	if strings.Contains(asked[0], "response_format") {
		t.Fatalf("gpt-image was sent response_format: %s", asked[0])
	}

	code, _, raw = postImages(t, s, "/v1/images/generations", "application/json", `{"model":"relay/gpt-image-2.5-sunburst","prompt":"gone","response_format":"b64_json"}`)
	a = fetchedAnswer{}
	json.Unmarshal([]byte(raw), &a)
	if code != 200 || len(a.Data) != 1 || a.Data[0].URL != "http://files.test/gone.png" {
		t.Fatalf("an image that can't be fetched: %d %.300s", code, raw)
	}

	code, _, raw = postImages(t, s, "/v1/images/generations", "application/json", `{"model":"relay/gpt-image-2.5-sunburst","prompt":"a 4K city"}`)
	a = fetchedAnswer{}
	json.Unmarshal([]byte(raw), &a)
	if code != 200 || len(a.Data) != 1 || a.Data[0].URL != "http://files.test/4k.png" || len(px.got()) != 2 {
		t.Fatalf("asked for no format: %d %.300s, proxy asked %v", code, raw, px.got())
	}
}

// #1576's 502s at 300 s said only "context deadline exceeded". A vendor
// that began answering but didn't finish in drawTimeout is said to have.
func TestVendorThatStopsMidAnswerIsSaid(t *testing.T) {
	was := drawUnit
	drawUnit = 60 * time.Millisecond // the 5 minutes, 300ms
	t.Cleanup(func() { drawUnit = was })
	s, _ := relayed(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"created":1,"data":[`)
		w.(http.Flusher).Flush()
		// only the gateway's deadline may end the answer: a timer of the
		// fake's could end it first under load and be read as a 502 (#4245)
		<-r.Context().Done()
	})
	code, _, raw := postImages(t, s, "/v1/images/generations", "application/json", `{"model":"relay/gpt-image-2.5-sunburst","prompt":"a 4K city"}`)
	if code != 504 || !strings.Contains(raw, "Relay began answering after 0s (200 OK) but hadn't sent all of its answer in 300ms: what came doesn't yet say whether the image was coming as a url or as b64_json; 21 B had come") {
		t.Fatalf("%d %s", code, raw)
	}
}

// david-zdw's relay (#1576) answered 2048x1152 on one domain with a 3 MB
// b64_json body that took 380 s, and on another with a 416 B url. Which of
// the two stalled is said, with how much had come.
func TestStalledAnswerSaysUrlOrB64(t *testing.T) {
	was := drawUnit
	drawUnit = 100 * time.Millisecond // the 5 minutes, 500ms
	t.Cleanup(func() { drawUnit = was })
	for _, c := range []struct {
		name, sent, want string
	}{
		{"b64_json", `{"created":1,"data":[{"b64_json":"iVBORw0KGgo` + strings.Repeat("A", 3000) + `"`, "it was sending the image itself as b64_json; 3.0 KB had come, and nothing more for the last "},
		{"url", `{"created":1,"data":[{"url":"https://files.example/a.png"}]`, "it was answering with the image's url, not the image; 59 B had come"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, _ := relayed(t, func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, c.sent)
				w.(http.Flusher).Flush()
				// only the gateway's deadline may end the answer (#4245)
				<-r.Context().Done()
			})
			code, _, raw := postImages(t, s, "/v1/images/generations", "application/json", `{"model":"relay/gpt-image-2.5-sunburst","prompt":"a 2K city","size":"2048x1152"}`)
			if code != 504 || !strings.Contains(raw, c.want) {
				t.Fatalf("%d %s", code, raw)
			}
		})
	}
}

func TestSizeOf(t *testing.T) {
	for n, want := range map[int]string{386: "386 B", 12400: "12.4 KB", 3_000_000: "3.0 MB", 19_700_000: "19.7 MB"} {
		if got := sizeOf(n); got != want {
			t.Errorf("sizeOf(%d) = %q, want %q", n, got, want)
		}
	}
}

// The URL a vendor answers an image with is in the vendor's answer, and
// what the model was steered to draw or do can steer it: one on this
// computer or its network is answered by URL, never downloaded and handed
// back as the image. The vendor's own host:port, which the user named
// themselves, is downloaded from.
func TestVendorImageURLAtPrivateAddressNotFetched(t *testing.T) {
	fresh(t)
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy"} {
		t.Setenv(k, "")
	}
	inside, hits := secret(t, string(pngBytes))
	resolveTo(t, map[string]string{"inside.test": "127.0.0.1"})
	_, port, _ := strings.Cut(strings.TrimPrefix(inside.URL, "http://"), ":")
	var next sync.Mutex
	answer := ""
	var up *httptest.Server
	up = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/files/own.png" {
			w.Write(pngBytes)
			return
		}
		next.Lock()
		u := answer
		next.Unlock()
		io.WriteString(w, `{"created":1,"data":[{"url":"`+u+`"}]}`)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Chat: up.URL + "/v1", Key: "key", Models: []string{"gpt-image-2.5-sunburst"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("relay", up.URL+"/v1", []catalog.Model{{ID: "gpt-image-2.5-sunburst"}}); err != nil {
		t.Fatal(err)
	}
	s := New()
	draw := func(u string) fetchedAnswer {
		t.Helper()
		next.Lock()
		answer = u
		next.Unlock()
		code, _, raw := postImages(t, s, "/v1/images/generations", "application/json", `{"model":"relay/gpt-image-2.5-sunburst","prompt":"a city","response_format":"b64_json"}`)
		var a fetchedAnswer
		json.Unmarshal([]byte(raw), &a)
		if code != 200 || len(a.Data) != 1 {
			t.Fatalf("%s: %d %.300s", u, code, raw)
		}
		return a
	}
	for _, u := range []string{inside.URL + "/admin.png", "http://inside.test:" + port + "/admin.png", "http://[::ffff:127.0.0.1]:" + port + "/admin.png"} {
		if a := draw(u); a.Data[0].URL != u || a.Data[0].B64 != "" {
			t.Fatalf("%s was answered %+v", u, a.Data[0])
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("the private service was asked %d times", hits.Load())
	}
	if a := draw(up.URL + "/files/own.png"); a.Data[0].URL != "" || a.Data[0].B64 != base64.StdEncoding.EncodeToString(pngBytes) {
		t.Fatalf("the vendor's own file: %+v", a.Data[0])
	}
}
