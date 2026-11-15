package gateway

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"

	"os"
	"strings"

	"testing"
	"time"

	"github.com/tidwall/gjson"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// testdata/opaque.heic (32x24) and alpha.heic (24x24, transparent around
// an icon) were written by macOS's own encoder, sips -s format heic, as an
// iPhone writes its photos.
func heicFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if !isHEIC(b) {
		t.Fatalf("%s is not a HEIC", name)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// sentImage reads the image a data: URL or bare base64 holds, its type as
// the request says it and as its bytes are.
func sentImage(t *testing.T, mime, b64 string) (image.Image, string) {
	t.Helper()
	if rest, ok := strings.CutPrefix(b64, "data:"); ok {
		head, data, _ := strings.Cut(rest, ",")
		mime, b64 = strings.TrimSuffix(head, ";base64"), data
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("not base64: %v", err)
	}
	img, format, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("sent as %s, not an image: %v", mime, err)
	}
	if "image/"+format != mime {
		t.Fatalf("sent as %s, its bytes are %s", mime, format)
	}
	return img, mime
}

// A HEIC goes as JPEG in every request shape that carries an image, its
// block's other fields kept, whatever type the request gave it; one with
// transparency goes as PNG.
func TestHEICGoesAsJPEG(t *testing.T) {
	opaque, alpha := heicFixture(t, "opaque.heic"), heicFixture(t, "alpha.heic")
	cases := []struct {
		name  string
		proto provider.Protocol
		body  string
		path  string // gjson path of the image's base64 or data: URL
		mime  string // gjson path of its type, when apart from it
		kept  string // gjson path of a field that must be kept
		want  string
	}{
		{"chat url string", provider.Chat, `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"what is it"},{"type":"image_url","image_url":"data:image/heic;base64,` + opaque + `"}]}]}`,
			"messages.0.content.1.image_url", "", "messages.0.content.0.text", "image/jpeg"},
		{"chat url object", provider.Chat, `{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/heic;base64,` + opaque + `","detail":"high"}}]}]}`,
			"messages.0.content.0.image_url.url", "", "messages.0.content.0.image_url.detail", "image/jpeg"},
		{"chat heic named jpeg", provider.Chat, `{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,` + opaque + `"}}]}]}`,
			"messages.0.content.0.image_url.url", "", "model", "image/jpeg"},
		{"chat tool image", provider.Chat, `{"model":"m","messages":[{"role":"user","content":"look"},{"role":"tool","tool_call_id":"c","content":[{"type":"image_url","image_url":{"url":"data:image/heif;base64,` + opaque + `"}}]}]}`,
			"messages.1.content.0.image_url.url", "", "messages.1.tool_call_id", "image/jpeg"},
		{"chat transparent", provider.Chat, `{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/heic;base64,` + alpha + `"}}]}]}`,
			"messages.0.content.0.image_url.url", "", "model", "image/png"},
		{"responses", provider.Responses, `{"model":"m","input":[{"role":"user","content":[{"type":"input_image","image_url":"data:image/heic;base64,` + opaque + `","detail":"auto"}]}]}`,
			"input.0.content.0.image_url", "", "input.0.content.0.detail", "image/jpeg"},
		{"responses tool output", provider.Responses, `{"model":"m","input":[{"type":"function_call_output","call_id":"c","output":[{"type":"input_image","image_url":"data:image/heic;base64,` + opaque + `"}]}]}`,
			"input.0.output.0.image_url", "", "input.0.call_id", "image/jpeg"},
		{"messages", provider.Anthropic, `{"model":"m","max_tokens":9,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/heic","data":"` + opaque + `"},"cache_control":{"type":"ephemeral"}}]}]}`,
			"messages.0.content.0.source.data", "messages.0.content.0.source.media_type", "messages.0.content.0.cache_control.type", "image/jpeg"},
		{"messages named png", provider.Anthropic, `{"model":"m","max_tokens":9,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + opaque + `"}}]}]}`,
			"messages.0.content.0.source.data", "messages.0.content.0.source.media_type", "max_tokens", "image/jpeg"},
		{"messages tool_result", provider.Anthropic, `{"model":"m","max_tokens":9,"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":[{"type":"image","source":{"type":"base64","media_type":"image/heic","data":"` + alpha + `"}}]}]}]}`,
			"messages.0.content.0.content.0.source.data", "messages.0.content.0.content.0.source.media_type", "messages.0.content.0.tool_use_id", "image/png"},
		{"gemini", provider.Gemini, `{"contents":[{"role":"user","parts":[{"text":"what"},{"inlineData":{"mimeType":"image/heic","data":"` + opaque + `"}}]}]}`,
			"contents.0.parts.1.inlineData.data", "contents.0.parts.1.inlineData.mimeType", "contents.0.parts.0.text", "image/jpeg"},
		{"gemini function response", provider.Gemini, `{"contents":[{"role":"user","parts":[{"functionResponse":{"name":"shot","response":{},"parts":[{"inlineData":{"mimeType":"image/heic","data":"` + opaque + `"}}]}}]}]}`,
			"contents.0.parts.0.functionResponse.parts.0.inlineData.data", "contents.0.parts.0.functionResponse.parts.0.inlineData.mimeType", "contents.0.parts.0.functionResponse.name", "image/jpeg"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !mayHoldHEIC([]byte(c.body)) {
				t.Fatal("the scan missed the HEIC")
			}
			out := heicAsJPEG(c.proto, []byte(c.body))
			if !json.Valid(out) {
				t.Fatalf("not JSON: %.200s", out)
			}
			before, after := gjson.Get(c.body, c.kept), gjson.GetBytes(out, c.kept)
			if !before.Exists() || before.Raw != after.Raw {
				t.Fatalf("%s was %s, is %s", c.kept, before.Raw, after.Raw)
			}
			mime := ""
			if c.mime != "" {
				mime = gjson.GetBytes(out, c.mime).Str
			}
			img, got := sentImage(t, mime, gjson.GetBytes(out, c.path).Str)
			if got != c.want {
				t.Fatalf("sent as %s, want %s", got, c.want)
			}
			b := img.Bounds()
			if c.want == "image/jpeg" && (b.Dx() != 32 || b.Dy() != 24) {
				t.Fatalf("%dx%d, want 32x24", b.Dx(), b.Dy())
			}
			if c.want == "image/png" {
				if b.Dx() != 24 || b.Dy() != 24 {
					t.Fatalf("%dx%d, want 24x24", b.Dx(), b.Dy())
				}
				// HEVC's alpha is lossy: near 0, not 0
				if _, _, _, a := img.At(0, 0).RGBA(); a > 0x1000 {
					t.Fatalf("the corner's alpha is %#x, want transparent", a)
				}
			}
			if mayHoldHEIC(out) {
				t.Fatalf("a HEIC is still in it")
			}
		})
	}
}

