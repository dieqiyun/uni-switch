package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

func TestNativeImageMultipartAndVendorAdapters(t *testing.T) {
	var polls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body gateway.Object
		if r.Method == "POST" {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1beta/models/gemini-image:generateContent":
			parts := anyList(object(anyList(body["contents"])[0])["parts"])
			if len(parts) != 2 || object(object(parts[1])["inlineData"])["data"] != "c3ludGhldGlj" {
				t.Error(body)
			}
			fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"ZWRpdGVk"}}]}}]}`)
		case "/api/v1/services/aigc/multimodal-generation/generation":
			if object(body["parameters"])["size"] != "1024*1024" {
				t.Error(body)
			}
			fmt.Fprint(w, `{"output":{"choices":[{"message":{"content":[{"image":"https://image.invalid/qwen.png"}]}}]}}`)
		case "/v1/images/generations":
			if r.Header.Get("X-ModelScope-Async-Mode") != "true" {
				t.Error(r.Header)
			}
			fmt.Fprint(w, `{"task_id":"synthetic-task"}`)
		case "/v1/tasks/synthetic-task":
			polls.Add(1)
			if r.Header.Get("X-ModelScope-Task-Type") != "image_generation" {
				t.Error(r.Header)
			}
			fmt.Fprint(w, `{"task_status":"SUCCEED","output_images":["https://image.invalid/modelscope.png"]}`)
		case "/v1/images":
			if body["aspect_ratio"] != "3:2" || len(anyList(body["input_references"])) != 1 {
				t.Error(body)
			}
			fmt.Fprint(w, `{"data":[{"url":"https://image.invalid/openrouter.png"}]}`)
		case "/v1/chat/completions":
			if len(anyList(body["modalities"])) != 2 {
				t.Error(body)
			}
			fmt.Fprint(w, `{"choices":[{"message":{"images":[{"image_url":{"url":"data:image/png;base64,ZWRpdGVk"}}]}}]}`)
		default:
			t.Error(r.URL)
			w.WriteHeader(404)
		}
	}))
	defer up.Close()
	dir := t.TempDir()
	core := gateway.NewService()
	for _, tc := range []struct {
		vendor, model, base string
		multipart           bool
	}{
		{"gemini", "gemini-image", "/v1beta", true},
		{"dashscope", "qwen-image", "/v1", false},
		{"modelscope", "flux", "/v1", false},
		{"openrouter", "flux-image", "/v1", false},
		{"openrouter", "google/gemini-image", "/v1", false},
	} {
		t.Run(tc.vendor+tc.model, func(t *testing.T) {
			p := Profile{Name: "fixture", BaseURL: up.URL + tc.base, Protocol: gateway.Chat, APIKey: "synthetic", Enabled: true, Vendor: tc.vendor, Models: []Model{{tc.model, "image"}}}
			if tc.vendor == "gemini" {
				p.Protocol = gateway.Gemini
			}
			id, err := Save(dir, p)
			if err != nil {
				t.Fatal(err)
			}
			body := []byte(fmt.Sprintf(`{"model":%q,"prompt":"edit","size":"1024x1024","images":["data:image/png;base64,c3ludGhldGlj"]}`, id+"/"+tc.model))
			if tc.vendor == "openrouter" {
				body = []byte(fmt.Sprintf(`{"model":%q,"prompt":"edit","size":"1536x1024","images":["data:image/png;base64,c3ludGhldGlj"]}`, id+"/"+tc.model))
			}
			content := "application/json"
			if tc.multipart {
				var buf bytes.Buffer
				form := multipart.NewWriter(&buf)
				form.WriteField("model", id+"/"+tc.model)
				form.WriteField("prompt", "edit")
				file, _ := form.CreatePart(textproto.MIMEHeader{"Content-Disposition": {`form-data; name="image[]"; filename="a.png"`}, "Content-Type": {"image/png"}})
				file.Write([]byte("synthetic"))
				form.Close()
				body = buf.Bytes()
				content = form.FormDataContentType()
			}
			w := callTest(t, dir, core, "POST", "/v1/images/edits", content, body)
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"data":[{`) {
				t.Fatal(w.Code, w.Body)
			}
		})
	}
	if polls.Load() != 1 {
		t.Fatal(polls.Load())
	}
}

func TestMiniMaxNestedTasksAndSourceValidation(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" {
			fmt.Fprint(w, `{"task_id":"native-mini"}`)
		} else {
			fmt.Fprint(w, `{"task":{"id":"native-mini","status":"succeeded","content":{"url":"https://video.invalid/movie.mp4"}}}`)
		}
	}))
	defer up.Close()
	dir := t.TempDir()
	core := gateway.NewService()
	p := Profile{Name: "mini", BaseURL: up.URL + "/v1", Protocol: gateway.Chat, APIKey: "synthetic", Enabled: true, Vendor: "minimax", Models: []Model{{"MiniMax-H3", "video"}}}
	id, err := Save(dir, p)
	if err != nil {
		t.Fatal(err)
	}
	w := callTest(t, dir, core, "POST", "/v1/videos", "application/json", []byte(fmt.Sprintf(`{"model":%q,"prompt":"movie","seconds":5}`, id+"/MiniMax-H3")))
	var out gateway.Object
	json.Unmarshal(w.Body.Bytes(), &out)
	w = callTest(t, dir, core, "GET", "/v1/videos/"+fmtText(out["id"]), "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"completed"`) {
		t.Fatal(w.Code, w.Body)
	}
	p.Account = "missing-account"
	if _, err = Save(dir, p); err == nil {
		t.Fatal("unknown account accepted")
	}
	p.Account = "chatgpt"
	p.ID = id
	if _, err = Save(dir, p); err != nil {
		t.Fatal(err)
	}
	profiles, _ := Profiles(dir, false)
	if profiles[0].APIKey != "" || profiles[0].BaseURL != "" {
		t.Fatal("account profile retained key")
	}
}
