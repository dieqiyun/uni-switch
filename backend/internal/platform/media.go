package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func vendor(p Profile) string {
	if p.Vendor != "" && p.Vendor != "auto" {
		return p.Vendor
	}
	u, _ := url.Parse(p.BaseURL)
	host := strings.ToLower(u.Hostname())
	switch {
	case p.Protocol == gateway.Gemini:
		return "gemini"
	case strings.Contains(host, "volces.com"):
		return "ark"
	case strings.Contains(host, "minimax") || strings.Contains(host, "minimaxi"):
		return "minimax"
	case host == "api.x.ai":
		return "grok"
	case strings.Contains(host, "dashscope"):
		return "dashscope"
	case host == "api-inference.modelscope.cn":
		return "modelscope"
	case host == "openrouter.ai":
		return "openrouter"
	}
	return "openai"
}
func readObject(res *http.Response) (gateway.Object, error) {
	defer res.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if e != nil {
		return nil, errors.New("媒体响应中断")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("媒体上游 HTTP %d，请检查模型权限和请求参数", res.StatusCode)
	}
	var v gateway.Object
	if json.Unmarshal(raw, &v) != nil {
		return nil, errors.New("媒体上游响应不是 JSON")
	}
	return v, nil
}
func media(w http.ResponseWriter, r *http.Request, directory string, profiles []Profile, core *gateway.Service, raw []byte) {
	body := gateway.Object{}
	content := r.Header.Get("Content-Type")
	mediaType, params, _ := mime.ParseMediaType(content)
	multipartRequest := mediaType == "multipart/form-data"
	var form *multipart.Form
	if multipartRequest {
		reader := multipart.NewReader(bytes.NewReader(raw), params["boundary"])
		var e error
		form, e = reader.ReadForm(64 << 20)
		if e != nil {
			bad(w, 400, "multipart 媒体请求无效")
			return
		}
		defer form.RemoveAll()
		for k, values := range form.Value {
			if len(values) > 0 {
				body[k] = values[0]
			}
		}
	} else {
		if json.Unmarshal(raw, &body) != nil || body == nil {
			bad(w, 400, "媒体请求必须为 JSON 或 multipart")
			return
		}
	}
	kind := "image"
	if r.URL.Path == "/v1/videos" {
		kind = "video"
	}
	if kind == "image" {
		n, err := imageCount(body)
		if err != nil {
			bad(w, 400, err.Error())
			return
		}
		body["n"] = n
	}
	p, model, e := pick(profiles, fmtText(body["model"]), kind)
	if e != nil {
		bad(w, 400, e.Error())
		return
	}
	if p.Account != "" {
		bad(w, 400, "此媒体接口需要 API Key 来源；订阅来源使用其推理接口")
		return
	}
	body["model"] = model
	var buffer bytes.Buffer
	if multipartRequest {
		writer := multipart.NewWriter(&buffer)
		for k, values := range form.Value {
			for _, v := range values {
				if k == "model" {
					v = model
				}
				_ = writer.WriteField(k, v)
			}
		}
		if len(form.Value["model"]) == 0 {
			_ = writer.WriteField("model", model)
		}
		for key, files := range form.File {
			for _, file := range files {
				part, e := writer.CreateFormFile(key, file.Filename)
				if e != nil {
					bad(w, 400, "附件无效")
					return
				}
				f, e := file.Open()
				if e != nil {
					bad(w, 400, "附件无法读取")
					return
				}
				_, e = io.Copy(part, f)
				f.Close()
				if e != nil {
					bad(w, 400, "附件读取失败")
					return
				}
			}
		}
		_ = writer.Close()
		raw = buffer.Bytes()
		content = writer.FormDataContentType()
	} else {
		raw, _ = json.Marshal(body)
		content = "application/json"
	}
	if kind == "video" {
		createVideo(w, r, directory, core, p, body, raw, content)
		return
	}
	v := vendor(p)
	if v == "gemini" || v == "dashscope" || v == "modelscope" || v == "openrouter" {
		refs, err := imageReferences(body, form)
		if err != nil {
			bad(w, 400, err.Error())
			return
		}
		body["images"] = refs
	}
	if v == "gemini" {
		geminiImage(w, r, core, p, body)
		return
	}
	if v == "modelscope" {
		modelScopeImage(w, r, core, p, body)
		return
	}
	if v == "openrouter" {
		openRouterImage(w, r, core, p, body)
		return
	}
	path := "images/generations"
	if strings.HasSuffix(r.URL.Path, "/edits") {
		path = "images/edits"
	}
	if v == "dashscope" {
		dashscopeImage(w, r, core, p, body)
		return
	}
	// OpenAI-compatible and Ark image endpoints retain all caller-owned fields.
	res, e := upstream(r.Context(), mediaCore(core), p, "POST", path, false, raw, content)
	if e != nil {
		bad(w, 502, "无法连接图片生成上游")
		return
	}
	relay(w, res)
}
func imageReferences(body gateway.Object, form *multipart.Form) ([]any, error) {
	refs := append([]any{}, anyList(body["images"])...)
	if image := fmtText(body["image"]); image != "" {
		refs = append(refs, image)
	}
	if body["mask"] != nil {
		return nil, errors.New("此厂商统一接口不支持 mask，请使用厂商原生接口")
	}
	if form != nil {
		if len(form.File["mask"]) > 0 {
			return nil, errors.New("此厂商统一接口不支持 mask")
		}
		for key, files := range form.File {
			if key != "image" && key != "image[]" && key != "images" {
				return nil, errors.New("未知图片附件字段")
			}
			for _, file := range files {
				f, err := file.Open()
				if err != nil {
					return nil, err
				}
				b, err := io.ReadAll(io.LimitReader(f, (20<<20)+1))
				f.Close()
				if err != nil || len(b) > 20<<20 {
					return nil, errors.New("图片读取失败或超过 20 MiB")
				}
				mt := http.DetectContentType(b)
				if supplied, _, _ := mime.ParseMediaType(file.Header.Get("Content-Type")); strings.HasPrefix(supplied, "image/") {
					mt = supplied
				}
				if !strings.HasPrefix(mt, "image/") {
					return nil, errors.New("图片附件类型无效")
				}
				refs = append(refs, "data:"+mt+";base64,"+base64.StdEncoding.EncodeToString(b))
			}
		}
	}
	return refs, nil
}
func inlinePicture(r *http.Request, core *gateway.Service, u string) (gateway.Object, error) {
	if rest, ok := strings.CutPrefix(u, "data:"); ok {
		mt, data, ok := strings.Cut(rest, ";base64,")
		if !ok || !strings.HasPrefix(mt, "image/") {
			return nil, errors.New("图片数据 URL 无效")
		}
		if _, err := base64.StdEncoding.DecodeString(data); err != nil {
			return nil, errors.New("图片 base64 无效")
		}
		return gateway.Object{"mimeType": mt, "data": data}, nil
	}
	parsed, err := url.Parse(u)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("参考图片必须为 HTTPS 或 base64 数据 URL")
	}
	req, err := http.NewRequestWithContext(r.Context(), "GET", u, nil)
	if err != nil {
		return nil, err
	}
	res, err := core.Client.Do(req)
	if err != nil {
		return nil, errors.New("参考图片下载失败")
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, (20<<20)+1))
	if err != nil || len(b) > 20<<20 || res.StatusCode != 200 {
		return nil, errors.New("参考图片读取失败或超过 20 MiB")
	}
	mt, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if !strings.HasPrefix(mt, "image/") {
		mt = http.DetectContentType(b)
	}
	if !strings.HasPrefix(mt, "image/") {
		return nil, errors.New("参考图片类型无效")
	}
	return gateway.Object{"mimeType": mt, "data": base64.StdEncoding.EncodeToString(b)}, nil
}
func mediaCore(core *gateway.Service) *gateway.Service {
	client := *core.Client
	if transport, ok := client.Transport.(*http.Transport); ok {
		t := transport.Clone()
		t.ResponseHeaderTimeout = 5 * time.Minute
		client.Transport = t
	}
	client.Timeout = 10 * time.Minute
	return &gateway.Service{Client: &client}
}
func geminiImage(w http.ResponseWriter, r *http.Request, core *gateway.Service, p Profile, body gateway.Object) {
	parts := []any{gateway.Object{"text": body["prompt"]}}
	for _, v := range anyList(body["images"]) {
		pic, err := inlinePicture(r, core, fmtText(v))
		if err != nil {
			bad(w, 400, err.Error())
			return
		}
		parts = append(parts, gateway.Object{"inlineData": pic})
	}
	cfg := object(body["generationConfig"])
	if cfg == nil {
		cfg = map[string]any{}
	}
	cfg["responseModalities"] = []string{"TEXT", "IMAGE"}
	if x := object(body["imageConfig"]); x != nil {
		cfg["imageConfig"] = x
	} else if size := fmtText(body["size"]); size != "" && size != "auto" {
		imageCfg := gateway.Object{}
		if strings.Contains(size, ":") {
			imageCfg["aspectRatio"] = size
		} else if ratio, _ := videoSize(size); ratio != "" {
			imageCfg["aspectRatio"] = ratio
		} else {
			imageCfg["imageSize"] = strings.ToUpper(size)
		}
		cfg["imageConfig"] = imageCfg
	}
	req := gateway.Object{"contents": []any{gateway.Object{"role": "user", "parts": parts}}, "generationConfig": cfg}
	raw, _ := json.Marshal(req)
	path := "models/" + fmtText(body["model"]) + ":generateContent"
	data := []any{}
	usages := []any{}
	n, _ := imageCount(body)
	for range n {
		res, e := upstream(r.Context(), mediaCore(core), p, "POST", path, false, raw, "application/json")
		if e != nil {
			if len(data) > 0 {
				break
			}
			bad(w, 502, "Gemini 图片请求失败")
			return
		}
		v, e := readObject(res)
		if e != nil {
			if len(data) > 0 {
				break
			}
			bad(w, 502, e.Error())
			return
		}
		usages = append(usages, v["usageMetadata"])
		for _, candidate := range anyList(v["candidates"]) {
			for _, x := range anyList(object(object(candidate)["content"])["parts"]) {
				part := object(x)
				if part["thought"] == true {
					continue
				}
				inline := object(part["inlineData"])
				if inline == nil {
					inline = object(part["inline_data"])
				}
				if b := fmtText(inline["data"]); b != "" {
					data = append(data, gateway.Object{"b64_json": b})
				}
			}
		}
	}
	if len(data) == 0 {
		bad(w, 502, "Gemini 没有返回生成图片")
		return
	}
	jsonReply(w, 200, gateway.Object{"created": time.Now().Unix(), "data": data, "usage": usages})
}
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func anyList(v any) []any         { a, _ := v.([]any); return a }
func dashscopeImage(w http.ResponseWriter, r *http.Request, core *gateway.Service, p Profile, body gateway.Object) {
	contents := []any{}
	for _, image := range anyList(body["images"]) {
		contents = append(contents, gateway.Object{"image": image})
	}
	contents = append(contents, gateway.Object{"text": body["prompt"]})
	input := gateway.Object{"messages": []any{gateway.Object{"role": "user", "content": contents}}}
	parameters := gateway.Object{}
	for _, k := range []string{"size", "n", "seed", "negative_prompt", "prompt_extend", "watermark"} {
		if v := body[k]; v != nil {
			parameters[k] = v
		}
	}
	if size := fmtText(parameters["size"]); size != "" {
		parameters["size"] = strings.ReplaceAll(size, "x", "*")
	}
	req := gateway.Object{"model": body["model"], "input": input, "parameters": parameters}
	raw, _ := json.Marshal(req)
	res, e := upstream(r.Context(), mediaCore(core), p, "POST", "api/v1/services/aigc/multimodal-generation/generation", true, raw, "application/json")
	if e != nil {
		bad(w, 502, "DashScope 图片请求失败")
		return
	}
	v, e := readObject(res)
	if e != nil {
		bad(w, 502, e.Error())
		return
	}
	data := []any{}
	for _, choice := range anyList(object(v["output"])["choices"]) {
		for _, item := range anyList(object(object(choice)["message"])["content"]) {
			if u := fmtText(object(item)["image"]); u != "" {
				data = append(data, gateway.Object{"url": u})
			}
		}
	}
	if len(data) == 0 {
		bad(w, 502, "DashScope 没有返回生成图片，请使用厂商原生接口检查模型参数")
		return
	}
	jsonReply(w, 200, gateway.Object{"created": time.Now().Unix(), "data": data, "usage": v["usage"]})
}

