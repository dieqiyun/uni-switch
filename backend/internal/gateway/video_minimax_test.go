package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// MiniMax's answers as its docs give them (platform.minimax.io/docs/
// api-reference/video-generation-v2-create and -query, 2026-10-29): the
// create's task_id, the query's succeeded and failed tasks, and its errors.
const (
	minimaxVideoCreateResponse    = `{"task_id":"424010985738629"}`
	minimaxVideoSucceededResponse = `{"task":{"id":"424010985738629","model":"MiniMax-H3","status":"succeeded","created_at":1785125529,"updated_at":1785125946,"content":{"url":"VIDEO_URL"},"resolution":"2K","duration":5,"usage":{"total_seconds":5,"input_seconds":0,"output_seconds":5,"input_image_count":0},"ratio":"16:9","task_type":"generation","modality":"video"}}`
	minimaxVideoFailedResponse    = `{"task":{"id":"424010985738630","model":"MiniMax-H3","status":"failed","error":{"code":"1026","message":"video description contains sensitive content"},"created_at":1785125529,"updated_at":1785125700,"resolution":"2K","duration":5,"usage":{},"ratio":"16:9","task_type":"generation","modality":"video"}}`
	minimax401                    = `{"type":"error","error":{"type":"authorized_error","message":"login fail: Please carry the API secret key in the 'Authorization' field of the request header (1004)","http_code":"401"},"request_id":"021785229015510a2c883cf675b9804d"}`
	minimax402                    = `{"type":"error","error":{"type":"insufficient_balance_error","message":"insufficient balance (1008)","http_code":"402"},"request_id":"021785229015510a2c883cf675b9804d"}`
	minimax422                    = `{"type":"error","error":{"type":"unprocessable_entity_error","message":"video description contains sensitive content (1026)","http_code":"422"},"request_id":"021785229015510a2c883cf675b9804d"}`
)

// the succeeded task while it is still being made: the same task, queued
// or running, with no video yet
func minimaxPending(status string) string {
	s := strings.Replace(minimaxVideoSucceededResponse, `"status":"succeeded"`, `"status":"`+status+`"`, 1)
	return strings.Replace(s, `"content":{"url":"VIDEO_URL"},`, "", 1)
}

// fakeMiniMax is MiniMax's video API on a test server: the create answers
// by what the prompt says, the task is queued, then running, then made
// (or stays queued: STUCK), and the video is at its own path.
type fakeMiniMax struct {
	*httptest.Server
	mu     sync.Mutex
	paths  []string
	bodies []string
	auth   []string
	polls  int
}

