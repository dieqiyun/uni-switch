package gateway

import (
	"encoding/json"
	"io"
	"net/http"

	"sync"
)

// wbStudio is WorkBuddy's API as far as drawing goes: its product config,
// which lists image models by tag among its chat models, and its images
// API, which answers OpenAI's shape inside WorkBuddy's {code, msg, data}.
type wbStudio struct {
	models string // the config's models

	mu     sync.Mutex
	asked  []string // method and path
	bodies map[string]map[string]any
	heads  map[string]http.Header
	answer func(path string, body map[string]any) string
}

func (s *wbStudio) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var body map[string]any
	json.Unmarshal(b, &body)
	s.mu.Lock()
	s.asked = append(s.asked, r.Method+" "+r.URL.Path)
	if s.bodies == nil {
		s.bodies, s.heads = map[string]map[string]any{}, map[string]http.Header{}
	}
	s.bodies[r.URL.Path], s.heads[r.URL.Path] = body, r.Header.Clone()
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/v3/config" {
		io.WriteString(w, `{"code":0,"msg":"OK","data":{"agents":[{"name":"cli","models":["hy3"]}],"models":`+s.models+`}}`)
		return
	}
	io.WriteString(w, s.answer(r.URL.Path, body))
}

func (s *wbStudio) got(path string) (map[string]any, http.Header) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bodies[path], s.heads[path]
}

func (s *wbStudio) count(what string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, a := range s.asked {
		if a == what {
			n++
		}
	}
	return n
}

const wbHunyuanModels = `[{"id":"hy3","name":"Hy3","tags":[]},
	{"id":"hunyuan-image-alpha","name":"Hunyuan Image Alpha","tags":["text-to-image"]},
	{"id":"hunyuan-image-alpha-edit","name":"Hunyuan Image Alpha Edit","tags":["image-to-image"]},
	{"id":"hunyuan-image-v3.0-art","name":"Hunyuan-Image-v3.0-art","tags":["text-to-image"]}]`

const wbGPTImageModels = `[{"id":"gpt-5.5","name":"GPT-5.5"},
	{"id":"gpt-image-2.5-sunburst","name":"GPT-Image-2.5-Sunburst","tags":["text-to-image","image-to-image"]},
	{"id":"seedance-2.5","name":"Seedance-2.5","tags":["text-to-video","image-to-video"]}]`
