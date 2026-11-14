package gateway

import (
	"encoding/base64"
	"io"
	"net/http"

	"sync"
)

// loomy is iFlytek's Loomy gateway (#4913): an OpenAI model list naming
// three image generators beside its chat models, and an images API all
// three answer in OpenAI's shape (measured live: 200 with a URL each).
type loomy struct {
	mu   sync.Mutex
	sent map[string][]string
}

func (v *loomy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	v.mu.Lock()
	if v.sent == nil {
		v.sent = map[string][]string{}
	}
	v.sent[r.URL.Path] = append(v.sent[r.URL.Path], string(body))
	v.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/v1/models":
		io.WriteString(w, `{"object":"list","data":[{"id":"Hy-Image-3.5-preview","object":"model"},{"id":"doubao-seedream-5-lite","object":"model"},{"id":"qwen-image-3.0-pro","object":"model"},{"id":"spark-lite-max","object":"model"}]}`)
	case "/v1/images/generations":
		io.WriteString(w, `{"created":1,"data":[{"b64_json":"`+base64.StdEncoding.EncodeToString(pngBytes)+`"}]}`)
	default:
		w.WriteHeader(404)
	}
}

func (v *loomy) got(path string) []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string(nil), v.sent[path]...)
}