// What the JPEG shows is the photo: its colours come through the decoder,
// not a blank of the right size.
func TestHEICPixelsKept(t *testing.T) {
	out := heicAsJPEG(provider.Chat, []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":"data:image/heic;base64,`+heicFixture(t, "opaque.heic")+`"}]}]}`))
	img, _ := sentImage(t, "", gjson.GetBytes(out, "messages.0.content.0.image_url").Str)
	var lo, hi uint32 = 1 << 16, 0
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			lo, hi = min(lo, r, g, b), max(hi, r, g, b)
		}
	}
	if hi-lo < 1<<12 {
		t.Fatalf("the image is flat (%d..%d): nothing was decoded", lo, hi)
	}
}

// A request with no HEIC goes byte for byte as it came: a PNG, a JPEG, an
// image/heic whose bytes are not one, a URL.
func TestNonHEICImageUnchanged(t *testing.T) {
	var p, j bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	png.Encode(&p, img)
	jpeg.Encode(&j, img, nil)
	pb, jb := base64.StdEncoding.EncodeToString(p.Bytes()), base64.StdEncoding.EncodeToString(j.Bytes())
	for _, c := range []struct {
		proto provider.Protocol
		body  string
	}{
		{provider.Chat, `{"model":"m",  "messages":[{"role":"user","content":[{"type":"image_url","image_url":"data:image/png;base64,` + pb + `"}]}]}`},
		{provider.Chat, `{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/heic;base64,` + jb + `"}}]}]}`},
		{provider.Chat, `{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/a.heic"}}]}]}`},
		{provider.Responses, `{"input":[{"role":"user","content":[{"type":"input_image","image_url":"data:image/jpeg;base64,` + jb + `"}]}]}`},
		{provider.Anthropic, `{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/heic","data":"` + pb + `"}}]}]}`},
		{provider.Gemini, `{"contents":[{"parts":[{"inlineData":{"mimeType":"image/png","data":"` + pb + `"}}]}]}`},
		// the HEIC's base64 as text a user pasted is not an image
		{provider.Chat, `{"messages":[{"role":"user","content":"` + heicFixture(t, "opaque.heic") + `"}]}`},
	} {
		if out := heicAsJPEG(c.proto, []byte(c.body)); !bytes.Equal(out, []byte(c.body)) {
			t.Errorf("%s changed:\n%.300s\nto\n%.300s", c.proto, c.body, out)
		}
	}
}