func newFakeMiniMax(t *testing.T) *fakeMiniMax {
	f := &fakeMiniMax{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.paths = append(f.paths, r.Method+" "+r.URL.Path)
		f.bodies = append(f.bodies, string(body))
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		if r.URL.Path == "/cdn/output.mp4" {
			w.Header().Set("Content-Type", "video/mp4")
			io.WriteString(w, "minimax-mp4-bytes")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") == "Bearer bad-key" {
			w.WriteHeader(401)
			io.WriteString(w, minimax401)
			return
		}
		if r.Header.Get("Authorization") != "Bearer key" && r.Header.Get("Authorization") != "Bearer second-key" {
			w.WriteHeader(401)
			io.WriteString(w, minimax401)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v2/video_generation":
			switch {
			case strings.Contains(string(body), "BROKE"):
				w.WriteHeader(402)
				io.WriteString(w, minimax402)
			case strings.Contains(string(body), "SENSITIVE"):
				w.WriteHeader(422)
				io.WriteString(w, minimax422)
			case strings.Contains(string(body), "FAILING"):
				io.WriteString(w, `{"task_id":"424010985738630"}`)
			case strings.Contains(string(body), "STUCK"):
				io.WriteString(w, `{"task_id":"424010985738699"}`)
			default:
				io.WriteString(w, minimaxVideoCreateResponse)
			}
		case r.Method == http.MethodGet && r.URL.Path == "/v2/query/video_generation/424010985738629":
			f.polls++
			switch f.polls {
			case 1:
				io.WriteString(w, minimaxPending("queued"))
			case 2:
				io.WriteString(w, minimaxPending("running"))
			default:
				io.WriteString(w, strings.Replace(minimaxVideoSucceededResponse, "VIDEO_URL", f.URL+"/cdn/output.mp4", 1))
			}
		case r.Method == http.MethodGet && r.URL.Path == "/v2/query/video_generation/424010985738630":
			io.WriteString(w, minimaxVideoFailedResponse)
		case r.Method == http.MethodGet && r.URL.Path == "/v2/query/video_generation/424010985738699":
			io.WriteString(w, strings.Replace(minimaxPending("queued"), "424010985738629", "424010985738699", 1))
		default:
			w.WriteHeader(404)
			io.WriteString(w, `{"type":"error","error":{"type":"not_found","message":"unexpected path"}}`)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeMiniMax) seen() (paths, bodies, auth []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...), append([]string(nil), f.bodies...), append([]string(nil), f.auth...)
}

// minimaxProvider saves a MiniMax API key from preset (minimax or
// minimax-cn) pointed at the fake, which stands in for MiniMax's host.
func minimaxProvider(t *testing.T, preset, id string, up *fakeMiniMax) provider.Provider {
	t.Helper()
	was := isMiniMaxHost
	t.Cleanup(func() { isMiniMaxHost = was })
	host := strings.TrimPrefix(up.URL, "http://")
	isMiniMaxHost = func(h string) bool { return h == host || was(h) }
	p, err := provider.FromPreset(preset)
	if err != nil {
		t.Fatal(err)
	}
	p.ID, p.Key = id, "key"
	p.Chat, p.Anthropic = up.URL+"/v1", up.URL+"/anthropic"
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	return p
}

// A MiniMax API key makes videos with H3, intl and CN alike, at its own
// host; none of the H3 models is a chat model.
func TestMiniMaxKeysMakeH3Videos(t *testing.T) {
	fresh(t)
	for _, preset := range []string{"minimax", "minimax-cn"} {
		p, err := provider.FromPreset(preset)
		if err != nil {
			t.Fatal(err)
		}
		p.ID, p.Key = preset, "key"
		var ids []string
		for _, m := range Videomakers(p) {
			ids = append(ids, m.ID)
			if !m.Films || m.Provider != preset {
				t.Fatalf("%s: %+v isn't a video model of its", preset, m)
			}
		}
		if strings.Join(ids, ",") != "MiniMax-H3,MiniMax-H3-Highspeed,MiniMax-H3-Lite,MiniMax-H3-Max" {
			t.Fatalf("%s makes videos with %v", preset, ids)
		}
		if len(catalog.Chat(Videomakers(p))) != 0 {
			t.Fatalf("%s: H3 is in the chat list", preset)
		}
		if len(customVideomakers(p)) != 0 {
			t.Fatalf("%s is asked at an OpenAI videos API", preset)
		}
	}
	if h := minimaxHost(provider.Provider{Chat: "https://api.minimax.io/v1"}); h != "https://api.minimax.io" {
		t.Fatalf("intl host %q", h)
	}
	if h := minimaxHost(provider.Provider{Anthropic: "https://api.minimaxi.com/anthropic"}); h != "https://api.minimaxi.com" {
		t.Fatalf("CN host %q", h)
	}
	if h := minimaxHost(provider.Provider{Chat: "https://api.minimax.cn/v1"}); h != "https://api.minimax.cn" {
		t.Fatalf("CN docs host %q", h)
	}
	// another vendor, a relay serving MiniMax's models, a sign-in: none
	for _, p := range []provider.Provider{
		{ID: "x", Chat: "https://api.deepseek.com/v1", Key: "k"},
		{ID: "relay", Chat: "https://relay.example.com/minimax/v1", Key: "k"},
	} {
		if filmsMiniMax(p) || len(Videomakers(p)) != 0 {
			t.Fatalf("%s makes MiniMax videos", p.ID)
		}
	}
}

// The H3 models are listed as video models only: /v1/models gives them
// "kind": "video" when asked for videomakers, and never as chat models.
func TestMiniMaxH3IsNoChatModel(t *testing.T) {
	fresh(t)
	up := newFakeMiniMax(t)
	minimaxProvider(t, "minimax", "minimax", up)
	s := New()
	_, obj, raw, _ := request(t, s, http.MethodGet, "/v1/models", "", "")
	if strings.Contains(string(raw), "MiniMax-H3") {
		t.Fatalf("chat list has H3: %s", raw)
	}
	_ = obj
	got := 0
	for _, m := range videomakerObjects(httptest.NewRequest(http.MethodGet, "/v1/models", nil)) {
		if strings.HasPrefix(m["id"].(string), "minimax/MiniMax-H3") && m["kind"] == "video" {
			got++
		}
	}
	if got != 4 {
		t.Fatalf("%d H3 video models listed", got)
	}
}

// A video is started at MiniMax's create, asked after at its query until
// it is made, and its MP4 fetched; the request's prompt, duration,
// resolution, ratio and images reach MiniMax as its API takes them.
func TestMiniMaxVideoIsStartedPolledAndFetched(t *testing.T) {
	fresh(t)
	up := newFakeMiniMax(t)
	minimaxProvider(t, "minimax", "minimax", up)
	s := New()
	code, obj, raw, _ := request(t, s, http.MethodPost, "/v1/videos", "application/json", `{"model":"minimax/MiniMax-H3-Lite","prompt":"a uni-switch lands on a branch","seconds":6,"size":"854x480"}`)
	if code != 200 || obj["status"] != "queued" || obj["model"] != "minimax/MiniMax-H3-Lite" {
		t.Fatalf("create: %d %s", code, raw)
	}
	id, _ := obj["id"].(string)
	if !strings.HasPrefix(id, "video_minimax.424010985738629.") || !strings.HasSuffix(id, "."+keyMark(provider.Provider{Key: "key"})) {
		t.Fatalf("id %q", id)
	}
	for _, want := range []string{"queued", "in_progress"} {
		code, obj, raw, _ = request(t, s, http.MethodGet, "/v1/videos/"+id, "", "")
		if code != 200 || obj["status"] != want {
			t.Fatalf("want %s: %d %s", want, code, raw)
		}
	}
	code, obj, raw, _ = request(t, s, http.MethodGet, "/v1/videos/"+id, "", "")
	if code != 200 || obj["status"] != "completed" || obj["seconds"] != "5" || obj["model"] != "minimax/MiniMax-H3" {
		t.Fatalf("succeeded: %d %s", code, raw)
	}
	code, _, raw, head := request(t, s, http.MethodGet, "/v1/videos/"+id+"/content", "", "")
	if code != 200 || string(raw) != "minimax-mp4-bytes" || head.Get("Content-Type") != "video/mp4" {
		t.Fatalf("content: %d %q %v", code, raw, head)
	}
	paths, bodies, auth := up.seen()
	if paths[0] != "POST /v2/video_generation" || auth[0] != "Bearer key" || paths[1] != "GET /v2/query/video_generation/424010985738629" || paths[len(paths)-1] != "GET /cdn/output.mp4" {
		t.Fatalf("paths %v auth %v", paths, auth)
	}
	var sent map[string]any
	json.Unmarshal([]byte(bodies[0]), &sent)
	content, _ := sent["content"].([]any)
	first, _ := content[0].(map[string]any)
	if sent["model"] != "MiniMax-H3-Lite" || sent["duration"] != float64(6) || sent["resolution"] != "480P" || sent["ratio"] != "16:9" || len(content) != 1 || first["type"] != "text" || first["text"] != "a uni-switch lands on a branch" {
		t.Fatalf("create body %s", bodies[0])
	}
	if _, ok := sent["callback_url"]; ok {
		t.Fatalf("a callback was sent: %s", bodies[0])
	}
}

// The first and last frames go as image_url items with their roles, and
// no ratio (the video takes the image's); a duration under the model's
// shortest is raised to it, and a resolution it doesn't make is its
// nearest.
func TestMiniMaxVideoBody(t *testing.T) {
	png := picture{Mime: "image/png", Data: []byte("png")}
	end := picture{URL: "https://example.com/end.png"}
	b, err := minimaxVideoBody("MiniMax-H3-Max", filming{Prompt: "p", Seconds: "2", Size: "1920x1080", Start: &png, End: &end})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Content []struct {
			Type     string            `json:"type"`
			ImageURL map[string]string `json:"image_url"`
			Role     string            `json:"role"`
		} `json:"content"`
		Duration   int    `json:"duration"`
		Resolution string `json:"resolution"`
		Ratio      *string
	}
	json.Unmarshal(b, &got)
	if len(got.Content) != 3 || got.Content[1].Role != "first_frame" || got.Content[1].ImageURL["url"] != "data:image/png;base64,cG5n" || got.Content[2].Role != "last_frame" || got.Content[2].ImageURL["url"] != "https://example.com/end.png" {
		t.Fatalf("frames: %s", b)
	}
	if got.Duration != 5 || got.Resolution != "768P" || strings.Contains(string(b), `"ratio"`) {
		t.Fatalf("Max at 2s 1080p: %s", b)
	}
	// H3 makes 2K; past 15 seconds is 15; a ratio it doesn't take is its nearest
	b, _ = minimaxVideoBody("MiniMax-H3", filming{Prompt: "p", Seconds: "40", Size: "1080x1920"})
	if !strings.Contains(string(b), `"resolution":"2K"`) || !strings.Contains(string(b), `"duration":15`) || !strings.Contains(string(b), `"ratio":"9:16"`) {
		t.Fatalf("H3 1080x1920: %s", b)
	}
	b, _ = minimaxVideoBody("MiniMax-H3", filming{Prompt: "p", Size: "3:2"})
	if !strings.Contains(string(b), `"ratio":"4:3"`) && !strings.Contains(string(b), `"ratio":"16:9"`) || !strings.Contains(string(b), `"duration":5`) || !strings.Contains(string(b), `"resolution":"768P"`) {
		t.Fatalf("H3 3:2: %s", b)
	}
	// references: adaptive (none sent) except Lite, which needs one
	b, _ = minimaxVideoBody("MiniMax-H3", filming{Prompt: "p", References: []picture{png}})
	if strings.Contains(string(b), `"ratio"`) || !strings.Contains(string(b), `"role":"reference_image"`) {
		t.Fatalf("H3 references: %s", b)
	}
	b, _ = minimaxVideoBody("MiniMax-H3-Lite", filming{Prompt: "p", References: []picture{png}})
	if !strings.Contains(string(b), `"ratio":"16:9"`) {
		t.Fatalf("Lite references: %s", b)
	}
	// the caller's own resolution goes as said
	b, _ = minimaxVideoBody("MiniMax-H3-Lite", filming{Prompt: "p", Extra: map[string]any{"resolution": "480p"}})
	if !strings.Contains(string(b), `"resolution":"480P"`) {
		t.Fatalf("own resolution: %s", b)
	}
	if _, err := minimaxVideoBody("MiniMax-H3", filming{Prompt: "p", Start: &png, References: []picture{png}}); err == nil {
		t.Fatal("frames and references went together")
	}
}

