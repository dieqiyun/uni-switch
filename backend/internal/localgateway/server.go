// Package localgateway is the Go-owned loopback entry point. Authentication,
// routes, applied-snapshot selection and response streaming stay in one core.
package localgateway

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/platform"
	"github.com/dieqiyun/uni-switch/backend/internal/state"
)

type Route struct {
	Port  uint16 `json:"port"`
	Token string `json:"token"`
}
type runtime struct {
	route     Route
	directory string
	server    *http.Server
	core      *gateway.Service
}

var servers = struct {
	sync.Mutex
	values map[string]*runtime
}{values: map[string]*runtime{}}

func fail(code, msg string) error { return &edit.Error{Code: code, Message: msg} }
func Start(directory string) (Route, error) {
	r, e := startRoot(directory)
	if e == nil {
		e = startSavedEndpoints(directory)
	}
	return r, e
}
func startRoot(directory string) (Route, error) {
	servers.Lock()
	defer servers.Unlock()
	if r := servers.values[directory]; r != nil {
		return r.route, nil
	}
	if !filepath.IsAbs(directory) {
		return Route{}, fail("bridge_config", "转换服务目录无效")
	}
	path := filepath.Join(directory, "protocol-bridge.json")
	raw, e := edit.Read(path)
	if e != nil {
		return Route{}, e
	}
	var route Route
	if raw != nil {
		if json.Unmarshal([]byte(*raw), &route) != nil || route.Port == 0 || len(route.Token) != 64 {
			return route, fail("bridge_config", "本地转换服务配置损坏，请保留数据目录并修复")
		}
		if _, e = hex.DecodeString(route.Token); e != nil {
			return route, fail("bridge_config", "本地转换服务配置无效")
		}
	}
	listener, e := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", route.Port))
	if e != nil {
		if raw != nil && Healthy(route) {
			return route, nil
		}
		return route, fail("bridge_port", "本地协议转换端口被占用，无法启动服务。请关闭占用此端口的程序后重试")
	}
	if raw == nil {
		route.Port = uint16(listener.Addr().(*net.TCPAddr).Port)
		var key [32]byte
		if _, e = rand.Read(key[:]); e != nil {
			listener.Close()
			return route, fail("bridge_config", "无法生成本地认证凭据")
		}
		route.Token = hex.EncodeToString(key[:])
	}
	if e = edit.WriteAtomic(path, strptr(agent.Canonical(route))); e != nil {
		listener.Close()
		return route, e
	}
	r := &runtime{route: route, directory: directory, core: gateway.NewService()}
	r.server = &http.Server{Handler: http.HandlerFunc(r.handle), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 64 << 10}
	servers.values[directory] = r
	go func() {
		_ = r.server.Serve(listener)
		servers.Lock()
		if servers.values[directory] == r {
			delete(servers.values, directory)
		}
		servers.Unlock()
	}()
	return route, nil
}
func strptr(s string) *string { return &s }
func Healthy(route Route) bool {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	defer t.CloseIdleConnections()
	c := &http.Client{Transport: t, Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, e := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/health", route.Port), nil)
	if e != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+route.Token)
	resp, e := c.Do(req)
	if e != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 204 && resp.Header.Get("X-Uni-Switch-Core") == "go/1"
}
func localError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": msg}})
}
func (r *runtime) handle(w http.ResponseWriter, req *http.Request) {
	if platform.Handle(w, req, r.directory, r.route.Token, r.core) {
		return
	}
	// Use the gateway's constant-time authorization for both health and inference.
	source := gateway.Responses
	target, id, operation := "codex", "", ""
	parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	extraClient := ""
	if len(parts) >= 3 && parts[0] == "clients" {
		extraClient, id = parts[1], parts[2]
		if _, ok := agent.LookupClient(extraClient); !ok {
			localError(w, 404, "客户端不存在")
			return
		}
		target = extraClient
		parts = parts[3:]
		if len(parts) > 0 && (parts[0] == "v1" || parts[0] == "v1beta") {
			parts = parts[1:]
		}
		source = gateway.Chat
		if extraClient == "gemini" || extraClient == "agy" {
			source = gateway.Gemini
		}
		if len(parts) > 0 && parts[0] == "messages" {
			source = gateway.Messages
		}
		if len(parts) > 0 && parts[0] == "responses" {
			source = gateway.Responses
		}
	} else if len(parts) >= 3 && parts[0] == "claude" {
		target, id, source = parts[1], parts[2], gateway.Messages
		if target != "claude_cli" && target != "claude_desktop" {
			localError(w, 404, "转换目标不存在")
			return
		}
		parts = parts[3:]
		if len(parts) > 0 && parts[0] == "v1" {
			parts = parts[1:]
		}
	} else if len(parts) >= 3 && parts[0] == "v1" {
		id = parts[1]
		parts = parts[2:]
	}
	xkey := req.Header.Get("x-api-key")
	if source == gateway.Gemini {
		xkey = req.Header.Get("x-goog-api-key")
		if xkey == "" {
			xkey = req.URL.Query().Get("key")
		}
	}
	auth := gateway.Call{Source: source, Token: r.route.Token, Authorization: req.Header.Get("Authorization"), XAPIKey: xkey, HasOrigin: len(req.Header.Values("Origin")) > 0}
	if !gateway.Authorized(auth) {
		localError(w, 401, "本地转换服务认证失败，请重新应用配置")
		return
	}
	if req.URL.Path == "/health" && req.Method == "GET" {
		w.Header().Set("X-Uni-Switch-Core", "go/1")
		w.WriteHeader(204)
		return
	}
	resource := strings.Join(parts, "/")
	if extraClient == "muse" && resource == "muse-code/models" && req.Method == "GET" {
		operation = "muse_models"
	} else if (resource == "models" || extraClient == "empryo" && resource == "") && req.Method == "GET" {
		operation = "models"
		if extraClient == "cursorlocal" {
			operation = "cursor_models"
		}
	} else if req.Method == "POST" {
		if source == gateway.Gemini && strings.HasPrefix(resource, "models/") {
			if strings.HasSuffix(resource, ":generateContent") || strings.HasSuffix(resource, ":streamGenerateContent") {
				operation = "inference"
			}
			if strings.HasSuffix(resource, ":countTokens") {
				operation = "count_tokens"
			}
		} else if source == gateway.Chat && resource == "chat/completions" {
			operation = "inference"
		} else if source == gateway.Responses {
			if resource == "responses" {
				operation = "inference"
			}
			if resource == "responses/compact" {
				operation = "compact"
			}
		} else {
			if resource == "messages" {
				operation = "inference"
			}
			if resource == "messages/count_tokens" {
				operation = "count_tokens"
			}
		}
	}
	if id == "" || operation == "" {
		localError(w, 404, "转换路由不存在")
		return
	}
	p, e := r.snapshot(target, id)
	if e != nil {
		localError(w, 409, "此转换配置尚未应用，或已切换供应商，请重新应用")
		return
	}
	models, e := p.Models(filepath.Join(r.directory, "model-metadata"), false)
	if e != nil {
		localError(w, 500, "无法读取模型资料")
		return
	}
	body := map[string]any{}
	if req.Method == "POST" {
		req.Body = http.MaxBytesReader(w, req.Body, 16<<20)
		d := json.NewDecoder(req.Body)
		d.UseNumber()
		if d.Decode(&body) != nil || body == nil {
			localError(w, 400, "请求必须是有效 JSON 对象且不超过 16 MiB")
			return
		}
		var extra any
		if d.Decode(&extra) != io.EOF {
			localError(w, 400, "请求包含额外 JSON 数据")
			return
		}
	}
	if source == gateway.Gemini {
		model := strings.TrimPrefix(resource, "models/")
		model, _, _ = strings.Cut(model, ":")
		body["model"] = model
		body["stream"] = strings.HasSuffix(resource, ":streamGenerateContent")
	}
	if extraClient == "minimax" {
		asked, _ := body["model"].(string)
		for id, alias := range gateway.FlatModelIDs(models) {
			if asked == alias {
				body["model"] = id
				break
			}
		}
	}
	if extraClient != "" {
		auth.Upstream = gateway.Messages
		if p.Upstream() != "anthropic" {
			auth.Upstream = gateway.Responses
			for _, m := range models {
				if m.ID == fmt.Sprint(body["model"]) && m.Metadata != nil {
					ep, _ := m.Metadata.Profile["endpoints"].(map[string]any)
					if ep["responses"] != true && ep["chatCompletions"] != false {
						auth.Upstream = gateway.Chat
					}
				}
			}
		}
	}
	auth.Operation = operation
	auth.BaseURL = fmt.Sprint(p.Summary["baseUrl"])
	auth.APIKey = p.APIKey
	auth.AuthMode = fmt.Sprint(p.Summary["authMode"])
	auth.DefaultModel = p.Model()
	auth.Models = models
	auth.Desktop = target == "claude_desktop"
	auth.Body = body
	auth.CacheDirectory = filepath.Join(r.directory, "gateway")
	headed := false
	e = r.core.Forward(req.Context(), auth, func(packet gateway.Packet) error {
		switch packet.Type {
		case "head":
			for k, v := range packet.Headers {
				if k == "content-type" || k == "cache-control" || k == "x-accel-buffering" {
					w.Header().Set(k, v)
				}
			}
			w.WriteHeader(packet.Status)
			headed = true
		case "data":
			b, e := base64.StdEncoding.DecodeString(packet.Data)
			if e != nil {
				return e
			}
			if _, e = w.Write(b); e != nil {
				return e
			}
			return http.NewResponseController(w).Flush()
		}
		return nil
	})
	if e != nil {
		if !headed {
			localError(w, 502, "Go 转换服务中断")
		} else {
			panic(http.ErrAbortHandler)
		}
	}
}
func (r *runtime) snapshot(target, id string) (agent.Provider, error) {
	db, e := state.Open(filepath.Join(r.directory, "uni-switch.db"))
	if e != nil {
		return agent.Provider{}, e
	}
	defer db.Close()
	if _, ok := agent.LookupClient(target); ok {
		var data string
		if e := db.QueryRow("SELECT state FROM client_configs WHERE id=?", target).Scan(&data); e != nil {
			return agent.Provider{}, e
		}
		var st struct {
			Active   *string         `json:"active"`
			Provider *agent.Provider `json:"provider"`
		}
		d := json.NewDecoder(strings.NewReader(data))
		d.UseNumber()
		if d.Decode(&st) != nil || st.Active == nil || *st.Active != id || st.Provider == nil || st.Provider.ID() != id {
			return agent.Provider{}, fail("bridge_inactive", "客户端应用快照无效")
		}
		return *st.Provider, nil
	}
	var active *string
	if e = db.QueryRow("SELECT active FROM targets WHERE id=?", target).Scan(&active); e != nil || active == nil || *active != id {
		return agent.Provider{}, fail("bridge_inactive", "转换配置未应用")
	}
	name := "bridge-active.json"
	if target == "claude_cli" {
		name = "bridge-claude-cli-active.json"
	}
	if target == "claude_desktop" {
		name = "bridge-claude-desktop-active.json"
	}
	raw, e := edit.Read(filepath.Join(r.directory, name))
	if e != nil || raw == nil {
		return agent.Provider{}, fail("bridge_inactive", "缺少应用快照")
	}
	var p agent.Provider
	d := json.NewDecoder(strings.NewReader(*raw))
	d.UseNumber()
	if d.Decode(&p) != nil || p.ID() != id || !p.Converted(target) {
		return p, fail("bridge_inactive", "应用快照无效")
	}
	return p, nil
}
func CloseAll(ctx context.Context) {
	servers.Lock()
	all := []*runtime{}
	for _, r := range servers.values {
		all = append(all, r)
	}
	servers.Unlock()
	for _, r := range all {
		_ = r.server.Shutdown(ctx)
	}
}