// One that can't be converted goes as it came: a decoder that panics on
// it, one that takes too long, a file cut short.
func TestHEICUnconvertedGoesAsItCame(t *testing.T) {
	opaque := heicFixture(t, "opaque.heic")
	body := func(b64 string) []byte {
		return []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":"data:image/heic;base64,` + b64 + `"}]}]}`)
	}
	reset := func() {
		heicCache.Lock()
		heicCache.m, heicCache.order, heicCache.size = nil, nil, 0
		heicCache.Unlock()
	}
	reset()
	t.Cleanup(reset)

	cut, _ := base64.StdEncoding.DecodeString(opaque)
	short := body(base64.StdEncoding.EncodeToString(cut[:len(cut)/2]))
	if out := heicAsJPEG(provider.Chat, short); !bytes.Equal(out, short) {
		t.Fatalf("a HEIC cut short changed: %.200s", out)
	}

	enc := heicEncode
	t.Cleanup(func() { heicEncode = enc })
	heicEncode = func([]byte) (string, []byte) { panic("bad file") }
	if out := heicAsJPEG(provider.Chat, body(opaque)); !bytes.Equal(out, body(opaque)) {
		t.Fatalf("a decoder's panic changed the body: %.200s", out)
	}
	reset()

	wait := heicWait
	t.Cleanup(func() { heicWait = wait })
	heicWait = 50 * time.Millisecond
	release := make(chan struct{})
	finished := make(chan struct{})
	heicEncode = func(raw []byte) (string, []byte) {
		defer close(finished)
		<-release
		return enc(raw)
	}
	if out := heicAsJPEG(provider.Chat, body(opaque)); !bytes.Equal(out, body(opaque)) {
		t.Fatalf("a conversion past its wait changed the body: %.200s", out)
	}
	// finished later, it serves the next turn from the cache
	close(release)
	<-finished
	heicWait = wait
	heicEncode = func([]byte) (string, []byte) { t.Error("converted twice"); return "", nil }
	deadline := time.Now().Add(10 * time.Second)
	for {
		heicCache.Lock()
		n := len(heicCache.m)
		heicCache.Unlock()
		if n == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	out := heicAsJPEG(provider.Chat, body(opaque))
	if _, mime := sentImage(t, "", gjson.GetBytes(out, "messages.0.content.0.image_url").Str); mime != "image/jpeg" {
		t.Fatalf("the next turn sent %s", mime)
	}
}
