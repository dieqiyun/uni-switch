package imagemcp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	pngenc "image/png"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// fourK is a real 3840x2160 PNG of about the size #1576's relay sent
// (10.6 MB): a gradient with noise over part of it, which doesn't compress.
var fourK = sync.OnceValue(func() []byte {
	const w, h = 3840, 2160
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	r := rand.New(rand.NewPCG(1576, 1))
	for y := range h {
		noisy := y%5 < 2
		for x := range w {
			c := color.NRGBA{uint8(x * 255 / w), uint8(y * 255 / h), 128, 255}
			if noisy {
				c.R, c.G, c.B = uint8(r.Uint32()), uint8(r.Uint32()), uint8(r.Uint32())
			}
			img.SetNRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	(&pngenc.Encoder{CompressionLevel: pngenc.BestSpeed}).Encode(&b, img)
	return b.Bytes()
})

// slowFile serves b in 256 KB pieces, every gap apart, as a vendor's
// file host far away does; with stallAfter > 0 it sends that much and
// then nothing more.
func slowFile(b []byte, gap time.Duration, stallAfter int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		for off := 0; off < len(b); off += 256 << 10 {
			if stallAfter > 0 && off >= stallAfter {
				select {
				case <-r.Context().Done():
				case <-time.After(10 * time.Second):
				}
				return
			}
			w.Write(b[off:min(off+256<<10, len(b))])
			w.(http.Flusher).Flush()
			time.Sleep(gap)
		}
	}
}

// urlGateway answers every images request with the URLs, whatever
// response_format asked (#1576's relay ignores it), and keeps the bodies.
type urlGateway struct {
	mu   sync.Mutex
	urls []string
	body []string
}

func (g *urlGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var b bytes.Buffer
	b.ReadFrom(r.Body)
	g.mu.Lock()
	g.body = append(g.body, b.String())
	g.mu.Unlock()
	var data []map[string]string
	for _, u := range g.urls {
		data = append(data, map[string]string{"url": u})
	}
	json.NewEncoder(w).Encode(map[string]any{"model": "one2api/gpt-image-2.5-sunburst", "data": data})
}

func initialized(t *testing.T, g http.Handler, project string) *client {
	c := start(t, g, project)
	c.call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"roots": map[string]any{}}, "clientInfo": map[string]any{"name": "dsh-mcp-client", "version": "1"}})
	return c
}

// await calls generate_image and waits on generation_result for its answer.
func (c *client) await(args map[string]any) (map[string]any, string) {
	c.t.Helper()
	r := c.call("tools/call", map[string]any{"name": "generate_image", "arguments": args})
	for range 400 {
		out := text(r)
		i := strings.Index(out, `with id "`)
		if r["isError"] == true || i < 0 {
			return r, out
		}
		id := out[i+len(`with id "`):]
		id = id[:strings.Index(id, `"`)]
		r = c.call("tools/call", map[string]any{"name": "generation_result", "arguments": map[string]any{"id": id}})
	}
	c.t.Fatalf("never done: %v", r)
	return nil, ""
}

// fetchLocal lets these tests' vendor, a server on this computer, be
// downloaded from: the user's fetchPrivate naming loopback.
func fetchLocal(t *testing.T) {
	t.Helper()
	was := settings.Load()
	st := was
	st.FetchPrivate = []string{"127.0.0.1"}
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { settings.Save(was) })
}

func shorterWaits(t *testing.T, stall time.Duration) {
	wasWithin, wasStall := answerWithin, gateway.ImageStall
	answerWithin, gateway.ImageStall = 200*time.Millisecond, stall
	t.Cleanup(func() { answerWithin, gateway.ImageStall = wasWithin, wasStall })
}

// #1576: a relay drew the 4K image in 40 s and answered with its URL, and
// generate_image lost it, "image 1: context deadline exceeded": its own
// download of the 10.6 MB had 2 minutes in all, whether or not it was still
// coming. A download that keeps coming now goes on to the end (here the
// cap that was is played by a stall shorter than the download), and the
// gateway was asked for the image itself.
func TestFourKImageByURLIsSaved(t *testing.T) {
	big := fourK()
	if len(big) < 9<<20 {
		t.Fatalf("the 4K image is %d bytes, not about 10.6 MB", len(big))
	}
	t.Logf("the 4K PNG is %.1f MB", float64(len(big))/(1<<20))
	// The stall also counts the time this process takes to grow the
	// download's buffer. Beside a full suite on a Linux box, a write that
	// grew one was held up to 2.7 s, and a 1 s stall went off at 4.2 MB of
	// a download that never stopped.
	shorterWaits(t, 5*time.Second)
	fetchLocal(t)
	files := httptest.NewServer(slowFile(big, 200*time.Millisecond, 0)) // ~8 s in all
	t.Cleanup(files.Close)
	g := &urlGateway{urls: []string{files.URL + "/4k.png"}}
	project := t.TempDir()
	c := initialized(t, g, project)
	began := time.Now()
	r, out := c.await(map[string]any{"prompt": "a 4K city", "size": "3840x2160", "path": "city.png"})
	saved := filepath.Join(project, "city.png")
	if r["isError"] == true || !strings.Contains(out, saved) {
		t.Fatalf("after %s: %s", time.Since(began).Round(time.Millisecond), out)
	}
	if took := time.Since(began); took <= gateway.ImageStall {
		t.Fatalf("the download took %s, no longer than the stall: not the slow one this is about", took)
	}
	if b, _ := os.ReadFile(saved); !bytes.Equal(b, big) {
		t.Fatalf("saved %d bytes of %d", len(b), len(big))
	}
	if !strings.Contains(g.body[0], `"response_format":"b64_json"`) {
		t.Fatalf("the gateway was asked %s", g.body[0])
	}
}

