package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// modelScope stands in for ModelScope's API-Inference: async image tasks, one
// at a time per user, and an answer without OpenAI's "data" (#645).
type modelScope struct {
	mu      sync.Mutex
	tasks   map[string]int // polls left before it is done
	running atomic.Int32
	most    atomic.Int32
	full    atomic.Int32 // "user queue is full" answers left to give
	fail    bool
	sync    bool // answer at once, without a task
	bodies  []string
	url     string
}

func (m *modelScope) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Authorization") != "Bearer key" {
		w.WriteHeader(401)
		io.WriteString(w, `{"errors":{"message":"Invalid token"}}`)
		return
	}
	switch {
	case r.Method == "POST" && r.URL.Path == "/v1/images/generations":
		body, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		m.bodies = append(m.bodies, string(body))
		m.mu.Unlock()
		if m.sync {
			io.WriteString(w, `{"images":[{"url":"`+m.url+`/out/sync.png"}]}`)
			return
		}
		if r.Header.Get("X-ModelScope-Async-Mode") != "true" {
			// what a request without the async header met (#645): no data
			io.WriteString(w, `{"request_id":"r1"}`)
			return
		}
		if m.full.Load() > 0 {
			m.full.Add(-1)
			w.WriteHeader(500)
			io.WriteString(w, `{"errors":{"message":"enqueue failed: user queue is full"}}`)
			return
		}
		if n := m.running.Add(1); n > m.most.Load() {
			m.most.Store(n)
		}
		m.mu.Lock()
		id := fmt.Sprintf("t%d", len(m.bodies))
		m.tasks[id] = 2
		m.mu.Unlock()
		io.WriteString(w, `{"task_id":"`+id+`","request_id":"r1"}`)
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v1/tasks/"):
		if r.Header.Get("X-ModelScope-Task-Type") != "image_generation" {
			w.WriteHeader(400)
			io.WriteString(w, `{"errors":{"message":"task type missing"}}`)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/v1/tasks/")
		m.mu.Lock()
		left := m.tasks[id]
		m.tasks[id] = left - 1
		m.mu.Unlock()
		switch {
		case left > 0:
			io.WriteString(w, `{"task_status":"RUNNING"}`)
		case m.fail:
			m.running.Add(-1)
			io.WriteString(w, `{"task_status":"FAILED","errors":{"message":"content moderation failed"}}`)
		default:
			m.running.Add(-1)
			io.WriteString(w, `{"task_status":"SUCCEED","output_images":["`+m.url+`/out/`+id+`.png"]}`)
		}
	case strings.HasPrefix(r.URL.Path, "/out/"):
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes)
	default:
		w.WriteHeader(404)
	}
}

func modelScoped(t *testing.T) (*Server, *modelScope) {
	t.Helper()
	fresh(t)
	m := &modelScope{tasks: map[string]int{}}
	up := httptest.NewServer(m)
	t.Cleanup(up.Close)
	m.url = up.URL
	was, poll, busy := isModelScope, modelScopePoll, modelScopeBusy
	isModelScope = func(p provider.Provider) bool { return p.ID == "ms" }
	modelScopePoll, modelScopeBusy = 5*time.Millisecond, []time.Duration{5 * time.Millisecond, 5 * time.Millisecond}
	t.Cleanup(func() { isModelScope, modelScopePoll, modelScopeBusy = was, poll, busy })
	if err := provider.Save(provider.Provider{ID: "ms", Name: "ModelScope", Chat: up.URL + "/v1", Key: "key", Models: []string{"Qwen/Qwen-Image-2.1"}}); err != nil {
		t.Fatal(err)
	}
	return New(), m
}

type urlAnswer struct {
	Data []struct {
		URL string `json:"url"`
	} `json:"data"`
}

