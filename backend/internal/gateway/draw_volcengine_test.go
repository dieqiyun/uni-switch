package gateway

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// arkImages stands in for Volcengine Ark's images API
// (https://www.volcengine.com/docs/82379/1541523): JSON only, at
// /api/v3/images/generations, one image an answer.
type arkImages struct {
	mu     sync.Mutex
	bodies []string
	types  []string
}

func (a *arkImages) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Authorization") != "Bearer key" {
		w.WriteHeader(401)
		io.WriteString(w, `{"error":{"code":"AuthenticationError","message":"bad key"}}`)
		return
	}
	body, _ := io.ReadAll(r.Body)
	a.mu.Lock()
	a.bodies = append(a.bodies, string(body))
	a.types = append(a.types, r.Header.Get("Content-Type"))
	a.mu.Unlock()
	if r.Method != "POST" || r.URL.Path != "/api/v3/images/generations" {
		w.WriteHeader(404)
		io.WriteString(w, `{"error":{"code":"NotFound","message":"no such path"}}`)
		return
	}
	var req map[string]any
	if json.Unmarshal(body, &req) != nil {
		w.WriteHeader(400)
		io.WriteString(w, `{"error":{"code":"InvalidParameter","message":"the request body is not valid JSON"}}`)
		return
	}
	io.WriteString(w, `{"model":"doubao-seedream-4-5-251128","created":1,"data":[{"b64_json":"`+base64.StdEncoding.EncodeToString(pngBytes)+`","size":"2048x2048"}],"usage":{"generated_images":1,"output_tokens":16384,"total_tokens":16384}}`)
}

func (a *arkImages) sent(t *testing.T) []map[string]any {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []map[string]any
	for _, b := range a.bodies {
		var m map[string]any
		json.Unmarshal([]byte(b), &m)
		out = append(out, m)
	}
	return out
}

func arked(t *testing.T) (*Server, *arkImages) {
	t.Helper()
	fresh(t)
	a := &arkImages{}
	up := httptest.NewServer(a)
	t.Cleanup(up.Close)
	was := isArk
	isArk = func(p provider.Provider) bool { return p.ID == "ark" }
	t.Cleanup(func() { isArk = was })
	if err := provider.Save(provider.Provider{ID: "ark", Name: "Volcengine Ark", Chat: up.URL + "/api/v3", Key: "key", Models: []string{"doubao-seedream-4-5-251128"}}); err != nil {
		t.Fatal(err)
	}
	return New(), a
}

// Ark watermarks every image unless told not to, takes no n, and takes a
// ratio only in the prompt: uni-switch sends watermark false, asks once per
// image, and sends a ratio as the doc's size for it (JL on Discord).
func TestArkDrawsWithoutWatermarkOncePerImage(t *testing.T) {
	s, a := arked(t)
	code, ans, raw := postImages(t, s, "/v1/images/generations", "application/json", `{"model":"ark/doubao-seedream-4-5-251128","prompt":"一只喜鹊","size":"16:9","quality":"high","n":2}`)
	if code != 200 || len(ans.Data) != 2 {
		t.Fatalf("want two images: %d %s", code, raw)
	}
	sent := a.sent(t)
	if len(sent) != 2 {
		t.Fatalf("want a request per image, got %d: %v", len(sent), a.bodies)
	}
	for _, m := range sent {
		if m["watermark"] != false {
			t.Fatalf("sent without watermark false: %v", m)
		}
		if m["size"] != "2848x1600" || m["model"] != "doubao-seedream-4-5-251128" || m["prompt"] != "一只喜鹊" || m["response_format"] != "b64_json" {
			t.Fatalf("sent %v", m)
		}
		for _, k := range []string{"n", "quality"} {
			if _, ok := m[k]; ok {
				t.Fatalf("sent %s, which Ark doesn't take: %v", k, m)
			}
		}
	}
}

// An edit's images go to Ark's generations as JSON data URLs in image, not
// a multipart form to /images/edits.
func TestArkEditSendsImagesAsJSON(t *testing.T) {
	s, a := arked(t)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("model", "ark/doubao-seedream-4-5-251128")
	mw.WriteField("prompt", "make it night")
	part, _ := mw.CreateFormFile("image", "a.png")
	part.Write(pngBytes)
	mw.Close()
	code, ans, raw := postImages(t, s, "/v1/images/edits", mw.FormDataContentType(), buf.String())
	if code != 200 || len(ans.Data) != 1 {
		t.Fatalf("%d %s", code, raw)
	}
	sent := a.sent(t)
	if len(sent) != 1 || a.types[0] != "application/json" {
		t.Fatalf("sent %v as %v", a.bodies, a.types)
	}
	img, _ := sent[0]["image"].(string)
	if img != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(pngBytes) || sent[0]["watermark"] != false {
		t.Fatalf("sent %v", sent[0])
	}

	// several images go as an array
	body := `{"model":"ark/doubao-seedream-4-5-251128","prompt":"merge them","image":["https://example.com/a.png","https://example.com/b.png"]}`
	if code, _, raw := postImages(t, s, "/v1/images/edits", "application/json", body); code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	imgs, _ := a.sent(t)[1]["image"].([]any)
	if len(imgs) != 2 || imgs[0] != "https://example.com/a.png" || imgs[1] != "https://example.com/b.png" {
		t.Fatalf("sent %v", a.bodies[1])
	}
}

// A ratio Ark's doc gives no size for is turned away before Ark is asked,
// saying to put it in the prompt; a tier or WxH goes as given.
func TestArkSizes(t *testing.T) {
	s, a := arked(t)
	code, _, raw := postImages(t, s, "/v1/images/generations", "application/json", `{"model":"ark/doubao-seedream-4-5-251128","prompt":"a cat","size":"5:4"}`)
	if code != 400 || !strings.Contains(raw, "prompt") || !strings.Contains(raw, "WIDTHxHEIGHT") {
		t.Fatalf("an unknown ratio should say what to do: %d %s", code, raw)
	}
	if len(a.sent(t)) != 0 {
		t.Fatalf("asked Ark with a ratio: %v", a.bodies)
	}
	for _, c := range []struct{ size, want string }{{"2K", "2K"}, {"3750x1250", "3750x1250"}, {"1:1", "2048x2048"}, {"auto", ""}} {
		a.bodies = nil
		if code, _, raw := postImages(t, s, "/v1/images/generations", "application/json", `{"model":"ark/doubao-seedream-4-5-251128","prompt":"a cat","size":"`+c.size+`"}`); code != 200 {
			t.Fatalf("%s: %d %s", c.size, code, raw)
		}
		got, _ := a.sent(t)[0]["size"].(string)
		if got != c.want {
			t.Fatalf("size %q sent as %q, want %q", c.size, got, c.want)
		}
	}
}

// Ark is told by its host, the preset's plans and a provider added by hand
// alike; other hosts aren't.
func TestIsArk(t *testing.T) {
	for base, want := range map[string]bool{
		"https://ark.cn-beijing.volces.com/api/v3":      true,
		"https://ark.cn-beijing.volces.com/api/plan/v3": true,
		"https://ark.ap-southeast.volces.com/api/v3":    true,
		"https://open.volces.com/api/v3":                false,
		"https://api.openai.com/v1":                     false,
	} {
		if got := isArk(provider.Provider{ID: "x", Chat: base}); got != want {
			t.Errorf("%s: %v", base, got)
		}
	}
}