// The same image as b64_json (what the gateway now answers) is saved whole.
func TestFourKImageAsB64IsSaved(t *testing.T) {
	big := fourK()
	shorterWaits(t, time.Second)
	g := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"model": "one2api/gpt-image-2.5-sunburst", "data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(big)}}})
	})
	project := t.TempDir()
	c := initialized(t, g, project)
	r, out := c.await(map[string]any{"prompt": "a 4K city", "size": "3840x2160", "path": "city.png"})
	saved := filepath.Join(project, "city.png")
	if r["isError"] == true || !strings.Contains(out, saved) {
		t.Fatalf("%s", out)
	}
	if b, _ := os.ReadFile(saved); !bytes.Equal(b, big) {
		t.Fatalf("saved %d bytes of %d", len(b), len(big))
	}
}

// A download that stops is given up when nothing has come for ImageStall,
// and the image, drawn and paid for, is answered by where the vendor keeps
// it; one of two that came is saved and said beside it.
func TestStalledDownloadSaysWhereTheImageIs(t *testing.T) {
	big := fourK()
	// A second, and a small image for the one that comes in full: the
	// stall also counts the time this process takes to grow the download's
	// buffer, and at 300 ms, beside a full suite on a Linux box, it went off
	// at 8.2 MB of a 4K image that was still coming, and once at 0.0 MB.
	shorterWaits(t, time.Second)
	fetchLocal(t)
	stuck := httptest.NewServer(slowFile(big, 0, 1<<20))
	t.Cleanup(stuck.Close)
	fine := httptest.NewServer(slowFile(png, 0, 0))
	t.Cleanup(fine.Close)

	project := t.TempDir()
	c := initialized(t, &urlGateway{urls: []string{stuck.URL + "/a.png"}}, project)
	r, out := c.await(map[string]any{"prompt": "a 4K city", "path": "city.png"})
	if r["isError"] != true || !strings.Contains(out, stuck.URL+"/a.png") || !strings.Contains(out, "nothing came for 1s, after 1.0 MB") {
		t.Fatalf("%v: %s", r["isError"], out)
	}

	c = initialized(t, &urlGateway{urls: []string{fine.URL + "/a.png", stuck.URL + "/b.png"}}, project)
	r, out = c.await(map[string]any{"prompt": "two cities", "n": 2, "path": "two.png"})
	if r["isError"] == true || !strings.Contains(out, filepath.Join(project, "two-1.png")) || !strings.Contains(out, "Not saved: image 2") || !strings.Contains(out, stuck.URL+"/b.png") {
		t.Fatalf("%s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(project, "two-1.png")); !bytes.Equal(b, png) {
		t.Fatalf("saved %d bytes of %d", len(b), len(png))
	}
}

// The URL an image is answered by is the vendor's, and what the model was
// steered to draw can steer it: one on this computer or its network is
// not downloaded unless the user's fetchPrivate names it, and the answer
// says where the image is.
func TestVendorImageAtPrivateAddressNotFetched(t *testing.T) {
	shorterWaits(t, time.Second)
	var hits sync.Mutex
	n := 0
	inside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Lock()
		n++
		hits.Unlock()
		w.Write([]byte("\x89PNG\r\n\x1a\n secret"))
	}))
	t.Cleanup(inside.Close)
	project := t.TempDir()
	c := initialized(t, &urlGateway{urls: []string{inside.URL + "/admin.png"}}, project)
	r, out := c.await(map[string]any{"prompt": "a city", "path": "city.png"})
	if r["isError"] != true || !strings.Contains(out, "private address") || !strings.Contains(out, inside.URL+"/admin.png") {
		t.Fatalf("%v: %s", r["isError"], out)
	}
	hits.Lock()
	defer hits.Unlock()
	if n != 0 {
		t.Fatalf("the private server was asked %d times", n)
	}
	if _, err := os.Stat(filepath.Join(project, "city.png")); err == nil {
		t.Fatal("something was saved")
	}
}
