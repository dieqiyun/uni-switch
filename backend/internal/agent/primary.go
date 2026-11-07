package agent

import (
	"sort"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// Go chooses owned keys and values, then the Go span editor applies them while
// preserving personal comments and unrelated client settings.
type Mutation struct {
	Path   []string `json:"path"`
	Value  any      `json:"value"`
	Remove bool     `json:"remove"`
}
type PrimaryInput struct {
	Client      string   `json:"client"`
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	Model       string   `json:"model"`
	IDs         []string `json:"ids"`
	BaseURL     string   `json:"baseUrl"`
	APIKey      string   `json:"apiKey"`
	AuthMode    string   `json:"authMode"`
	Protocol    string   `json:"protocol"`
	Effort      *string  `json:"effort"`
	Fast        *bool    `json:"fast"`
	Context     *int64   `json:"context"`
	Compact     *int64   `json:"compact"`
	CatalogPath string   `json:"catalogPath"`
	ProfileID   string   `json:"profileId"`
	Config      Object   `json:"config"`
	Auth        Object   `json:"auth"`
}
type PrimaryPlan struct {
	Mutations []Mutation `json:"mutations"`
	ExtraKeys []string   `json:"extraKeys"`
}

var clientKeys = []string{"model", "apiKeyHelper", "env.ANTHROPIC_BASE_URL", "env.ANTHROPIC_AUTH_TOKEN", "env.ANTHROPIC_API_KEY", "env.ANTHROPIC_MODEL", "env.ANTHROPIC_DEFAULT_SONNET_MODEL", "env.ANTHROPIC_DEFAULT_OPUS_MODEL", "env.ANTHROPIC_DEFAULT_HAIKU_MODEL", "env.ANTHROPIC_DEFAULT_FABLE_MODEL", "env.CLAUDE_CODE_USE_BEDROCK", "env.CLAUDE_CODE_USE_VERTEX", "env.CLAUDE_CODE_USE_FOUNDRY"}
var profileKeys = []string{"inferenceProvider", "inferenceGatewayBaseUrl", "inferenceGatewayApiKey", "inferenceGatewayAuthScheme", "inferenceCredentialKind", "inferenceCredentialHelper", "inferenceCredentialHelperWindows", "inferenceGatewayOidc", "inferenceIdpOidc", "inferenceModels", "modelDiscoveryEnabled", "disableDeploymentModeChooser"}

func validEffort(s string) bool {
	for _, e := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"} {
		if e == s {
			return true
		}
	}
	return false
}
func splitContext(id string) (string, bool) {
	s := strings.TrimSpace(id)
	if strings.HasSuffix(strings.ToLower(s), "[1m]") {
		return strings.TrimSpace(s[:len(s)-4]), true
	}
	return s, false
}
func modelRole(id string) string {
	return gateway.ModelRole(id)
}
func roleModel(ids []string, role, fallback string) string {
	for _, id := range ids {
		if modelRole(id) == role {
			return id
		}
	}
	if role == "fable" {
		for _, id := range ids {
			if modelRole(id) == "opus" {
				return id
			}
		}
	}
	return fallback
}
func Primary(p PrimaryInput) (PrimaryPlan, error) {
	out := PrimaryPlan{Mutations: []Mutation{}, ExtraKeys: []string{}}
	set := func(key string, v any) {
		out.Mutations = append(out.Mutations, Mutation{Path: strings.Split(key, "."), Value: v})
	}
	remove := func(key string) {
		out.Mutations = append(out.Mutations, Mutation{Path: strings.Split(key, "."), Remove: true})
	}
	if p.Config == nil {
		p.Config = Object{}
	}
	base := strings.TrimSuffix(strings.TrimRight(p.BaseURL, "/"), "/v1")
	switch p.Client {
	case "codex":
		if selected := text(p.Config["profile"]); selected != "" {
			profile := object(object(p.Config["profiles"])[selected])
			for _, key := range []string{"model", "model_provider", "model_reasoning_effort", "service_tier", "features.fast_mode", "model_context_window", "model_auto_compact_token_limit", "model_catalog_json", "model_providers.uni_switch", "web_search", "desktop.enabled-reasoning-efforts"} {
				if _, ok := profile[key]; ok {
					return out, fail("profile_override", "当前 Codex profile 覆盖了模型、供应商或性能设置，请先移除该覆盖再切换")
				}
			}
		}
		store := text(p.Config["cli_auth_credentials_store"])
		if store == "keyring" || store == "auto" {
			return out, fail("credential_store", "当前 Codex 使用系统凭据库，请使用 cli_auth_credentials_store = \"file\" 后再应用；原登录未修改")
		}
		for _, key := range []string{"model_providers", "features", "desktop"} {
			if v := p.Config[key]; v != nil && object(v) == nil {
				return out, fail("invalid_shape", "Codex 的 "+key+" 必须是 TOML 表")
			}
		}
		provider := Object{"name": p.Name, "base_url": p.BaseURL, "wire_api": "responses", "requires_openai_auth": text(object(p.Auth["tokens"])["access_token"]) != "" && p.AuthMode != "x-api-key"}
		if p.AuthMode == "x-api-key" {
			provider["http_headers"] = Object{"x-api-key": p.APIKey}
		} else {
			provider["experimental_bearer_token"] = p.APIKey
		}
		set("model", p.Model)
		set("model_provider", "uni_switch")
		set("model_providers.uni_switch", provider)
		set("model_catalog_json", p.CatalogPath)
		if p.Effort != nil {
			set("model_reasoning_effort", *p.Effort)
		} else if v, ok := p.Config["model_reasoning_effort"]; ok && !validEffort(text(v)) {
			remove("model_reasoning_effort")
		}
		if p.Protocol == "anthropic" {
			set("web_search", "disabled")
		}
		if p.Fast != nil {
			tier := "default"
			if *p.Fast {
				tier = "priority"
			}
			set("service_tier", tier)
			set("features.fast_mode", true)
		} else {
			remove("service_tier")
			remove("features.fast_mode")
		}
		if p.Context != nil {
			set("model_context_window", *p.Context)
		} else {
			remove("model_context_window")
		}
		if p.Compact != nil {
			set("model_auto_compact_token_limit", *p.Compact)
		} else {
			remove("model_auto_compact_token_limit")
		}
		var levels []any
		if v, ok := object(p.Config["desktop"])["enabled-reasoning-efforts"]; ok {
			var valid bool
			levels, valid = v.([]any)
			if !valid {
				return out, fail("invalid_reasoning_display", "Codex 的思考强度显示设置格式无效")
			}
			for _, v := range levels {
				if !validEffort(text(v)) && v != "persistent" {
					return out, fail("invalid_reasoning_display", "Codex 的思考强度显示设置格式无效")
				}
			}
		} else {
			levels = []any{"low", "medium", "high", "xhigh", "ultra", "persistent"}
		}
		for _, e := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"} {
			if !contains(levels, e) {
				levels = append(levels, e)
			}
		}
		set("desktop.enabled-reasoning-efforts", levels)
	case "claude_cli":
		if v := p.Config["env"]; v != nil && object(v) == nil {
			return out, fail("invalid_shape", "Claude 的 env 必须是对象")
		}
		for _, key := range clientKeys {
			remove(key)
		}
		set("env.ANTHROPIC_BASE_URL", base)
		auth := "env.ANTHROPIC_AUTH_TOKEN"
		if p.AuthMode == "x-api-key" {
			auth = "env.ANTHROPIC_API_KEY"
		}
		set(auth, p.APIKey)
		set("env.ANTHROPIC_MODEL", p.Model)
		set("model", p.Model)
		if p.Protocol == "openai" {
			set("env.ENABLE_TOOL_SEARCH", "false")
			out.ExtraKeys = append(out.ExtraKeys, "env.ENABLE_TOOL_SEARCH")
		}
		for _, role := range []string{"sonnet", "opus", "haiku", "fable"} {
			set("env.ANTHROPIC_DEFAULT_"+strings.ToUpper(role)+"_MODEL", roleModel(p.IDs, role, p.Model))
		}
	case "claude_desktop":
		switch p.Kind {
		case "meta":
			entries, ok := p.Config["entries"].([]any)
			if p.Config["entries"] != nil && !ok {
				return out, fail("invalid_shape", "Claude 配置库 entries 必须是数组")
			}
			found := false
			for _, e := range entries {
				if text(object(e)["id"]) == p.ProfileID {
					found = true
				}
			}
			if !found {
				entries = append(entries, Object{"id": p.ProfileID, "name": "uni-switch"})
			}
			set("entries", entries)
			set("appliedId", p.ProfileID)
		case "deployment":
			set("deploymentMode", "3p")
		default:
			for _, key := range profileKeys {
				remove(key)
			}
			models := []any{}
			positions := map[string]int{}
			for _, id := range p.IDs {
				name, large := splitContext(id)
				if i, ok := positions[name]; ok {
					if large {
						v := object(models[i])
						if v == nil {
							v = Object{"name": name}
							models[i] = v
						}
						v["supports1m"] = true
					}
					continue
				}
				positions[name] = len(models)
				if large {
					models = append(models, Object{"name": name, "supports1m": true})
				} else {
					models = append(models, name)
				}
			}
			values := Object{"inferenceProvider": "gateway", "inferenceGatewayBaseUrl": base, "inferenceGatewayApiKey": p.APIKey, "inferenceGatewayAuthScheme": p.AuthMode, "inferenceCredentialKind": "static", "inferenceModels": models, "modelDiscoveryEnabled": false, "disableDeploymentModeChooser": true}
			keys := make([]string, 0, len(values))
			for key := range values {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				set(key, values[key])
			}
		}
	default:
		return out, fail("invalid_client", "未知客户端")
	}
	return out, nil
}