// A last frame in the request reaches MiniMax, JSON and multipart alike.
func TestMiniMaxVideoTakesALastFrame(t *testing.T) {
	fresh(t)
	up := newFakeMiniMax(t)
	minimaxProvider(t, "minimax", "minimax", up)
	s := New()
	code, _, raw, _ := request(t, s, http.MethodPost, "/v1/videos", "application/json", `{"model":"minimax/MiniMax-H3","prompt":"the kite falls","input_reference":"data:image/png;base64,cG5n","last_frame":"https://example.com/end.png"}`)
	if code != 200 {
		t.Fatalf("create: %d %s", code, raw)
	}
	_, bodies, _ := up.seen()
	if !strings.Contains(bodies[0], `"role":"first_frame"`) || !strings.Contains(bodies[0], `"url":"https://example.com/end.png"},"role":"last_frame"`) && !strings.Contains(bodies[0], `"role":"last_frame"`) {
		t.Fatalf("create body %s", bodies[0])
	}
	// frames and references together are turned away before MiniMax is asked
	code, obj, raw, _ := request(t, s, http.MethodPost, "/v1/videos", "application/json", `{"model":"minimax/MiniMax-H3","prompt":"x","input_reference":"data:image/png;base64,cG5n","reference_images":["data:image/png;base64,cG5n"]}`)
	if code != 400 || !strings.Contains(errorOf(obj), "not both") {
		t.Fatalf("mixed: %d %s", code, raw)
	}
	if paths, _, _ := up.seen(); len(paths) != 1 {
		t.Fatalf("MiniMax was asked: %v", paths)
	}
}

