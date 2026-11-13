package gateway

import (
	"encoding/base64"
	"encoding/json"
	"io"

	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// grokMedia is the backend a Grok subscription's images and videos are
// asked at: what each path was sent is kept, a video takes two asks to be
// made, and a request id says how it ends.
type grokMedia struct {
	*httptest.Server
	mu    sync.Mutex
	paths []string
	sent  map[string][]string
	asked map[string]int
}

func newGrokMedia(t *testing.T) *grokMedia {
	t.Helper()
	g := &grokMedia{sent: map[string][]string{}, asked: map[string]int{}}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		g.mu.Lock()
		defer g.mu.Unlock()
		g.paths = append(g.paths, r.Method+" "+r.URL.Path)
		g.sent[r.URL.Path] = append(g.sent[r.URL.Path], string(b))
		w.Header().Set("Content-Type", "application/json")
		say := func(code int, body string) { w.WriteHeader(code); io.WriteString(w, body) }
		png := base64.StdEncoding.EncodeToString(pngBytes)
		switch {
		case r.URL.Path == "/v1/images/generations" && strings.Contains(string(b), "RATELIMITED"):
			say(429, `{"code":"resource-exhausted","error":"You have reached your image generation limit for this period."}`)
		case r.URL.Path == "/v1/images/generations" && strings.Contains(string(b), "REFUSED"):
			say(400, `{"code":"invalid-argument","error":"Generated image rejected by content moderation."}`)
		case r.URL.Path == "/v1/images/generations" && strings.Contains(string(b), "BROKEN"):
			say(500, `upstream exploded`)
		case r.URL.Path == "/v1/images/generations" && strings.Contains(string(b), `"n":2`):
			say(200, `{"data":[{"b64_json":"`+png+`"},{"b64_json":"`+png+`"}]}`)
		case r.URL.Path == "/v1/images/generations" || r.URL.Path == "/v1/images/edits":
			say(200, `{"data":[{"b64_json":"`+png+`"}]}`)
		case r.URL.Path == "/v1/videos/generations" && strings.Contains(string(b), "TOOLONG"):
			say(400, `{"code":"invalid-argument","error":"Duration must be between 1 and 15 seconds"}`)
		case r.URL.Path == "/v1/videos/generations" && strings.Contains(string(b), "FAILING"):
			say(200, `{"request_id":"req-fail"}`)
		case r.URL.Path == "/v1/videos/generations" && strings.Contains(string(b), "EXPIRING"):
			say(200, `{"request_id":"req-expired"}`)
		case r.URL.Path == "/v1/videos/generations" && strings.Contains(string(b), "NOID"):
			say(200, `{}`)
		case r.URL.Path == "/v1/videos/generations":
			say(200, `{"request_id":"req-1"}`)
		case r.URL.Path == "/v1/videos/req-1":
			g.asked["req-1"]++
			switch g.asked["req-1"] {
			case 1:
				say(202, `{"status":"pending","progress":0}`)
			case 2:
				say(202, `{"status":"pending","progress":40}`)
			default:
				say(200, `{"status":"done","progress":100,"model":"grok-imagine-video","video":{"url":"`+g.URL+`/bucket/req-1.mp4","duration":6,"respect_moderation":true},"usage":{"cost_in_usd_ticks":3000000000}}`)
			}
		case r.URL.Path == "/v1/videos/req-fail":
			say(200, `{"status":"failed","error":{"code":"invalid_argument","message":"image_url must either be a base64-encoded image or a URL."}}`)
		case r.URL.Path == "/v1/videos/req-expired":
			say(200, `{"status":"expired"}`)
		case r.URL.Path == "/bucket/req-1.mp4":
			w.Header().Set("Content-Type", "binary/octet-stream")
			io.WriteString(w, "fake-mp4-bytes")
		default:
			say(404, `{"code":"not-found","error":"no such path `+r.URL.Path+`"}`)
		}
	}))
	t.Cleanup(g.Close)
	was := provider.GrokBase
	provider.GrokBase = g.URL + "/v1"
	t.Cleanup(func() { provider.GrokBase = was })
	return g
}

func (g *grokMedia) body(path string, i int) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if i >= len(g.sent[path]) {
		return ""
	}
	return g.sent[path][i]
}

func request(t *testing.T, s *Server, method, path, ct, body string) (int, map[string]any, []byte, http.Header) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	s.Handler().ServeHTTP(rec, req)
	var obj map[string]any
	json.Unmarshal(rec.Body.Bytes(), &obj)
	return rec.Code, obj, rec.Body.Bytes(), rec.Result().Header
}

func errorOf(obj map[string]any) string {
	if e, ok := obj["error"].(map[string]any); ok {
		m, _ := e["message"].(string)
		return m
	}
	return ""
}

