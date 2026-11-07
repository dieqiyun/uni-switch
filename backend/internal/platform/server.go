package platform

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/dieqiyun/uni-switch/backend/internal/claudesub"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func jsonReply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func bad(w http.ResponseWriter, status int, msg string) {
	jsonReply(w, status, map[string]any{"error": map[string]any{"message": msg, "type": "gateway_error"}})
}
func knownPath(path string) bool {
	if strings.HasPrefix(path, "/v1/native/") || strings.HasPrefix(path, "/v1/magpie/native/") || strings.HasPrefix(path, "/v1/uni-switch/native/") || strings.HasPrefix(path, "/v1/videos/") || strings.HasPrefix(path, "/v1beta/models/") {
		return true
	}
	switch path {
	case "/v1/models", "/v1beta/models", "/v1/chat/completions", "/v1/responses", "/v1/messages", "/v1/messages/count_tokens", "/v1/images/generations", "/v1/images/edits", "/v1/videos":
		return true
	}
	return false
}
func Handle(w http.ResponseWriter, r *http.Request, directory, token string, core *gateway.Service) bool {
	path := r.URL.Path
	if !knownPath(path) {
		return false
	}
	xkey := r.Header.Get("x-api-key")
	if xkey == "" {
		xkey = r.Header.Get("x-goog-api-key")
	}
	if xkey == "" && strings.HasPrefix(path, "/v1beta/") {
		xkey = r.URL.Query().Get("key")
	}
	auth := gateway.Call{Source: gateway.Chat, Token: token, Authorization: r.Header.Get("Authorization"), XAPIKey: xkey, HasOrigin: len(r.Header.Values("Origin")) > 0}
	if !gateway.Authorized(auth) {
		bad(w, 401, "本地网关认证失败")
		return true
	}
	profiles, e := Profiles(directory, false)
	if e != nil {
		bad(w, 500, e.Error())
		return true
	}
	if strings.Contains(path, "/native/") {
		native(w, r, profiles, core)
		return true
	}
	if path == "/v1/models" || path == "/v1beta/models" {
		if r.Method != "GET" {
			bad(w, 405, "请使用 GET")
			return true
		}
		kinds := r.URL.Query().Get("kind")
		if kinds == "" {
			kinds = "chat"
		}
		data := []any{}
		models := []any{}
		for _, p := range profiles {
			if !p.Enabled {
				continue
			}
			for _, m := range p.Models {
				include := kinds == "all"
				for _, k := range strings.Split(kinds, ",") {
					include = include || k == m.Kind
				}
				if !include {
					continue
				}
				id := p.ID + "/" + m.ID
				data = append(data, map[string]any{"id": id, "object": "model", "owned_by": p.Name, "kind": m.Kind})
				models = append(models, map[string]any{"name": "models/" + id, "displayName": m.ID, "supportedGenerationMethods": []string{"generateContent", "streamGenerateContent", "countTokens"}})
			}
		}
		if path == "/v1beta/models" {
			jsonReply(w, 200, map[string]any{"models": models})
		} else {
			jsonReply(w, 200, map[string]any{"object": "list", "data": data})
		}
		return true
	}
	if strings.HasPrefix(path, "/v1/videos/") {
		videoGet(w, r, directory, profiles, core)
		return true
	}
	if r.Method != "POST" {
		bad(w, 405, "请使用 POST")
		return true
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<20))
	if e != nil {
		bad(w, 413, "请求超过 64 MiB")
		return true
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	if path == "/v1/images/edits" || path == "/v1/images/generations" || path == "/v1/videos" {
		media(w, r, directory, profiles, core, raw)
		return true
	}
	var body gateway.Object
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if dec.Decode(&body) != nil || body == nil {
		bad(w, 400, "请求必须是 JSON 对象")
		return true
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		bad(w, 400, "请求包含额外 JSON")
		return true
	}
	auth.Source = gateway.Responses
	operation := "inference"
	asked := fmtText(body["model"])
	switch {
	case path == "/v1/chat/completions":
		auth.Source = gateway.Chat
	case path == "/v1/messages" || path == "/v1/messages/count_tokens":
		auth.Source = gateway.Messages
		if strings.HasSuffix(path, "count_tokens") {
			operation = "count_tokens"
		}
	case strings.HasPrefix(path, "/v1beta/models/"):
		auth.Source = gateway.Gemini
		tail := strings.TrimPrefix(path, "/v1beta/models/")
		separator := strings.LastIndexByte(tail, ':')
		name, action, ok := "", "", separator >= 0
		if ok {
			name, action = tail[:separator], tail[separator+1:]
		}
		if !ok || (action != "generateContent" && action != "streamGenerateContent" && action != "countTokens") {
			bad(w, 404, "Gemini 方法不存在")
			return true
		}
		asked = name
		body["stream"] = action == "streamGenerateContent"
		if action == "countTokens" {
			operation = "count_tokens"
		}
	}
	p, model, e := pick(profiles, asked, "chat")
	if e != nil {
		bad(w, 400, e.Error())
		return true
	}
	call, e := Resolve(r.Context(), directory, p)
	if e != nil {
		bad(w, 401, e.Error())
		return true
	}
	body["model"] = model
	call.Source = auth.Source
	call.Token = token
	call.Authorization = "Bearer " + token
	call.Body = body
	call.Operation = operation
	// Copilot models can expose a subset of native APIs; honor the source only
	// when it is the profile's selected native protocol, never guess by model ID.
	if call.Transport == "claude" {
		if operation == "count_tokens" {
			bad(w, 400, "Claude 订阅不提供精确 token 计数")
			return true
		}
		if e = claudesub.Forward(r.Context(), call, packetSink(w)); e != nil {
			panic(http.ErrAbortHandler)
		}
		return true
	}
	if e = core.Forward(r.Context(), call, packetSink(w)); e != nil {
		panic(http.ErrAbortHandler)
	}
	return true
}
func packetSink(w http.ResponseWriter) gateway.Sink {
	return func(p gateway.Packet) error {
		switch p.Type {
		case "head":
			for k, v := range p.Headers {
				w.Header().Set(k, v)
			}
			w.WriteHeader(p.Status)
		case "data":
			b, e := base64.StdEncoding.DecodeString(p.Data)
			if e != nil {
				return e
			}
			if _, e = w.Write(b); e != nil {
				return e
			}
			_ = http.NewResponseController(w).Flush()
		}
		return nil
	}
}
func pick(profiles []Profile, asked, kind string) (Profile, string, error) {
	matches := []Profile{}
	ids := []string{}
	for _, p := range profiles {
		if !p.Enabled {
			continue
		}
		for _, m := range p.Models {
			if m.Kind != kind {
				continue
			}
			if asked == p.ID+"/"+m.ID {
				return p, m.ID, nil
			}
			if asked == m.ID || asked == "" {
				matches = append(matches, p)
				ids = append(ids, m.ID)
			}
		}
	}
	if len(matches) == 1 {
		return matches[0], ids[0], nil
	}
	if len(matches) > 1 {
		return Profile{}, "", errors.New("模型存在多个来源，请使用 /v1/models 返回的 来源ID/模型ID")
	}
	return Profile{}, "", errors.New("没有可用的此类型模型，请在网关来源中添加")
}
func requestURL(p Profile, path string, host bool) (string, error) {
	u, e := url.Parse(p.BaseURL)
	if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("上游地址无效")
	}
	if host {
		u.Path = ""
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimLeft(path, "/")
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}
func upstream(ctx context.Context, core *gateway.Service, p Profile, method, path string, host bool, raw []byte, content string) (*http.Response, error) {
	address, e := requestURL(p, path, host)
	if e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(raw))
	if e != nil {
		return nil, e
	}
	if content != "" {
		req.Header.Set("Content-Type", content)
	}
	if p.Protocol == gateway.Gemini {
		req.Header.Set("x-goog-api-key", p.APIKey)
	} else if p.AuthMode == "x-api-key" {
		req.Header.Set("x-api-key", p.APIKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("User-Agent", "uni-switch-core/1")
	return core.Client.Do(req)
}
func relay(w http.ResponseWriter, res *http.Response) {
	defer res.Body.Close()
	for _, k := range []string{"Content-Type", "Cache-Control", "Retry-After", "X-Request-ID", "Content-Disposition"} {
		if v := res.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(res.StatusCode)
	timer := time.AfterFunc(90*time.Second, func() { _ = res.Body.Close() })
	defer timer.Stop()
	buf := make([]byte, 32<<10)
	for {
		n, e := res.Body.Read(buf)
		if n > 0 {
			timer.Reset(90 * time.Second)
			if _, err := w.Write(buf[:n]); err != nil {
				return
			}
			_ = http.NewResponseController(w).Flush()
		}
		if e == io.EOF {
			return
		}
		if e != nil {
			panic(http.ErrAbortHandler)
		}
	}
}
func native(w http.ResponseWriter, r *http.Request, profiles []Profile, core *gateway.Service) {
	_, tail, _ := strings.Cut(r.URL.Path, "/native/")
	id, path, ok := strings.Cut(tail, "/")
	if !ok || path == "" {
		bad(w, 404, "请指定来源和原生路径")
		return
	}
	switch r.Method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
	default:
		bad(w, 405, "不支持此方法")
		return
	}
	var chosen *Profile
	for _, p := range profiles {
		if p.Enabled && p.ID == id {
			chosen = &p
			break
		}
	}
	if chosen == nil {
		bad(w, 404, "来源不存在")
		return
	}
	p := *chosen
	if p.Account != "" {
		bad(w, 400, "订阅账号不提供厂商原生透传，请使用统一推理接口")
		return
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." || segment == "." || strings.ContainsAny(segment, "\\\x00") {
			bad(w, 400, "原生路径无效")
			return
		}
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<20))
	if e != nil {
		bad(w, 413, "原生请求过大")
		return
	}
	host := true
	if vendor(p) == "ark" && (strings.HasPrefix(path, "images/") || strings.HasPrefix(path, "contents/generations/tasks")) {
		host = false
	}
	address, e := requestURL(p, path, host)
	if e != nil {
		bad(w, 400, e.Error())
		return
	}
	u, _ := url.Parse(address)
	query := r.URL.Query()
	query.Del("key")
	query.Del("api_key")
	u.RawQuery = query.Encode()
	req, e := http.NewRequestWithContext(r.Context(), r.Method, u.String(), bytes.NewReader(raw))
	if e != nil {
		bad(w, 400, "原生请求无效")
		return
	}
	for _, key := range []string{"Content-Type", "Accept", "Anthropic-Version", "Anthropic-Beta", "Idempotency-Key"} {
		if v := r.Header.Get(key); v != "" {
			req.Header.Set(key, v)
		}
	}
	if p.Protocol == gateway.Gemini {
		req.Header.Set("x-goog-api-key", p.APIKey)
	} else if p.AuthMode == "x-api-key" {
		req.Header.Set("x-api-key", p.APIKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	res, e := core.Client.Do(req)
	if e != nil {
		bad(w, 502, "无法连接厂商原生接口")
		return
	}
	relay(w, res)
}