// A bad key, an empty balance and refused content are each said for what
// they are, with MiniMax's own words and status; so is a task that failed.
func TestMiniMaxVideoFailuresSayWhy(t *testing.T) {
	fresh(t)
	up := newFakeMiniMax(t)
	p := minimaxProvider(t, "minimax", "minimax", up)
	s := New()
	for _, c := range []struct {
		prompt string
		code   int
		says   []string
	}{
		{"BROKE", 402, []string{"out of balance", "pay-as-you-go", "insufficient balance (1008)"}},
		{"SENSITIVE", 422, []string{"sensitive content: change what the video asks for", "(1026)"}},
	} {
		code, obj, raw, _ := request(t, s, http.MethodPost, "/v1/videos", "application/json", `{"model":"minimax/MiniMax-H3","prompt":"`+c.prompt+`"}`)
		for _, want := range c.says {
			if code != c.code || !strings.Contains(errorOf(obj), want) {
				t.Fatalf("%s: want %d %q, got %d %s", c.prompt, c.code, want, code, raw)
			}
		}
	}
	// the key turned away
	p.Key = "bad-key"
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	code, obj, raw, _ := request(t, s, http.MethodPost, "/v1/videos", "application/json", `{"model":"minimax/MiniMax-H3","prompt":"a kite"}`)
	if code != 401 || !strings.Contains(errorOf(obj), "turned the API key away") || !strings.Contains(errorOf(obj), "(1004)") {
		t.Fatalf("bad key: %d %s", code, raw)
	}
	p.Key = "key"
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	// a task that failed says why, by its code
	code, obj, raw, _ = request(t, s, http.MethodPost, "/v1/videos", "application/json", `{"model":"minimax/MiniMax-H3","prompt":"FAILING"}`)
	if code != 200 {
		t.Fatalf("create failing: %d %s", code, raw)
	}
	id := obj["id"].(string)
	code, obj, raw, _ = request(t, s, http.MethodGet, "/v1/videos/"+id, "", "")
	e, _ := obj["error"].(map[string]any)
	if code != 200 || obj["status"] != "failed" || e["code"] != "1026" || !strings.Contains(e["message"].(string), "sensitive content") || !strings.Contains(e["message"].(string), "video description contains sensitive content") {
		t.Fatalf("failed: %d %s", code, raw)
	}
	code, obj, raw, _ = request(t, s, http.MethodGet, "/v1/videos/"+id+"/content", "", "")
	if code != 409 || !strings.Contains(errorOf(obj), "change what the video asks for") {
		t.Fatalf("failed content: %d %s", code, raw)
	}
}

