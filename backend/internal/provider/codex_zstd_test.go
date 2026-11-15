package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// A ChatGPT turn goes as Codex's client sends it (openai/codex
// codex-rs/http-client/src/request.rs: zstd level 3, Content-Encoding:
// zstd), and only a turn: CodexBase's /responses, POSTed, not already
// encoded. Anything else is left as it was.
func TestCodexCompressed(t *testing.T) {
	t.Cleanup(CodexZstdForTests(true))
	was := CodexBase
	CodexBase = "https://chatgpt.example/backend-api/codex"
	t.Cleanup(func() { CodexBase = was })
	body := bytes.Repeat([]byte(`{"model":"gpt-6-luna","input":"the same words again "}`), 20000)

	req, _ := http.NewRequest("POST", CodexBase+"/responses", bytes.NewReader(body))
	n := CodexCompressed(req, body)
	wire, _ := io.ReadAll(req.Body)
	if req.Header.Get("Content-Encoding") != "zstd" || n != len(wire) || req.ContentLength != int64(n) || n >= len(body)/10 {
		t.Fatalf("sent %d of %d bytes as %q, Content-Length %d", n, len(body), req.Header.Get("Content-Encoding"), req.ContentLength)
	}
	d, _ := zstd.NewReader(nil)
	defer d.Close()
	if out, err := d.DecodeAll(wire, nil); err != nil || !bytes.Equal(out, body) {
		t.Fatalf("decoded %d bytes, err %v", len(out), err)
	}
	again, _ := req.GetBody()
	if b, _ := io.ReadAll(again); !bytes.Equal(b, wire) {
		t.Error("GetBody gives another body")
	}

	for _, u := range []string{CodexBase + "/responses/compact", CodexBase + "/models", "https://api.openai.com/v1/responses", "http://127.0.0.1:1/backend-api/codex/responses"} {
		req, _ := http.NewRequest("POST", u, bytes.NewReader(body))
		if n := CodexCompressed(req, body); n != len(body) || req.Header.Get("Content-Encoding") != "" {
			t.Errorf("%s compressed", u)
		}
	}
	req, _ = http.NewRequest("POST", CodexBase+"/responses", bytes.NewReader(body))
	req.Header.Set("Content-Encoding", "gzip")
	if n := CodexCompressed(req, body); n != len(body) || req.Header.Get("Content-Encoding") != "gzip" {
		t.Error("an encoded body encoded again")
	}
}

// Large turns stay within Codex's client's 2 MiB window. The repeated
// image is farther back than 4 MiB, as inline screenshots in a history
// can be, so the encoder's default 8 MiB window would reach it.
func TestCodexCompressedLargeImageTurn(t *testing.T) {
	t.Cleanup(CodexZstdForTests(true))
	img := image.NewGray(image.Rect(0, 0, 2048, 2048))
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range img.Pix {
		img.Pix[i] = byte(rng.Uint64())
	}
	var encoded bytes.Buffer
	pngEncoder := png.Encoder{CompressionLevel: png.NoCompression}
	if err := pngEncoder.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	imageURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
	body, err := json.Marshal(map[string]any{
		"model": "gpt-6.1-sol", "stream": true, "store": false,
		"input": []any{
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "input_text", "text": "Inspect this screenshot."},
				map[string]any{"type": "input_image", "image_url": imageURL},
			}},
			map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "output_text", "text": strings.Repeat("The screenshot shows a test pattern. ", 4000)},
			}},
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "input_text", "text": "Compare it with this screenshot."},
				map[string]any{"type": "input_image", "image_url": imageURL},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(body) <= 8<<20 || !json.Valid(body) {
		t.Fatalf("fixture must be valid JSON larger than 8 MiB, got %d bytes", len(body))
	}
	req, err := http.NewRequest("POST", CodexBase+"/responses", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	CodexCompressed(req, body)
	wire, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	defer req.Body.Close()
	var header zstd.Header
	if err := header.Decode(wire); err != nil {
		t.Fatal(err)
	}
	window := header.WindowSize
	if header.SingleSegment {
		window = header.FrameContentSize
	}
	if window > 2<<20 {
		t.Errorf("zstd frame window = %d bytes, want at most 2 MiB", window)
	}
	d, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if out, err := d.DecodeAll(wire, nil); err != nil || !bytes.Equal(out, body) {
		t.Fatalf("decoded %d bytes, want original %d bytes, err %v", len(out), len(body), err)
	}
}

// The warm-up "hi" is a turn as Codex's are, and goes compressed too.
func TestCodexWarmGoesZstd(t *testing.T) {
	t.Cleanup(CodexZstdForTests(true))
	var enc string
	var got map[string]any
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enc = r.Header.Get("Content-Encoding")
		d, _ := zstd.NewReader(r.Body)
		defer d.Close()
		json.NewDecoder(d).Decode(&got)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n")
	}))
	defer fake.Close()
	was := CodexBase
	CodexBase = fake.URL + "/backend-api/codex"
	t.Cleanup(func() { CodexBase = was })

	if err := warmCodex(context.Background(), func(context.Context, *http.Request, []byte) error { return nil }, "gpt-6-luna", ""); err != nil {
		t.Fatal(err)
	}
	if enc != "zstd" || got["model"] != "gpt-6-luna" || got["input"] == nil {
		t.Fatalf("warmed %q: %v", enc, got)
	}
}

// The encoder's coders and their histories live as long as the process, so
// how many it keeps is set by uni-switch, not by the machine's cores: 28 turns
// of 1 MiB at once on 16 cores held ~96 MB with the library's default of a
// coder per core.
func TestCodexZstdHoldsLittle(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(16))
	var b bytes.Buffer
	for i := 0; b.Len() < 1<<20; i++ {
		b.WriteString(`{"role":"user","content":"line `)
		b.WriteString(strings.Repeat("x", i*7919%61))
		b.WriteString(`"},`)
	}
	src := b.Bytes()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	z := newCodexZstd()
	var wg sync.WaitGroup
	for range 28 {
		wg.Add(1)
		go func() { defer wg.Done(); z.EncodeAll(src, nil) }()
	}
	wg.Wait()
	runtime.GC()
	runtime.ReadMemStats(&after)
	if held := int64(after.HeapInuse) - int64(before.HeapInuse); held > 24<<20 {
		t.Fatalf("the encoder holds %d MB after 28 turns at once", held>>20)
	}
	runtime.KeepAlive(z)
}