func TestVolcengineAgentPlanVideos(t *testing.T) {
	p, err := provider.FromPreset("volcengine")
	if err != nil {
		t.Fatal(err)
	}
	pr := provider.Preset("volcengine")
	p.Chat, p.Responses, p.Anthropic = pr.Regions[1].Chat, pr.Regions[1].Responses, pr.Regions[1].Anthropic
	ms, want := Videomakers(p), p.PlanVideos()
	if len(ms) == 0 || !slices.EqualFunc(ms, want, func(a, b catalog.Model) bool { return a.ID == b.ID }) {
		t.Fatalf("Agent Plan videos: %+v, want preset plan's %+v", ms, want)
	}
	p.Chat, p.Responses, p.Anthropic = pr.Regions[0].Chat, pr.Regions[0].Responses, pr.Regions[0].Anthropic
	if ms := Videomakers(p); len(ms) != 0 {
		t.Fatalf("Coding Plan videos: %+v", ms)
	}
}

func TestVolcengineVideoBody(t *testing.T) {
	f := filming{Prompt: "a uni-switch", Seconds: "6", Size: "1280x720", Start: &picture{Mime: "image/png", Data: []byte("png")}, References: []picture{{URL: "https://example.test/ref.png"}}}
	var got map[string]any
	if err := json.Unmarshal(volcengineVideoBody("doubao-seedance-2.5", f), &got); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "doubao-seedance-2.5" || got["duration"] != float64(6) || got["resolution"] != "720p" || got["ratio"] != "16:9" {
		t.Fatalf("body: %+v", got)
	}
	content, _ := got["content"].([]any)
	if len(content) != 3 {
		t.Fatalf("content: %+v", got["content"])
	}
	first, _ := content[1].(map[string]any)
	reference, _ := content[2].(map[string]any)
	if first["role"] != "first_frame" || reference["role"] != "reference_image" {
		t.Fatalf("image roles: %+v", content)
	}
}

func TestVolcengineVideoIsStartedPolledAndFetched(t *testing.T) {
	fresh(t)
	var mu sync.Mutex
	var paths, bodies []string
	asked := map[string]int{}
	var up *httptest.Server
	up = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		bodies = append(bodies, string(body))
		if r.URL.Path != "/ark/video.mp4" && r.Header.Get("Authorization") != "Bearer key" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/plan/v3/contents/generations/tasks" && strings.Contains(string(body), "FAILING"):
			io.WriteString(w, volcengineVideoFailedCreateResponse)
		case r.Method == http.MethodPost && r.URL.Path == "/api/plan/v3/contents/generations/tasks":
			io.WriteString(w, volcengineVideoCreateResponse)
		case r.Method == http.MethodGet && r.URL.Path == "/api/plan/v3/contents/generations/tasks/cgt-20261008165954-4sc9n":
			asked["success"]++
			if asked["success"] == 1 {
				io.WriteString(w, volcengineVideoRunningResponse)
			} else {
				io.WriteString(w, strings.Replace(volcengineVideoSucceededResponse, "VIDEO_URL", up.URL+"/ark/video.mp4", 1))
			}
		case r.Method == http.MethodGet && r.URL.Path == "/api/plan/v3/contents/generations/tasks/cgt-20261008165955-bgckq":
			io.WriteString(w, volcengineVideoFailedResponse)
		case r.Method == http.MethodGet && r.URL.Path == "/ark/video.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			io.WriteString(w, "ark-video-bytes")
		default:
			w.WriteHeader(404)
			io.WriteString(w, `{"error":{"code":"not_found","message":"unexpected path"}}`)
		}
	}))
	t.Cleanup(up.Close)

	p, err := provider.FromPreset("volcengine")
	if err != nil {
		t.Fatal(err)
	}
	p.ID, p.Key = "ark-agent", "key"
	p.Chat, p.Responses = up.URL+"/api/plan/v3", up.URL+"/api/plan/v3"
	p.Anthropic = up.URL + "/api/plan"
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}

	s := New()
	code, obj, raw, _ := request(t, s, http.MethodPost, "/v1/videos", "application/json", `{"model":"ark-agent/doubao-seedance-2.0-mini","prompt":"a uni-switch","seconds":5,"size":"854x480"}`)
	if code != 200 || obj["status"] != "queued" || obj["model"] != "ark-agent/doubao-seedance-2.0-mini" {
		t.Fatalf("create: %d %s", code, raw)
	}
	id, _ := obj["id"].(string)
	code, obj, raw, _ = request(t, s, http.MethodGet, "/v1/videos/"+id, "", "")
	if code != 200 || obj["status"] != "in_progress" {
		t.Fatalf("running: %d %s", code, raw)
	}
	code, obj, raw, _ = request(t, s, http.MethodGet, "/v1/videos/"+id, "", "")
	if code != 200 || obj["status"] != "completed" || obj["seconds"] != "5" {
		t.Fatalf("succeeded: %d %s", code, raw)
	}
	code, _, raw, head := request(t, s, http.MethodGet, "/v1/videos/"+id+"/content", "", "")
	if code != 200 || string(raw) != "ark-video-bytes" || head.Get("Content-Type") != "video/mp4" {
		t.Fatalf("content: %d %q %v", code, raw, head)
	}

	code, failed, raw, _ := request(t, s, http.MethodPost, "/v1/videos", "application/json", `{"model":"ark-agent/doubao-seedance-2.0-mini","prompt":"FAILING"}`)
	if code != 200 {
		t.Fatalf("create failing: %d %s", code, raw)
	}
	code, failed, raw, _ = request(t, s, http.MethodGet, "/v1/videos/"+failed["id"].(string), "", "")
	errObj, _ := failed["error"].(map[string]any)
	if code != 200 || failed["status"] != "failed" || errObj["code"] != "OutputVideoSensitiveContentDetected.PolicyViolation" || !strings.Contains(errObj["message"].(string), "copyright restrictions") {
		t.Fatalf("failed: %d %s", code, raw)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(paths) < 7 || paths[0] != "POST /api/plan/v3/contents/generations/tasks" || !strings.Contains(bodies[0], `"content":[`) || !strings.Contains(bodies[0], `"duration":5`) || !slices.Contains(paths, "GET /ark/video.mp4") {
		t.Fatalf("upstream paths %v, create body %s", paths, bodies[0])
	}
}