// generate_video, with no model to name, makes its video with H3 when a
// MiniMax key is all that makes videos; a task still queued when the call
// has waited its time answers with the id to wait on, and asking again
// while it is still queued says so again.
func TestImageMCPMakesMiniMaxVideos(t *testing.T) {
	fresh(t)
	up := newFakeMiniMax(t)
	minimaxProvider(t, "minimax-cn", "minimax-cn", up)
	if m := AutoVideomaker(); m != "minimax-cn/MiniMax-H3" {
		t.Fatalf("auto videomaker %q", m)
	}
	t.Setenv("UNI_SWITCH_PUBLIC_URL", "")
	s := New()
	h := localGuard(s.Handler())
	was, wasPoll := imageMCPWithin, imageMCPPoll
	t.Cleanup(func() { imageMCPWithin, imageMCPPoll = was, wasPoll })
	imageMCPWithin, imageMCPPoll = 30*time.Millisecond, time.Millisecond
	m := readImageReply(t, imagePost(h, "127.0.0.1:5000", imageToolCall(GenerateVideoTool, map[string]any{"prompt": "STUCK kite", "seconds": 4})))
	text := m.Result.Content[0].Text
	i := strings.Index(text, `"video_minimax-cn.424010985738699.`)
	if m.Result.IsError || i < 0 || !strings.Contains(text, GenerationResultTool) {
		t.Fatalf("still queued: %+v", m)
	}
	id := strings.Trim(strings.Fields(text[i:])[0], `".,`)
	m = readImageReply(t, imagePost(h, "127.0.0.1:5000", imageToolCall(GenerationResultTool, map[string]any{"id": id})))
	if m.Result.IsError || !strings.Contains(m.Result.Content[0].Text, id) {
		t.Fatalf("still queued again: %+v", m)
	}
	imageMCPWithin = time.Minute
	m = readImageReply(t, imagePost(h, "127.0.0.1:5000", imageToolCall(GenerateVideoTool, map[string]any{"prompt": "a kite", "seconds": 4})))
	urls := urlsIn(m.Result.Content[0].Text)
	if m.Result.IsError || len(urls) != 1 || !strings.HasSuffix(urls[0], "/content") {
		t.Fatalf("made: %+v", m)
	}
	if w := imageGet(h, "127.0.0.1:5000", urls[0]); w.Code != 200 || w.Body.String() != "minimax-mp4-bytes" {
		t.Fatalf("content: %d %s", w.Code, w.Body.String())
	}
	paths, bodies, _ := up.seen()
	if paths[0] != "POST /v2/video_generation" || !strings.Contains(bodies[0], `"duration":4`) || !strings.Contains(bodies[0], `"ratio":"16:9"`) {
		t.Fatalf("paths %v body %s", paths, bodies[0])
	}
}

