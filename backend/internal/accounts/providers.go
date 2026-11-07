package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"runtime"
	"time"
)

func (m *Manager) device(ctx context.Context, f *flow) {
	defer f.cancel()
	v, e := m.request(ctx, "POST", m.Endpoints.Device, "", "application/x-www-form-urlencoded", []byte(url.Values{"client_id": {"Iv1.b507a08c87ecfe98"}, "scope": {"read:user"}}.Encode()))
	if e != nil {
		f.set("failed", e.Error())
		return
	}
	code := s(v["device_code"])
	page := s(v["verification_uri"])
	if code == "" || page == "" {
		f.set("failed", "设备授权响应无效")
		return
	}
	f.mu.Lock()
	f.status.URL = page
	f.status.Code = s(v["user_code"])
	f.mu.Unlock()
	interval := time.Duration(n(v["interval"])) * time.Second
	if interval < time.Second {
		interval = 5 * time.Second
	}
	expiry := time.Now().Add(time.Duration(n(v["expires_in"])) * time.Second)
	for {
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			f.set("failed", "登录已过期")
			return
		case <-timer.C:
		}
		if time.Now().After(expiry) {
			f.set("failed", "设备码已过期")
			return
		}
		tok, err := m.request(ctx, "POST", m.Endpoints.DeviceToken, "", "application/x-www-form-urlencoded", []byte(url.Values{"client_id": {"Iv1.b507a08c87ecfe98"}, "device_code": {code}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}.Encode()))
		if err != nil {
			f.set("failed", err.Error())
			return
		}
		switch s(tok["error"]) {
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
		case "":
		default:
			f.set("failed", "GitHub 设备登录被拒绝或已过期")
			return
		}
		access := s(tok["access_token"])
		if access == "" {
			f.set("failed", "GitHub 未返回令牌")
			return
		}
		who, err := m.request(ctx, "GET", m.Endpoints.GithubWho, access, "", nil)
		if err != nil {
			f.set("failed", err.Error())
			return
		}
		a := Account{ID: random(), Kind: "copilot", Name: f.name, User: s(who["login"]), Access: access, Enabled: true}
		if a.Name == "" {
			a.Name = a.User
		}
		if a.User == "" {
			f.set("failed", "GitHub 未返回用户身份")
			return
		}
		if ctx.Err() != nil {
			return
		}
		if err = m.store(a); err != nil {
			f.set("failed", err.Error())
		} else {
			f.set("done", "登录成功")
		}
		return
	}
}
func (m *Manager) copilot(ctx context.Context, a *Account, force bool) error {
	if !force && a.Session != "" && a.SessionExpiry > time.Now().Add(time.Minute).Unix() {
		return nil
	}
	v, e := m.request(ctx, "GET", m.Endpoints.Copilot, a.Access, "", nil)
	if e != nil {
		return e
	}
	token := s(v["token"])
	if token == "" {
		return errors.New("Copilot 订阅未返回会话令牌")
	}
	base := s(object(v["endpoints"])["api"])
	if base == "" {
		base = "https://api.githubcopilot.com"
	}
	u, e := url.Parse(base)
	if e != nil || u.User != nil || u.Scheme != "https" || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("Copilot API 地址无效")
	}
	a.Session = token
	a.SessionExpiry = n(v["expires_at"])
	a.APIBase = base
	return nil
}
func (m *Manager) googleProject(ctx context.Context, a *Account) error {
	platform := map[string]string{"windows/amd64": "WINDOWS_AMD64", "linux/amd64": "LINUX_AMD64", "linux/arm64": "LINUX_ARM64", "darwin/amd64": "DARWIN_AMD64", "darwin/arm64": "DARWIN_ARM64"}[runtime.GOOS+"/"+runtime.GOARCH]
	if platform == "" {
		platform = "PLATFORM_UNSPECIFIED"
	}
	meta := map[string]any{"ideType": "IDE_UNSPECIFIED", "platform": platform, "pluginType": "GEMINI"}
	body := map[string]any{"metadata": meta}
	if a.Project != "" {
		body["cloudaicompanionProject"] = a.Project
	}
	raw, _ := json.Marshal(body)
	v, e := m.request(ctx, "POST", m.Endpoints.GoogleBase+"/v1internal:loadCodeAssist", a.Access, "application/json", raw)
	if e != nil {
		return e
	}
	project := s(v["cloudaicompanionProject"])
	if project == "" {
		project = s(object(v["cloudaicompanionProject"])["id"])
	}
	if project != "" {
		a.Project = project
		return nil
	}
	if a.Project != "" {
		return nil
	}
	tiers, _ := v["allowedTiers"].([]any)
	tier := ""
	for _, x := range tiers {
		t := object(x)
		if t["isDefault"] == true && t["userDefinedCloudaicompanionProject"] != true {
			tier = s(t["id"])
			break
		}
	}
	if tier == "" {
		return errors.New("此 Google 账号需要在账号设置中填写已授权的 Cloud 项目 ID")
	}
	raw, _ = json.Marshal(map[string]any{"tierId": tier, "metadata": meta})
	for i := 0; i < 20; i++ {
		v, e = m.request(ctx, "POST", m.Endpoints.GoogleBase+"/v1internal:onboardUser", a.Access, "application/json", raw)
		if e != nil {
			return e
		}
		if v["done"] == true {
			project = s(object(v["response"])["cloudaicompanionProject"])
			if project == "" {
				project = s(object(object(v["response"])["cloudaicompanionProject"])["id"])
			}
			if project != "" {
				a.Project = project
				return nil
			}
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return errors.New("Google 项目开通尚未完成，请稍后重试")
}
