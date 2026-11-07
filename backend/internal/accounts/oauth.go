package accounts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Endpoints struct{ Authorize, Token, Who, GoogleAuthorize, GoogleToken, GoogleWho, GoogleBase, Device, DeviceToken, GithubWho, Copilot string }

func Defaults() Endpoints {
	return Endpoints{Authorize: "https://auth.openai.com/oauth/authorize", Token: "https://auth.openai.com/oauth/token", Who: "https://chatgpt.com/backend-api/codex", GoogleAuthorize: "https://accounts.google.com/o/oauth2/v2/auth", GoogleToken: "https://oauth2.googleapis.com/token", GoogleWho: "https://www.googleapis.com/oauth2/v2/userinfo?alt=json", GoogleBase: "https://cloudcode-pa.googleapis.com", Device: "https://github.com/login/device/code", DeviceToken: "https://github.com/login/oauth/access_token", GithubWho: "https://api.github.com/user", Copilot: "https://api.github.com/copilot_internal/v2/token"}
}

const codexClient = "app_EMoamEEZ73f0CkXaXp7hrann"

// Google registration belongs to the configured application. Do not distribute
// another client's registration or fall back to it when refreshing an account.
type OAuthClient struct {
	ID     string `json:"clientId"`
	Secret string `json:"clientSecret"`
}

func (c OAuthClient) valid() bool {
	return c.ID != "" && c.Secret != "" && len(c.ID) <= 4096 && len(c.Secret) <= 4096 &&
		!strings.ContainsAny(c.ID+c.Secret, "\r\n\x00")
}

