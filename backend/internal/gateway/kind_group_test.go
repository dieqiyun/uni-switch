package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// kindVendor is an OpenAI-style vendor whose list has a chat model and one
// model of each other API, and that serves each of those APIs; busy
// answers every one of them 429, out of quota. It counts what it is asked.
type kindVendor struct {
	*httptest.Server
	busy  bool
	mu    sync.Mutex
	asked []string
}

func newKindVendor(t *testing.T, busy bool) *kindVendor {
	t.Helper()
	v := &kindVendor{busy: busy}
	v.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		v.mu.Lock()
		v.asked = append(v.asked, r.Method+" "+r.URL.Path)
		v.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" && r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"object":"list","data":[`+
				`{"id":"chat-model","object":"model"},`+
				`{"id":"embed-1","object":"model"},`+
				`{"id":"bge-reranker-1","object":"model"},`+
				`{"id":"tts-1","object":"model"},`+
				`{"id":"whisper-1","object":"model"},`+
				`{"id":"omni-moderation-latest","object":"model"}]}`)
			return
		}
		if v.busy {
			w.WriteHeader(429)
			io.WriteString(w, `{"error":{"message":"quota exceeded"}}`)
			return
		}
		switch r.URL.Path {
		case "/v1/embeddings":
			io.WriteString(w, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.5,0.25]}],"model":"embed-1","usage":{"prompt_tokens":1,"total_tokens":1}}`)
		case "/v1/rerank":
			io.WriteString(w, `{"results":[{"index":1,"relevance_score":0.75}]}`)
		case "/v1/audio/speech":
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Write([]byte("ID3 kind speech"))
		case "/v1/audio/transcriptions":
			io.WriteString(w, `{"text":"heard by kind"}`)
		default:
			w.WriteHeader(404)
			io.WriteString(w, `{"error":{"message":"no route `+r.URL.Path+` `+string(b)+`"}}`)
		}
	}))
	t.Cleanup(v.Close)
	return v
}

func (v *kindVendor) log() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string(nil), v.asked...)
}

// A video model two providers list under one id is a group by that id on
// /v1/videos: the first that can't make it passes it to the next. Before,
// the bare id went to the first provider listing it and ended there.
func TestVideoKindGroupByTheModelsID(t *testing.T) {
	fresh(t)
	a, b := newOpenAIVideos(t), newOpenAIVideos(t)
	a.lacks = "sk-a"
	saveFetched(t, provider.Provider{ID: "a", Name: "A", Chat: a.URL + "/v1", Key: "sk-a", Models: []string{"gpt-5.5"}})
	saveFetched(t, provider.Provider{ID: "b", Name: "B", Chat: b.URL + "/v1", Key: "sk-b", Models: []string{"gpt-5.5"}})
	code, obj, raw := asKey(t, New().Handler(), "", "POST", "/v1/videos", `{"model":"vid-1","prompt":"a cat on a fence"}`)
	if code != 200 || obj["model"] != "b/vid-1" {
		t.Fatalf("video: %d %s", code, raw)
	}
	if got := strings.Join(a.log(), ","); !strings.Contains(got, "POST /v1/videos sk-a") {
		t.Fatalf("the first provider was never asked: %v", a.log())
	}
}