func TestVolcengineVideoState(t *testing.T) {
	var st videoState
	if err := json.Unmarshal([]byte(`{"status":"succeeded","model":"doubao-seedance-2.5","content":{"video_url":"https://example.test/video.mp4"},"duration":5}`), &st); err != nil {
		t.Fatal(err)
	}
	obj := videoObject("video_volcengine.task.1.0", "volcengine/"+st.Model, time.Unix(1, 0), st, filming{})
	if obj["status"] != "completed" || obj["progress"] != 100 || obj["seconds"] != "5" || st.Content.VideoURL != "https://example.test/video.mp4" {
		t.Fatalf("completed state: %+v, object: %+v", st, obj)
	}
	for status, want := range map[string]string{"queued": "queued", "running": "in_progress", "cancelled": "failed"} {
		st = videoState{Status: status}
		if got := videoObject("id", "model", time.Unix(1, 0), st, filming{})["status"]; got != want {
			t.Errorf("%s became %v, want %s", status, got, want)
		}
	}
}

func TestVideoIDsRoundTrip(t *testing.T) {
	id := videoID(provider.Provider{ID: "grok"}, "0ab-12", time.Unix(1790000000, 0))
	if id != "video_grok.0ab-12.1790000000.0" {
		t.Fatalf("id %q", id)
	}
	if pid, vid, at, by, ok := parseVideoID(id); !ok || pid != "grok" || vid != "0ab-12" || at.Unix() != 1790000000 || by != "0" {
		t.Fatalf("parsed %q %q %v %q %v", pid, vid, at, by, ok)
	}
	for _, bad := range []string{"", "video_", "video_grok", "video_grok.x", "video_grok.x.y", "video_grok.x.1", "video_.x.1.0", "video_grok..1.0", "grok.x.1.0", "video_grok.x.1.2.3",
		"video_grok.x?a=1.1.0", "video_grok.x/y.1.0", "video_grok.x y.1.0", "video_grok.%2e%2e.1.0", "video_grok.x#.1.0", "video_gr/ok.x.1.0"} {
		if _, _, _, _, ok := parseVideoID(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestVideoResolutionAndAspect(t *testing.T) {
	for size, want := range map[string]string{"1280x720": "720p", "720x1280": "720p", "1920x1080": "1080p", "854x480": "480p", "640x640": "480p", "1024x1024": "720p", "16:9": "", "": "", "junk": "", "0x5": ""} {
		if got := videoResolution("grok-imagine-video-1.5", size); got != want {
			t.Errorf("videoResolution(1.5, %q) = %q, want %q", size, got, want)
		}
	}
	// grok-imagine-video stops at 720p: the vendor turns 1080p away for it
	for size, want := range map[string]string{"1920x1080": "720p", "1080x1920": "720p", "1280x720": "720p", "854x480": "480p", "16:9": ""} {
		if got := videoResolution("grok-imagine-video", size); got != want {
			t.Errorf("videoResolution(base, %q) = %q, want %q", size, got, want)
		}
	}
	for size, want := range map[string]string{"1280x720": "16:9", "720x1280": "9:16", "1024x1024": "1:1", "1600x1200": "4:3", "9:16": "9:16"} {
		if got := aspectAmong(size, grokVideoAspects); got != want {
			t.Errorf("aspect(%q) = %q, want %q", size, got, want)
		}
	}
}

func TestVideoNeedsAMaker(t *testing.T) {
	codexSignedIn(t) // a ChatGPT account makes no videos, and there is no Grok one
	s := New()
	code, obj, raw, _ := request(t, s, "POST", "/v1/videos", "application/json", `{"prompt":"a kite"}`)
	if code != 400 || !strings.Contains(errorOf(obj), "no model to make videos with") {
		t.Fatalf("%d %s", code, raw)
	}
}

func mustFind(t *testing.T, id string) provider.Provider {
	t.Helper()
	p, err := provider.Find(id)
	if err != nil {
		t.Fatal(err)
	}
	return *p
}
