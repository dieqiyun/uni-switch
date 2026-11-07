package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func callTest(t *testing.T, dir string, core *gateway.Service, method, path, content string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://127.0.0.1"+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 64))
	req.Header.Set("Content-Type", content)
	w := httptest.NewRecorder()
	if !Handle(w, req, dir, strings.Repeat("a", 64), core) {
		t.Fatal("not handled", path)
	}
	return w
}
func fixtureProfile(base string) Profile {
	return Profile{Name: "fixture", BaseURL: base, Protocol: gateway.Gemini, APIKey: "synthetic-key", Enabled: true, Vendor: "auto", Models: []Model{{"gemini-test", "chat"}, {"gemini-image", "image"}, {"video-test", "video"}}}
}
func TestUnifiedAuthCatalogAllProtocolsAndNativePreservation(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("x-goog-api-key") != "synthetic-key" {
			t.Error("credential leak/replacement", r.Header)
		}
		if r.URL.Path == "/v9/native-endpoint" {
			if r.Header.Get("Authorization") != "" || r.URL.Query().Get("key") != "" || r.URL.Query().Get("custom") != "keep" {
				t.Error(r.Header, r.URL)
			}
			raw, _ := io.ReadAll(r.Body)
			if string(raw) != "opaque-payload" {
				t.Error(string(raw))
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: opaque\n\n")
			return
		}
		if !strings.Contains(r.URL.Path, "gemini-test:generateContent") {
			t.Error(r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1}}`)
	}))
	defer up.Close()
	dir := t.TempDir()
	p := fixtureProfile(up.URL + "/v1beta")
	id, e := Save(dir, p)
	if e != nil {
		t.Fatal(e)
	}
	core := gateway.NewService()
	unauth := httptest.NewRequest("GET", "http://127.0.0.1/v1/models", nil)
	w := httptest.NewRecorder()
	Handle(w, unauth, dir, strings.Repeat("a", 64), core)
	if w.Code != 401 || calls.Load() != 0 {
		t.Fatal(w.Code, calls.Load())
	}
	w = callTest(t, dir, core, "GET", "/v1/models", "", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "gemini-image") || !strings.Contains(w.Body.String(), id+"/gemini-test") {
		t.Fatal(w.Body)
	}
	w = callTest(t, dir, core, "GET", "/v1/models?kind=image,video", "", nil)
	if !strings.Contains(w.Body.String(), "gemini-image") || !strings.Contains(w.Body.String(), "video-test") {
		t.Fatal(w.Body)
	}
	for _, entry := range [][2]string{{"/v1/responses", `"input":"hello"`}, {"/v1/chat/completions", `"messages":[{"role":"user","content":"hello"}]`}, {"/v1/messages", `"messages":[{"role":"user","content":"hello"}]`}, {"/v1beta/models/" + id + "/gemini-test:generateContent", `"contents":[{"parts":[{"text":"hello"}]}]`}} {
		body := []byte(fmt.Sprintf(`{"model":%q,%s}`, id+"/gemini-test", entry[1]))
		w = callTest(t, dir, core, "POST", entry[0], "application/json", body)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "hello") {
			t.Fatal(entry[0], w.Code, w.Body)
		}
	}
	w = callTest(t, dir, core, "PATCH", "/v1/native/"+id+"/v9/native-endpoint?custom=keep&key=local-secret", "text/plain", []byte("opaque-payload"))
	if w.Code != 200 || w.Body.String() != "data: opaque\n\n" {
		t.Fatal(w.Code, w.Body)
	}
	p.ID = id
	p.APIKey = ""
	p.Name = "renamed"
	if _, e = Save(dir, p); e != nil {
		t.Fatal(e)
	}
	profiles, e := Profiles(dir, false)
	if e != nil || profiles[0].APIKey != "synthetic-key" {
		t.Fatal(profiles, e)
	}
	redacted, _ := Profiles(dir, true)
	b, _ := json.Marshal(redacted)
	if strings.Contains(string(b), "synthetic-key") {
		t.Fatal("public source leaked secret")
	}
}
func TestImagesGeminiOpenAIEditAndNativeArkPath(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1beta/models/gemini-image:generateContent":
			var body gateway.Object
			_ = json.NewDecoder(r.Body).Decode(&body)
			if object(body["generationConfig"])["responseModalities"] == nil {
				t.Error(body)
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"c3ludGhldGlj"}}]}}]}`)
		case "/v1/images/edits":
			if e := r.ParseMultipartForm(1 << 20); e != nil {
				t.Error(e)
			}
			if r.FormValue("model") != "gpt-image" || r.FormValue("custom") != "keep" {
				t.Error(r.Form)
			}
			f, _, e := r.FormFile("image")
			if e != nil {
				t.Error(e)
			} else {
				b, _ := io.ReadAll(f)
				f.Close()
				if string(b) != "synthetic-png" {
					t.Error(string(b))
				}
			}
			fmt.Fprint(w, `{"data":[{"b64_json":"edited"}]}`)
		case "/ark/api/v3/images/generations":
			fmt.Fprint(w, `{"native":true}`)
		default:
			t.Error(r.URL)
			w.WriteHeader(404)
		}
	}))
	defer up.Close()
	dir := t.TempDir()
	core := gateway.NewService()
	p := fixtureProfile(up.URL + "/v1beta")
	id, e := Save(dir, p)
	if e != nil {
		t.Fatal(e)
	}
	w := callTest(t, dir, core, "POST", "/v1/images/generations", "application/json", []byte(fmt.Sprintf(`{"model":%q,"prompt":"test"}`, id+"/gemini-image")))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "c3ludGhldGlj") {
		t.Fatal(w.Code, w.Body)
	}
	p = Profile{Name: "openai", Protocol: gateway.Responses, BaseURL: up.URL + "/v1", APIKey: "synthetic-key", Enabled: true, Models: []Model{{"gpt-image", "image"}}}
	id, e = Save(dir, p)
	if e != nil {
		t.Fatal(e)
	}
	var buf bytes.Buffer
	form := multipart.NewWriter(&buf)
	_ = form.WriteField("model", id+"/gpt-image")
	_ = form.WriteField("custom", "keep")
	part, _ := form.CreateFormFile("image", "a.png")
	_, _ = part.Write([]byte("synthetic-png"))
	_ = form.Close()
	w = callTest(t, dir, core, "POST", "/v1/images/edits", form.FormDataContentType(), buf.Bytes())
	if w.Code != 200 || !strings.Contains(w.Body.String(), "edited") {
		t.Fatal(w.Code, w.Body)
	}
	p = Profile{Name: "ark", Protocol: gateway.Chat, BaseURL: up.URL + "/ark/api/v3", APIKey: "synthetic-key", Enabled: true, Vendor: "ark", Models: []Model{{"seedream", "image"}}}
	id, e = Save(dir, p)
	if e != nil {
		t.Fatal(e)
	}
	w = callTest(t, dir, core, "POST", "/v1/magpie/native/"+id+"/images/generations", "application/json", []byte(`{"model":"seedream"}`))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "native") {
		t.Fatal(w.Code, w.Body)
	}
}
func TestVideoTaskPersistsProviderAndCredentialOwner(t *testing.T) {
	var created atomic.Int32
	var reads atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-key" {
			t.Error(r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/contents/generations/tasks" && r.Method == "POST":
			created.Add(1)
			var body gateway.Object
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(anyList(body["content"])) != 1 || body["prompt"] != nil || body["duration"] != float64(5) {
				t.Error(body)
			}
			fmt.Fprint(w, `{"id":"upstream-task","status":"queued"}`)
		case strings.HasSuffix(r.URL.Path, "upstream-task"):
			reads.Add(1)
			fmt.Fprint(w, `{"id":"upstream-task","status":"succeeded","content":{"video_url":"https://video.invalid/synthetic.mp4"},"seed":42}`)
		default:
			t.Error(r.URL)
			w.WriteHeader(404)
		}
	}))
	defer up.Close()
	dir := t.TempDir()
	core := gateway.NewService()
	p := Profile{Name: "ark", Protocol: gateway.Chat, BaseURL: up.URL + "/v1", APIKey: "synthetic-key", Enabled: true, Vendor: "ark", Models: []Model{{"seedance", "video"}}}
	id, e := Save(dir, p)
	if e != nil {
		t.Fatal(e)
	}
	p.ID = id
	w := callTest(t, dir, core, "POST", "/v1/videos", "application/json", []byte(fmt.Sprintf(`{"model":%q,"prompt":"test","seconds":"5"}`, id+"/seedance")))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var v gateway.Object
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	taskID := fmtText(v["id"])
	if !strings.HasPrefix(taskID, "video_") || v["status"] != "queued" {
		t.Fatal(v)
	}
	w = callTest(t, dir, core, "GET", "/v1/videos/"+taskID, "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"completed"`) || !strings.Contains(w.Body.String(), `"seed":42`) {
		t.Fatal(w.Code, w.Body)
	}
	p.APIKey = "synthetic-changed"
	_, _ = Save(dir, p)
	w = callTest(t, dir, core, "GET", "/v1/videos/"+taskID, "", nil)
	if w.Code != 409 || reads.Load() != 1 || created.Load() != 1 {
		t.Fatal(w.Code, reads.Load(), created.Load())
	}
	p.APIKey = "synthetic-key"
	_, _ = Save(dir, p)
	w = callTest(t, dir, core, "GET", "/v1/videos/"+taskID, "", nil)
	if w.Code != 200 || reads.Load() != 2 {
		t.Fatal(w.Code, reads.Load())
	}
}
func TestNativeBlocksSubscriptionAndTraversalAndOrigin(t *testing.T) {
	dir := t.TempDir()
	p := fixtureProfile("https://unused.invalid/v1")
	p.Account = "chatgpt"
	id, e := Save(dir, p)
	if e != nil {
		t.Fatal(e)
	}
	core := gateway.NewService()
	w := callTest(t, dir, core, "GET", "/v1/native/"+id+"/v1/models", "", nil)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	p.Account = ""
	p.ID = id
	_, _ = Save(dir, p)
	w = callTest(t, dir, core, "GET", "/v1/native/"+id+"/../admin", "", nil)
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body)
	}
	req := httptest.NewRequest("GET", "http://127.0.0.1/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 64))
	req.Header.Set("Origin", "https://browser.invalid")
	w = httptest.NewRecorder()
	Handle(w, req, dir, strings.Repeat("a", 64), core)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	_ = context.Background()
}