type Login struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	State   string `json:"state"`
	URL     string `json:"url,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}
type flow struct {
	mu                                       sync.Mutex
	status                                   Login
	state, verifier, redirect, name, project string
	cancel                                   context.CancelFunc
	callback                                 chan string
	paste                                    chan string
	client                                   OAuthClient
}

func (f *flow) snapshot() Login { f.mu.Lock(); defer f.mu.Unlock(); return f.status }
func (f *flow) set(state, msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.status.State == "cancelled" {
		return
	}
	f.status.State = state
	f.status.Message = msg
}
func (m *Manager) LoginStatus(id string) (Login, error) {
	m.mu.Lock()
	f := m.flows[id]
	m.mu.Unlock()
	if f == nil {
		return Login{}, errors.New("登录会话不存在或已结束")
	}
	return f.snapshot(), nil
}
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.flows[id]
	if f == nil {
		return errors.New("登录会话不存在")
	}
	f.set("cancelled", "登录已取消")
	f.cancel()
	return nil
}
func (m *Manager) Callback(id, raw string) error {
	m.mu.Lock()
	f := m.flows[id]
	m.mu.Unlock()
	if f == nil || f.snapshot().State != "waiting" {
		return errors.New("登录会话已结束")
	}
	if f.status.Kind == "claude" {
		select {
		case f.paste <- raw:
			return nil
		default:
			return errors.New("正在处理登录结果")
		}
	}
	return acceptCallback(f, raw)
}
func acceptCallback(f *flow, raw string) error {
	u, e := url.Parse(raw)
	if e != nil {
		return errors.New("请粘贴完整回调地址")
	}
	expected, e := url.Parse(f.redirect)
	if e != nil || u.Scheme != expected.Scheme || u.Host != expected.Host || u.Path != expected.Path {
		return errors.New("登录回调地址不匹配")
	}
	q := u.Query()
	if len(q["state"]) != 1 || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(f.state)) != 1 {
		return errors.New("登录 state 校验失败")
	}
	if q.Get("error") != "" {
		return errors.New("账号服务拒绝授权")
	}
	if q.Get("code") == "" || len(q["code"]) != 1 {
		return errors.New("回调缺少授权码")
	}
	select {
	case f.callback <- q.Get("code"):
		return nil
	default:
		return errors.New("回调已提交")
	}
}
func (m *Manager) Start(kind, name, project string) (Login, error) {
	return m.StartWithOAuth(kind, name, project, OAuthClient{})
}

func (m *Manager) StartWithOAuth(kind, name, project string, client OAuthClient) (Login, error) {
	if kind != "chatgpt" && kind != "google" && kind != "antigravity" && kind != "copilot" && kind != "claude" {
		return Login{}, errors.New("未知订阅类型")
	}
	if kind == "google" || kind == "antigravity" {
		client.ID = strings.TrimSpace(client.ID)
		client.Secret = strings.TrimSpace(client.Secret)
		if !client.valid() {
			return Login{}, errors.New("请填写此 Google 服务可用的 OAuth Client ID 和 Client Secret")
		}
	} else {
		client = OAuthClient{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	f := &flow{status: Login{ID: random(), Kind: kind, State: "waiting"}, state: random(), verifier: random(), name: name, project: project, client: client, cancel: cancel, callback: make(chan string, 1), paste: make(chan string, 1)}
	m.mu.Lock()
	for id, old := range m.flows {
		st := old.snapshot().State
		if st != "waiting" {
			old.cancel()
			delete(m.flows, id)
		}
	}
	if len(m.flows) >= 8 {
		m.mu.Unlock()
		cancel()
		return Login{}, errors.New("请先完成或取消已有登录")
	}
	m.flows[f.status.ID] = f
	m.mu.Unlock()
	if kind == "copilot" {
		go m.device(ctx, f)
		return f.snapshot(), nil
	}
	if kind == "claude" {
		go m.claudeLogin(ctx, f)
		return f.snapshot(), nil
	}
	port := 0
	host := "127.0.0.1"
	path := "/oauth2callback"
	if kind == "chatgpt" {
		port = 1455
		host = "localhost"
		path = "/auth/callback"
	}
	if kind == "antigravity" {
		host = "localhost"
		path = "/oauth-callback"
	}
	listener, e := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if e != nil {
		cancel()
		f.set("failed", "登录回调端口被占用，请结束已有登录后重试")
		return f.snapshot(), nil
	}
	f.redirect = fmt.Sprintf("http://%s:%d%s", host, listener.Addr().(*net.TCPAddr).Port, path)
	hash := sha256.Sum256([]byte(f.verifier))
	q := url.Values{"response_type": {"code"}, "redirect_uri": {f.redirect}, "state": {f.state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])}, "code_challenge_method": {"S256"}}
	auth := m.Endpoints.Authorize
	if kind == "chatgpt" {
		q.Set("client_id", codexClient)
		q.Set("scope", "openid profile email offline_access api.connectors.read api.connectors.invoke")
		q.Set("id_token_add_organizations", "true")
		q.Set("codex_cli_simplified_flow", "true")
		q.Set("originator", "codex_cli_rs")
	} else {
		q.Set("client_id", f.client.ID)
		scope := "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile"
		if kind == "antigravity" {
			scope += " https://www.googleapis.com/auth/cclog https://www.googleapis.com/auth/experimentsandconfigs"
		}
		q.Set("scope", scope)
		q.Set("access_type", "offline")
		q.Set("prompt", "consent")
		auth = m.Endpoints.GoogleAuthorize
	}
	f.mu.Lock()
	f.status.URL = auth + "?" + q.Encode()
	f.mu.Unlock()
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if r.Method != "GET" || r.URL.Path != path || r.Header.Get("Origin") != "" {
			http.Error(w, "Invalid callback", 400)
			return
		}
		raw := f.redirect + "?" + r.URL.RawQuery
		if acceptCallback(f, raw) != nil {
			http.Error(w, "Login callback validation failed", 400)
			return
		}
		_, _ = io.WriteString(w, "登录已收到，可以返回 uni-switch。")
	})}
	go func() { _ = server.Serve(listener) }()
	go func() {
		defer cancel()
		defer server.Close()
		select {
		case code := <-f.callback:
			a, e := m.exchange(ctx, f, code)
			if e == nil && ctx.Err() == nil {
				e = m.store(a)
			}
			if e != nil {
				f.set("failed", e.Error())
			} else if ctx.Err() == nil {
				f.set("done", "登录成功")
			} else {
				f.set("failed", "登录已过期")
			}
		case <-ctx.Done():
			f.set("failed", "登录已过期")
		}
	}()
	return f.snapshot(), nil
}
func (m *Manager) request(ctx context.Context, method, address, token, content string, body []byte) (map[string]any, error) {
	req, e := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(body))
	if e != nil {
		return nil, errors.New("账号请求地址无效")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", content)
	req.Header.Set("User-Agent", "uni-switch/1")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, e := m.Client.Do(req)
	if e != nil {
		return nil, errors.New("无法连接账号服务")
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	var v map[string]any
	if e != nil || json.Unmarshal(b, &v) != nil {
		return nil, errors.New("账号服务响应无效")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("账号服务 HTTP %d，请重新登录或检查订阅权限", res.StatusCode)
	}
	return v, nil
}
func s(v any) string              { x, _ := v.(string); return x }
func n(v any) int64               { x, _ := v.(float64); return int64(x) }
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func claims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	b, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return nil
	}
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}
func (m *Manager) exchange(ctx context.Context, f *flow, code string) (Account, error) {
	kind := f.status.Kind
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {f.redirect}, "code_verifier": {f.verifier}}
	endpoint := m.Endpoints.Token
	if kind == "chatgpt" {
		form.Set("client_id", codexClient)
	} else {
		if !f.client.valid() {
			return Account{}, errors.New("Google OAuth 客户端配置缺失，请重新登录")
		}
		form.Set("client_id", f.client.ID)
		form.Set("client_secret", f.client.Secret)
		endpoint = m.Endpoints.GoogleToken
	}
	v, e := m.request(ctx, "POST", endpoint, "", "application/x-www-form-urlencoded", []byte(form.Encode()))
	if e != nil {
		return Account{}, e
	}
	a := Account{ID: random(), Kind: kind, Name: f.name, Enabled: true, Access: s(v["access_token"]), Refresh: s(v["refresh_token"]), Expiry: time.Now().Unix() + n(v["expires_in"]), Project: f.project}
	if kind == "google" || kind == "antigravity" {
		a.OAuthClientID = f.client.ID
		a.OAuthClientSecret = f.client.Secret
	}
	if a.Access == "" || a.Refresh == "" {
		return a, errors.New("账号服务没有返回完整令牌")
	}
	if kind == "chatgpt" {
		c := claims(s(v["id_token"]))
		a.User = s(c["email"])
		auth := object(c["https://api.openai.com/auth"])
		a.AccountID = s(auth["chatgpt_account_id"])
		if a.AccountID == "" {
			a.AccountID = s(object(claims(a.Access)["https://api.openai.com/auth"])["chatgpt_account_id"])
		}
		if a.User == "" {
			a.User = a.AccountID
		}
		if a.AccountID == "" {
			return a, errors.New("ChatGPT 登录缺少账号标识")
		}
	} else {
		who, err := m.request(ctx, "GET", m.Endpoints.GoogleWho, a.Access, "", nil)
		if err != nil {
			return a, err
		}
		a.User = s(who["email"])
	}
	if a.User == "" {
		return a, errors.New("账号服务没有返回用户身份")
	}
	if a.Name == "" {
		a.Name = a.User
	}
	return a, nil
}
func (m *Manager) refresh(ctx context.Context, a *Account) error {
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {a.Refresh}}
	address := m.Endpoints.Token
	if a.Kind == "chatgpt" {
		form.Set("client_id", codexClient)
	} else {
		client := OAuthClient{ID: a.OAuthClientID, Secret: a.OAuthClientSecret}
		if !client.valid() {
			return errors.New("Google OAuth 客户端配置缺失，请重新登录")
		}
		form.Set("client_id", client.ID)
		form.Set("client_secret", client.Secret)
		address = m.Endpoints.GoogleToken
	}
	if a.Refresh == "" {
		return errors.New("账号缺少刷新令牌，请重新登录")
	}
	v, e := m.request(ctx, "POST", address, "", "application/x-www-form-urlencoded", []byte(form.Encode()))
	if e != nil {
		return e
	}
	access := s(v["access_token"])
	if access == "" {
		return errors.New("刷新没有返回访问令牌")
	}
	a.Access = access
	if next := s(v["refresh_token"]); next != "" {
		a.Refresh = next
	}
	a.Expiry = time.Now().Unix() + n(v["expires_in"])
	return nil
}
func validLoginURL(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	switch u.Host {
	case "auth.openai.com", "accounts.google.com", "github.com", "claude.ai", "console.anthropic.com", "platform.claude.com":
		return true
	}
	return false
}