func TestModelScopeDraws(t *testing.T) {
	s, m := modelScoped(t)
	code, _, raw := postImages(t, s, "/v1/images/generations", "application/json", `{"model":"ms/Qwen/Qwen-Image-2.1","prompt":"A golden cat","size":"1024x1024","quality":"high","n":2}`)
	if code != 200 {
		t.Fatalf("code %d: %s", code, raw)
	}
	var a urlAnswer
	json.Unmarshal([]byte(raw), &a)
	if len(a.Data) != 2 || !strings.HasSuffix(a.Data[0].URL, ".png") || a.Data[0].URL == a.Data[1].URL {
		t.Fatalf("want two task images, got %s", raw)
	}
	var sent map[string]any
	json.Unmarshal([]byte(m.bodies[0]), &sent)
	if sent["model"] != "Qwen/Qwen-Image-2.1" || sent["prompt"] != "A golden cat" || sent["size"] != "1024x1024" {
		t.Fatalf("sent %s", m.bodies[0])
	}
	for _, k := range []string{"n", "quality"} {
		if _, ok := sent[k]; ok {
			t.Fatalf("sent %s, which ModelScope doesn't take: %s", k, m.bodies[0])
		}
	}
	if m.most.Load() != 1 {
		t.Fatalf("%d tasks ran at once; ModelScope takes one", m.most.Load())
	}
}

// Draws asked at once wait their turn rather than meet a full queue.
func TestModelScopeOneAtATime(t *testing.T) {
	s, m := modelScoped(t)
	var wg sync.WaitGroup
	codes := make([]int, 3)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _, _ = postImages(t, s, "/v1/images/generations", "application/json", `{"model":"ms/Qwen/Qwen-Image-2.1","prompt":"a cat"}`)
		}()
	}
	wg.Wait()
	for i, c := range codes {
		if c != 200 {
			t.Fatalf("draw %d: %d", i, c)
		}
	}
	if m.most.Load() != 1 {
		t.Fatalf("%d tasks ran at once", m.most.Load())
	}
}

// A queue another client filled is waited out.
func TestModelScopeQueueFull(t *testing.T) {
	s, m := modelScoped(t)
	m.full.Store(2)
	if code, _, raw := postImages(t, s, "/v1/images/generations", "application/json", `{"model":"ms/Qwen/Qwen-Image-2.1","prompt":"a cat"}`); code != 200 {
		t.Fatalf("code %d: %s", code, raw)
	}
	m.full.Store(10)
	code, _, raw := postImages(t, s, "/v1/images/generations", "application/json", `{"model":"ms/Qwen/Qwen-Image-2.1","prompt":"a cat"}`)
	if code != 500 || !strings.Contains(raw, "user queue is full") {
		t.Fatalf("a queue that stays full should say so: %d %s", code, raw)
	}
}

// What went wrong is what ModelScope said, not "drew nothing".
func TestModelScopeSaysWhy(t *testing.T) {
	s, m := modelScoped(t)
	m.fail = true
	code, _, raw := postImages(t, s, "/v1/images/generations", "application/json", `{"model":"ms/Qwen/Qwen-Image-2.1","prompt":"a cat"}`)
	if code != 502 || !strings.Contains(raw, "content moderation failed") || strings.Contains(raw, "drew nothing") {
		t.Fatalf("%d %s", code, raw)
	}
	m.fail, m.sync = false, true
	code, _, raw = postImages(t, s, "/v1/images/generations", "application/json", `{"model":"ms/Qwen/Qwen-Image-2.1","prompt":"a cat"}`)
	var a urlAnswer
	json.Unmarshal([]byte(raw), &a)
	if code != 200 || len(a.Data) != 1 || !strings.HasSuffix(a.Data[0].URL, "/out/sync.png") {
		t.Fatalf("a sync answer's image: %d %s", code, raw)
	}
}

// Any images API that answers without an image says what it answered.
func TestImagesAPINoImageSaysWhy(t *testing.T) {
	fresh(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"errors":{"message":"model not supported for images"}}`)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "art", Name: "Art", Chat: up.URL + "/v1", Key: "key", Models: []string{"gpt-image-1"}}); err != nil {
		t.Fatal(err)
	}
	code, _, raw := postImages(t, New(), "/v1/images/generations", "application/json", `{"model":"art/gpt-image-1","prompt":"a cat"}`)
	if code != 502 || !strings.Contains(raw, "model not supported for images") {
		t.Fatalf("%d %s", code, raw)
	}
}