type videoTask struct {
	ID         string         `json:"id"`
	Provider   string         `json:"provider"`
	Credential string         `json:"credential"`
	Remote     string         `json:"remote"`
	Vendor     string         `json:"vendor"`
	Model      string         `json:"model"`
	Created    int64          `json:"created"`
	Request    gateway.Object `json:"request"`
}

func credential(p Profile) string {
	sum := sha256.Sum256([]byte(p.BaseURL + "\x00" + p.APIKey))
	return hex.EncodeToString(sum[:])
}
func saveTask(directory string, t videoTask) error {
	dir := filepath.Join(directory, "gateway", "videos")
	if e := edit.PrivateDirectory(dir); e != nil {
		return e
	}
	b, _ := json.Marshal(t)
	raw := string(b)
	return edit.WriteAtomic(filepath.Join(dir, t.ID+".json"), &raw)
}
func loadTask(directory, id string) (videoTask, error) {
	var t videoTask
	if len(id) != 38 || !strings.HasPrefix(id, "video_") {
		return t, errors.New("视频任务标识无效")
	}
	if _, e := hex.DecodeString(strings.TrimPrefix(id, "video_")); e != nil {
		return t, e
	}
	raw, e := edit.Read(filepath.Join(directory, "gateway", "videos", id+".json"))
	if e != nil || raw == nil {
		return t, errors.New("视频任务不存在")
	}
	if json.Unmarshal([]byte(*raw), &t) != nil || t.ID != id {
		return t, errors.New("视频任务记录无效")
	}
	return t, nil
}
func createVideo(w http.ResponseWriter, r *http.Request, directory string, core *gateway.Service, p Profile, body gateway.Object, raw []byte, content string) {
	v := vendor(p)
	path := "videos"
	host := false
	req := body
	switch v {
	case "ark", "minimax":
		if strings.HasPrefix(content, "multipart/") {
			bad(w, 400, "此视频厂商请使用 JSON 请求和图片 URL")
			return
		}
		req = mapCopy(body)
		parts := anyList(req["content"])
		if len(parts) == 0 && body["prompt"] != nil {
			parts = append(parts, gateway.Object{"type": "text", "text": body["prompt"]})
		}
		for _, pair := range [][2]string{{"image", "first_frame"}, {"last_frame", "last_frame"}} {
			if u := fmtText(body[pair[0]]); u != "" {
				parts = append(parts, gateway.Object{"type": "image_url", "image_url": gateway.Object{"url": u}, "role": pair[1]})
			}
		}
		for _, x := range anyList(body["reference_images"]) {
			u := fmtText(x)
			if u == "" {
				u = fmtText(object(x)["url"])
			}
			parts = append(parts, gateway.Object{"type": "image_url", "image_url": gateway.Object{"url": u}, "role": "reference_image"})
		}
		req["content"] = parts
		for _, k := range []string{"prompt", "image", "last_frame", "reference_images", "seconds", "size"} {
			delete(req, k)
		}
		if body["seconds"] != nil {
			n, _ := strconv.Atoi(fmt.Sprint(body["seconds"]))
			req["duration"] = n
		}
		if size := fmtText(body["size"]); size != "" {
			ratio, res := videoSize(size)
			if req["ratio"] == nil && ratio != "" {
				req["ratio"] = ratio
			}
			if req["resolution"] == nil && res != "" {
				req["resolution"] = res
			}
		}
		path = "contents/generations/tasks"
		if v == "minimax" {
			path = "v2/video_generation"
			host = true
		}
		raw, _ = json.Marshal(req)
		content = "application/json"
	case "grok":
		path = "v1/videos/generations"
		host = true
		req = mapCopy(body)
		if x := body["seconds"]; x != nil {
			n, _ := strconv.Atoi(fmt.Sprint(x))
			req["duration"] = n
			delete(req, "seconds")
		}
		if size := fmtText(body["size"]); size != "" {
			ar, res := videoSize(size)
			if ar != "" {
				req["aspect_ratio"] = ar
			}
			if res != "" {
				req["resolution"] = res
			}
			delete(req, "size")
		}
		if image := fmtText(body["image"]); image != "" {
			req["image"] = gateway.Object{"url": image}
		}
		raw, _ = json.Marshal(req)
		content = "application/json"
	}
	res, e := upstream(r.Context(), mediaCore(core), p, "POST", path, host, raw, content)
	if e != nil {
		bad(w, 502, "无法创建视频任务")
		return
	}
	out, e := readObject(res)
	if e != nil {
		bad(w, 502, e.Error())
		return
	}
	remote := fmtText(out["id"])
	if task := object(out["task"]); task != nil {
		out = task
		remote = fmtText(out["id"])
	}
	if remote == "" {
		remote = fmtText(out["request_id"])
	}
	if remote == "" {
		remote = fmtText(out["task_id"])
	}
	if remote == "" {
		bad(w, 502, "上游没有返回视频任务标识")
		return
	}
	task := videoTask{ID: "video_" + newID(), Provider: p.ID, Credential: credential(p), Remote: remote, Vendor: v, Model: fmtText(body["model"]), Created: time.Now().Unix(), Request: body}
	if e = saveTask(directory, task); e != nil {
		bad(w, 500, "视频已提交但任务记录写入失败，请在厂商控制台查看")
		return
	}
	jsonReply(w, 200, videoObject(task, out))
}
func mapCopy(v gateway.Object) gateway.Object {
	b, _ := json.Marshal(v)
	var out gateway.Object
	_ = json.Unmarshal(b, &out)
	return out
}
func videoSize(size string) (string, string) {
	if strings.Contains(size, ":") {
		return size, ""
	}
	bits := strings.Split(size, "x")
	if len(bits) != 2 {
		return "", size
	}
	w, _ := strconv.Atoi(bits[0])
	h, _ := strconv.Atoi(bits[1])
	if w <= 0 || h <= 0 {
		return "", ""
	}
	gcd := func(a, b int) int {
		for b != 0 {
			a, b = b, a%b
		}
		return a
	}
	g := gcd(w, h)
	res := "480p"
	if min(w, h) >= 1080 {
		res = "1080p"
	} else if min(w, h) >= 720 {
		res = "720p"
	}
	return fmt.Sprintf("%d:%d", w/g, h/g), res
}
func videoObject(t videoTask, v gateway.Object) gateway.Object {
	if task := object(v["task"]); task != nil {
		v = task
	}
	out := mapCopy(v)
	status := fmtText(v["status"])
	if status == "" {
		status = fmtText(v["state"])
	}
	switch status {
	case "succeeded", "done", "Success", "success":
		status = "completed"
	case "running", "processing", "Processing":
		status = "in_progress"
	case "failed", "expired", "cancelled", "Fail":
		status = "failed"
	case "completed", "in_progress":
	default:
		status = "queued"
	}
	out["id"] = t.ID
	out["object"] = "video"
	out["model"] = t.Model
	out["created_at"] = t.Created
	out["status"] = status
	if status == "completed" {
		out["progress"] = 100
	}
	if out["progress"] == nil {
		out["progress"] = 0
	}
	return out
}
func videoGet(w http.ResponseWriter, r *http.Request, directory string, profiles []Profile, core *gateway.Service) {
	if r.Method != "GET" && r.Method != "DELETE" {
		bad(w, 405, "视频任务只支持 GET 或 DELETE")
		return
	}
	tail := strings.TrimPrefix(r.URL.Path, "/v1/videos/")
	id, action, _ := strings.Cut(tail, "/")
	if action != "" && action != "content" {
		bad(w, 404, "视频操作不存在")
		return
	}
	t, e := loadTask(directory, id)
	if e != nil {
		bad(w, 404, e.Error())
		return
	}
	var p Profile
	for _, profile := range profiles {
		if profile.ID == t.Provider && profile.Enabled {
			p = profile
			break
		}
	}
	if p.ID == "" || p.Account != "" || credential(p) != t.Credential {
		bad(w, 409, "视频来源已删除、停用或更换凭据，请恢复创建任务时的来源")
		return
	}
	remote := url.PathEscape(t.Remote)
	if strings.ContainsAny(t.Remote, "/\\?#") {
		bad(w, 500, "上游任务标识无效")
		return
	}
	path := "videos/" + remote
	host := false
	switch t.Vendor {
	case "ark":
		path = "contents/generations/tasks/" + remote
	case "minimax":
		path = "v2/query/video_generation/" + remote
		host = true
	case "grok":
		path = "v1/videos/" + remote
		host = true
	}
	if action == "content" && t.Vendor == "openai" {
		path += "/content"
	}
	res, e := upstream(r.Context(), core, p, r.Method, path, host, nil, "")
	if e != nil {
		bad(w, 502, "无法读取视频任务")
		return
	}
	if (action == "content" && t.Vendor == "openai") || r.Method == "DELETE" {
		relay(w, res)
		return
	}
	out, e := readObject(res)
	if e != nil {
		bad(w, 502, e.Error())
		return
	}
	if action != "content" {
		jsonReply(w, 200, videoObject(t, out))
		return
	}
	videoURL := fmtText(object(out["content"])["video_url"])
	if task := object(out["task"]); task != nil {
		out = task
		videoURL = fmtText(object(out["content"])["video_url"])
	}
	if videoURL == "" {
		videoURL = fmtText(object(out["content"])["url"])
	}
	if videoURL == "" {
		videoURL = fmtText(object(out["video"])["url"])
	}
	u, e := url.Parse(videoURL)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		bad(w, 409, "视频尚未完成，或没有 HTTPS 下载地址")
		return
	}
	download, e := http.NewRequestWithContext(r.Context(), "GET", videoURL, nil)
	if e != nil {
		bad(w, 502, "视频下载地址无效")
		return
	}
	response, e := core.Client.Do(download)
	if e != nil {
		bad(w, 502, "视频下载失败")
		return
	}
	relay(w, response)
}
