// Package platform assembles API-key and subscription sources into one gateway.
// Vendor-native requests stay opaque; cross-protocol requests share gateway IR.
package platform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/dieqiyun/uni-switch/backend/internal/accounts"
	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/state"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type Model struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}
type Profile struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	BaseURL      string           `json:"baseUrl"`
	Protocol     gateway.Protocol `json:"protocol"`
	AuthMode     string           `json:"authMode"`
	APIKey       string           `json:"apiKey,omitempty"`
	Account      string           `json:"account,omitempty"`
	Vendor       string           `json:"vendor"`
	Models       []Model          `json:"models"`
	Enabled      bool             `json:"enabled"`
	Builtin      bool             `json:"builtin,omitempty"`
	NativeModels []gateway.Model  `json:"-"`
}
type Config struct {
	Profiles []Profile `json:"profiles"`
}

var storeMu sync.Mutex

func load(directory string) (Config, error) {
	c := Config{Profiles: []Profile{}}
	raw, e := edit.Read(filepath.Join(directory, "gateway", "sources.json"))
	if e != nil {
		return c, e
	}
	if raw != nil && json.Unmarshal([]byte(*raw), &c) != nil {
		return c, errors.New("网关来源配置损坏")
	}
	return c, nil
}
func newID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func validate(p Profile) error {
	if p.Name == "" || len(p.Models) == 0 || len(p.Models) > 1000 {
		return errors.New("请填写名称和模型")
	}
	if strings.ContainsAny(p.ID, "/\\.\x00") || strings.ContainsAny(p.APIKey, "\r\n\x00") {
		return errors.New("来源标识或密钥无效")
	}
	if p.AuthMode != "" && p.AuthMode != "bearer" && p.AuthMode != "x-api-key" {
		return errors.New("未知认证方式")
	}
	switch p.Vendor {
	case "", "auto", "openai", "gemini", "ark", "grok", "minimax", "dashscope", "modelscope", "openrouter":
	default:
		return errors.New("未知媒体厂商")
	}
	if p.Account == "" {
		u, e := url.Parse(p.BaseURL)
		if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return errors.New("API 地址无效")
		}
	}
	switch p.Protocol {
	case gateway.Messages, gateway.Responses, gateway.Chat, gateway.Gemini:
	default:
		return errors.New("未知上游协议")
	}
	seen := map[string]bool{}
	for _, m := range p.Models {
		if m.ID == "" || strings.ContainsAny(m.ID, "\r\n\x00?#") || seen[m.ID] {
			return errors.New("模型 ID 为空、重复或无效")
		}
		seen[m.ID] = true
		switch m.Kind {
		case "chat", "image", "video":
		default:
			return errors.New("未知模型类型")
		}
	}
	return nil
}
func Save(directory string, p Profile) (string, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	if p.ID == "" {
		p.ID = newID()
	}
	if p.Account != "" {
		known := false
		for _, kind := range []string{"chatgpt", "claude", "google", "antigravity", "copilot"} {
			known = known || p.Account == kind
		}
		if !known {
			list, err := accounts.Open(directory).List()
			if err != nil {
				return "", err
			}
			for _, a := range list {
				known = known || a.ID == p.Account
			}
		}
		if !known {
			return "", errors.New("订阅账号不存在")
		}
		p.APIKey = ""
		p.BaseURL = ""
	}
	p.Builtin = false
	if e := validate(p); e != nil {
		return "", e
	}
	c, e := load(directory)
	if e != nil {
		return "", e
	}
	if db, err := state.Open(filepath.Join(directory, "uni-switch.db")); err == nil {
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'providers'").Scan(&count)
		if err == nil && count > 0 {
			err = db.QueryRow("SELECT COUNT(*) FROM providers WHERE json_extract(data, '$.summary.id') = ?", p.ID).Scan(&count)
		} else {
			count = 0
		}
		db.Close()
		if err != nil {
			return "", err
		}
		if count > 0 {
			return "", errors.New("现有供应商请在供应商列表中修改")
		}
	}
	found := false
	for i, old := range c.Profiles {
		if old.ID == p.ID {
			if p.APIKey == "" && p.Account == "" {
				p.APIKey = old.APIKey
			}
			c.Profiles[i] = p
			found = true
		}
	}
	if !found {
		c.Profiles = append(c.Profiles, p)
	}
	if p.Account == "" && p.APIKey == "" {
		return "", errors.New("请填写 API Key 或选择订阅账号")
	}
	if e = save(directory, c); e != nil {
		return "", e
	}
	return p.ID, nil
}
func save(directory string, c Config) error {
	if !filepath.IsAbs(directory) {
		return errors.New("网关目录无效")
	}
	if e := edit.PrivateDirectory(filepath.Join(directory, "gateway")); e != nil {
		return e
	}
	b, _ := json.Marshal(c)
	raw := string(b)
	return edit.WriteAtomic(filepath.Join(directory, "gateway", "sources.json"), &raw)
}
func Delete(directory, id string) error {
	storeMu.Lock()
	defer storeMu.Unlock()
	c, e := load(directory)
	if e != nil {
		return e
	}
	out := []Profile{}
	for _, p := range c.Profiles {
		if p.ID != id {
			out = append(out, p)
		}
	}
	c.Profiles = out
	return save(directory, c)
}
func Profiles(directory string, redact bool) ([]Profile, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	c, e := load(directory)
	if e != nil {
		return nil, e
	}
	out := append([]Profile{}, c.Profiles...)
	// Existing API providers are first-class gateway sources without copying their
	// credentials or requiring a native client to be currently applied.
	db, e := state.Open(filepath.Join(directory, "uni-switch.db"))
	if e == nil {
		defer db.Close()
		rows, err := db.Query("SELECT data FROM providers ORDER BY rowid")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var data string
				if rows.Scan(&data) != nil {
					continue
				}
				var a agent.Provider
				if json.Unmarshal([]byte(data), &a) != nil {
					continue
				}
				own, _ := edit.Read(filepath.Join(directory, "protocol-bridge.json"))
				if own != nil {
					var route struct {
						Port int `json:"port"`
					}
					_ = json.Unmarshal([]byte(*own), &route)
					u, _ := url.Parse(fmtText(a.Summary["baseUrl"]))
					if u != nil && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost") && u.Port() == strconv.Itoa(route.Port) {
						continue
					}
				}
				p := Profile{Builtin: true, ID: a.ID(), Name: fmtText(a.Summary["name"]), BaseURL: fmtText(a.Summary["baseUrl"]), APIKey: a.APIKey, AuthMode: fmtText(a.Summary["authMode"]), Enabled: true, Vendor: "auto", Models: []Model{}}
				p.Protocol = gateway.Responses
				p.NativeModels, _ = a.Models(filepath.Join(directory, "model-metadata"), true)
				if a.Upstream() == "anthropic" {
					p.Protocol = gateway.Messages
				}
				if a.Upstream() == "gemini" {
					p.Protocol = gateway.Gemini
				}
				for _, id := range a.IDs() {
					p.Models = append(p.Models, Model{id, modelKind(id)})
				}
				out = append(out, p)
			}
		}
	}
	if redact {
		for i := range out {
			out[i].APIKey = ""
		}
	}
	return out, nil
}
func fmtText(v any) string { s, _ := v.(string); return s }
func modelKind(id string) string {
	s := strings.ToLower(id)
	for _, x := range []string{"sora", "veo", "seedance", "video", "minimax-h3", "wan2", "kling"} {
		if strings.Contains(s, x) {
			return "video"
		}
	}
	for _, x := range []string{"gpt-image", "dall-e", "imagen", "-image", "seedream", "flux", "stable-diffusion"} {
		if strings.Contains(s, x) {
			return "image"
		}
	}
	return "chat"
}
func Resolve(ctx context.Context, directory string, p Profile) (gateway.Call, error) {
	c := gateway.Call{BaseURL: p.BaseURL, APIKey: p.APIKey, AuthMode: p.AuthMode, Upstream: p.Protocol, NativeRelay: true, CacheDirectory: filepath.Join(directory, "gateway")}
	for _, m := range p.Models {
		if m.Kind == "chat" {
			c.Models = append(c.Models, gateway.Model{ID: m.ID, Enabled: true})
			if c.DefaultModel == "" {
				c.DefaultModel = m.ID
			}
		}
	}
	if len(p.NativeModels) > 0 {
		c.Models = p.NativeModels
	}
	if p.Account == "" {
		return c, nil
	}
	a, e := accounts.Open(directory).Resolve(ctx, p.Account, false)
	if e != nil {
		return c, e
	}
	c.APIKey = a.Access
	c.NativeRelay = false
	c.AccountKind = a.Kind
	switch a.Kind {
	case "chatgpt":
		c.BaseURL = "https://chatgpt.com/backend-api/codex"
		c.Upstream = gateway.Responses
		c.Transport = "chatgpt"
		c.Headers = map[string]string{"ChatGPT-Account-Id": a.AccountID, "Originator": "codex_cli_rs"}
	case "google", "antigravity":
		c.BaseURL = "https://cloudcode-pa.googleapis.com"
		if a.Kind == "antigravity" {
			c.BaseURL = "https://daily-cloudcode-pa.googleapis.com"
		}
		c.Upstream = gateway.Gemini
		c.Transport = "codeassist"
		c.Project = a.Project
	case "copilot":
		c.BaseURL = a.APIBase
		c.APIKey = a.Session
		c.Upstream = p.Protocol
		c.Headers = map[string]string{"Editor-Version": "vscode/1.140.0", "Editor-Plugin-Version": "copilot-chat/0.68.0", "Copilot-Integration-Id": "vscode-chat", "X-GitHub-Api-Version": "2026-01-09"}
	case "claude":
		c.Transport = "claude"
		c.BaseURL = a.ConfigDir
		c.APIKey = a.ID
		c.Upstream = gateway.Messages
	default:
		return c, errors.New("未知账号类型")
	}
	return c, nil
}
