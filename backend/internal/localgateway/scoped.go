package localgateway

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/state"
)

// These applications cannot persist an unencrypted custom key, or require a
// public attribution key. A separate loopback listener has access to exactly
// one client's applied snapshot. It has no management or other-client routes.
func NeedsScoped(client string) bool {
	switch client {
	case "fx", "muse", "openbot", "empryo", "commandcode", "ante", "lody", "zed":
		return true
	}
	return false
}
func StartScoped(directory, client string) (Route, error) {
	if !NeedsScoped(client) {
		return Route{}, fail("invalid_client", "客户端不使用独立本地入口")
	}
	servers.Lock()
	defer servers.Unlock()
	key := directory + "|" + client
	if r := servers.values[key]; r != nil {
		return r.route, nil
	}
	path := filepath.Join(directory, "client-endpoints", client+".json")
	raw, e := edit.Read(path)
	if e != nil {
		return Route{}, e
	}
	var route Route
	if raw != nil {
		if json.Unmarshal([]byte(*raw), &route) != nil || route.Port == 0 || len(route.Token) != 64 {
			return route, fail("bridge_config", "客户端本地入口状态损坏")
		}
		if _, e := hex.DecodeString(route.Token); e != nil {
			return route, fail("bridge_config", "客户端本地入口令牌无效")
		}
	}
	l, e := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", route.Port))
	if e != nil {
		if raw != nil && scopedHealthy(route, client) {
			return route, nil
		}
		return route, fail("bridge_port", "客户端专用端口被占用，请保留配置并检查端口")
	}
	if raw == nil {
		route.Port = uint16(l.Addr().(*net.TCPAddr).Port)
		var token [32]byte
		if _, e = rand.Read(token[:]); e != nil {
			l.Close()
			return route, e
		}
		route.Token = hex.EncodeToString(token[:])
		if e = edit.WriteAtomic(path, strptr(agent.Canonical(route))); e != nil {
			l.Close()
			return route, e
		}
	}
	r := &runtime{route: route, directory: directory, core: gateway.NewService()}
	r.server = &http.Server{ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 64 << 10, Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if len(req.Header.Values("Origin")) > 0 {
			localError(w, 403, "此客户端入口不接受浏览器请求")
			return
		}
		if req.URL.Path == "/health" && req.Method == "GET" {
			if !gateway.Authorized(gateway.Call{Source: gateway.Chat, Token: route.Token, Authorization: req.Header.Get("Authorization")}) {
				localError(w, 401, "客户端入口认证失败")
				return
			}
			w.Header().Set("X-Uni-Switch-Core", "go/1")
			w.Header().Set("X-Uni-Switch-Client", client)
			w.WriteHeader(204)
			return
		}
		if strings.Contains(req.URL.Path, "clients/") || strings.Contains(req.URL.Path, "claude/") {
			localError(w, 404, "路由不存在")
			return
		}
		if client == "lody" && req.Header.Get("Authorization") != "Bearer magpie-lody" && req.Header.Get("x-api-key") != "magpie-lody" {
			localError(w, 401, "Lody 入口认证失败")
			return
		}
		if client == "zed" && !gateway.Authorized(gateway.Call{Source: gateway.Chat, Token: route.Token, Authorization: req.Header.Get("Authorization")}) {
			localError(w, 401, "Zed 入口认证失败")
			return
		}
		db, e := state.Open(filepath.Join(directory, "uni-switch.db"))
		if e != nil {
			localError(w, 409, "应用状态不可用")
			return
		}
		var data string
		e = db.QueryRow("SELECT state FROM client_configs WHERE id=?", client).Scan(&data)
		db.Close()
		var st struct {
			Active *string `json:"active"`
		}
		if e != nil || json.Unmarshal([]byte(data), &st) != nil || st.Active == nil {
			localError(w, 409, "客户端尚未应用供应商")
			return
		}
		copy := req.Clone(req.Context())
		u := *req.URL
		copy.URL = &u
		copy.URL.Path = "/clients/" + client + "/" + *st.Active + "/" + strings.TrimLeft(req.URL.Path, "/")
		copy.Header = req.Header.Clone()
		copy.Header.Set("Authorization", "Bearer "+route.Token)
		r.handle(w, copy)
	})}
	servers.values[key] = r
	go func() {
		_ = r.server.Serve(l)
		servers.Lock()
		if servers.values[key] == r {
			delete(servers.values, key)
		}
		servers.Unlock()
	}()
	return route, nil
}
func scopedHealthy(route Route, client string) bool {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	defer t.CloseIdleConnections()
	c := &http.Client{Transport: t, Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/health", route.Port), nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+route.Token)
	resp, err := c.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 204 && resp.Header.Get("X-Uni-Switch-Core") == "go/1" && resp.Header.Get("X-Uni-Switch-Client") == client
}
func startSavedEndpoints(directory string) error {
	entries, e := os.ReadDir(filepath.Join(directory, "client-endpoints"))
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	for _, entry := range entries {
		client := strings.TrimSuffix(entry.Name(), ".json")
		if entry.IsDir() || !NeedsScoped(client) {
			continue
		}
		if _, e = StartScoped(directory, client); e != nil {
			return e
		}
	}
	return nil
}