// A key whose plan doesn't make H3 (a Token Plan's) hands the video to the
// provider's next key.
func TestMiniMaxVideoMovesPastATokenPlanKey(t *testing.T) {
	fresh(t)
	var mu sync.Mutex
	var auth []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth = append(auth, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") == "Bearer plan-key" {
			w.WriteHeader(403)
			io.WriteString(w, `{"type":"error","error":{"type":"permission_error","message":"token plan not support model (2061)","http_code":"403"}}`)
			return
		}
		io.WriteString(w, minimaxVideoCreateResponse)
	}))
	t.Cleanup(up.Close)
	was := isMiniMaxHost
	t.Cleanup(func() { isMiniMaxHost = was })
	host := strings.TrimPrefix(up.URL, "http://")
	isMiniMaxHost = func(h string) bool { return h == host }
	p, _ := provider.FromPreset("minimax")
	p.ID, p.Key, p.Chat, p.Anthropic = "minimax", "plan-key", up.URL+"/v1", up.URL+"/anthropic"
	p.Keys = []provider.KeyAccount{{Key: "plan-key"}, {Key: "payg-key"}}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	s := New()
	code, obj, raw, _ := request(t, s, http.MethodPost, "/v1/videos", "application/json", `{"model":"minimax/MiniMax-H3","prompt":"a kite"}`)
	if code != 200 || !strings.HasSuffix(obj["id"].(string), "."+keyMark(provider.Provider{Key: "payg-key"})) {
		t.Fatalf("create: %d %s (asked with %v)", code, raw, auth)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(auth) != 2 || auth[0] != "Bearer plan-key" || auth[1] != "Bearer payg-key" {
		t.Fatalf("asked with %v", auth)
	}
}
