package supplier

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/dieqiyun/uni-switch/backend/internal/contract"
	"github.com/dieqiyun/uni-switch/backend/internal/modelmeta"
)

func itoa(n int) string  { return strconv.Itoa(n) }
func str(v any) string   { s, _ := v.(string); return s }
func obj(v any) object   { m, _ := v.(map[string]any); return m }
func arr(v any) []any    { a, _ := v.([]any); return a }
func isTrue(v any) bool  { return v == true }
func isFalse(v any) bool { return v == false }
func first(m object, names ...string) any {
	for _, name := range names {
		if v, ok := m[name]; ok {
			return v
		}
	}
	return nil
}
func integer(v any) (int64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	x, err := n.Int64()
	return x, err == nil
}
func validEffort(s string) bool {
	switch s {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra":
		return true
	}
	return false
}
func modelItems(value object) ([]any, *contract.Error) {
	items, ok := first(value, "data", "models").([]any)
	if !ok {
		return nil, contract.Fail("model_response", "模型接口需返回 data 或 models 数组")
	}
	if len(items) > 500 {
		return nil, contract.Fail("model_limit", "供应商返回了超过 500 个模型，请缩小模型列表")
	}
	return items, nil
}

func parseModels(value object) ([]contract.Model, *contract.Error) {
	items, err := modelItems(value)
	if err != nil {
		return nil, err
	}
	models := make([]contract.Model, 0, len(items))
	seen := make(map[string]bool)
	for _, item := range items {
		m := obj(item)
		id := str(item)
		if id == "" {
			id = str(m["id"])
			if id == "" {
				id = str(m["name"])
			}
			if id == "" && modelmeta.ParseVendor("", m).Codex {
				id = str(m["slug"])
			}
		}
		id = strings.TrimSpace(id)
		if id == "" || len(id) > 256 || strings.IndexFunc(id, unicode.IsControl) >= 0 {
			return nil, contract.Fail("model_response", "模型接口返回了无效的模型 ID")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		facts := modelmeta.ParseVendor(id, m)
		resolved := modelmeta.Resolve(facts, nil, modelmeta.Preferences{}, "")
		efforts := facts.Efforts
		if efforts == nil {
			efforts = []string{}
		}
		// contextWindow is a user setting; vendor context is stored separately.
		models = append(models, contract.Model{ID: id, ReasoningEfforts: efforts, Enabled: true, Capabilities: resolved.Capabilities, Profile: resolved.Profile, Metadata: &resolved, MetadataUpdatedAt: time.Now().Unix()})
	}
	if len(models) == 0 {
		return nil, contract.Fail("no_models", "此密钥没有返回可用模型，原模型列表已保留")
	}
	return models, nil
}

// ParseModelList serves the remaining native diagnostic caller through the
// same Go parser as discovery, with no second Rust field-resolution policy.
func ParseModelList(value map[string]any) ([]contract.Model, *contract.Error) {
	return parseModels(value)
}

func modelProtocol(value object, items []any) string {
	allMessages := len(items) > 0
	openai, anthropic, allClaude := false, false, true
	for _, item := range items {
		m := obj(item)
		endpoints := arr(m["supported_endpoint_types"])
		messages, responses := false, false
		for _, e := range endpoints {
			messages = messages || str(e) == "anthropic"
			responses = responses || str(e) == "openai-response"
		}
		allMessages = allMessages && messages && !responses
		openai = openai || str(m["object"]) == "model"
		anthropic = anthropic || str(m["type"]) == "model"
		allClaude = allClaude && strings.HasPrefix(str(m["id"]), "claude-")
	}
	if allMessages {
		return "anthropic"
	}
	if str(value["object"]) == "list" || openai {
		return "openai"
	}
	_, paginated := value["has_more"]
	if anthropic || paginated && allClaude {
		return "anthropic"
	}
	return "openai"
}

// Discover uses one total deadline across endpoints, auth attempts and pages.
// Only metadata GETs are retried; this service never replays inference requests.
func (s *Service) Discover(ctx context.Context, c contract.Connection) (*contract.Models, *contract.Error) {
	u, err := validateURL(c.BaseURL)
	if err != nil {
		return nil, err
	}
	if c.Protocol != nil && *c.Protocol != "openai" && *c.Protocol != "anthropic" {
		return nil, contract.Fail("invalid_protocol", "模型查询协议无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	modes := []string{"bearer", "x-api-key"}
	if strings.EqualFold(u.Hostname(), "api.anthropic.com") {
		modes = []string{"x-api-key", "bearer"}
	}
	if c.AuthMode != nil {
		modes = []string{*c.AuthMode}
	}
	var last *contract.Error
	for _, mode := range modes {
		result, failure := s.syncModels(ctx, c, mode)
		if failure == nil {
			return result, nil
		}
		last = failure
		if failure.Status != 401 && failure.Status != 403 {
			break
		}
	}
	return nil, last
}

func (s *Service) syncModels(ctx context.Context, c contract.Connection, auth string) (*contract.Models, *contract.Error) {
	if auth != "bearer" && auth != "x-api-key" {
		return nil, contract.Fail("invalid_auth", "模型查询认证方式无效")
	}
	base := strings.TrimRight(c.BaseURL, "/")
	endpoints := []string{base + "/models"}
	if !strings.HasSuffix(base, "/v1") && !strings.HasSuffix(base, "/backend-api/codex") {
		endpoints = append(endpoints, base+"/v1/models")
	}
	last := contract.Fail("no_models", "未找到可用的模型接口")
	for _, endpoint := range endpoints {
		u, err := validateURL(endpoint)
		if err != nil {
			return nil, err
		}
		collected := []contract.Model{}
		catalog := []any{}
		seen := make(map[string]bool)
		cursors := make(map[string]bool)
		for page := 0; page < 10; page++ {
			if ctx.Err() != nil {
				return nil, contract.Fail("network", "模型同步超时，请稍后重试")
			}
			version := c.Protocol == nil || *c.Protocol != "openai"
			value, err := s.get(ctx, u, &c.APIKey, nil, auth == "x-api-key", version, 10*time.Second)
			if err != nil {
				if err.Status == 404 || err.Status == 405 || err.Code == "supplier_json" {
					last = err
					break
				}
				return nil, err
			}
			models, err := parseModels(value)
			if err != nil {
				if err.Code == "model_response" {
					last = err
					break
				}
				return nil, err
			}
			items, _ := modelItems(value)
			catalog = append(catalog, items...)
			for _, model := range models {
				if !seen[model.ID] {
					seen[model.ID] = true
					collected = append(collected, model)
				}
			}
			if len(collected) > 500 {
				return nil, contract.Fail("model_limit", "供应商返回了超过 500 个模型，请缩小模型列表")
			}
			if !isTrue(value["has_more"]) {
				protocol := modelProtocol(value, catalog)
				if c.Protocol != nil {
					protocol = *c.Protocol
				}
				resolved := *u
				resolved.RawQuery = ""
				escaped := strings.TrimSuffix(resolved.EscapedPath(), "/models")
				resolved.Path = strings.TrimSuffix(resolved.Path, "/models")
				resolved.RawPath = escaped
				resolvedBase := strings.TrimRight(resolved.String(), "/")
				if protocol == "anthropic" {
					resolvedBase = base
				}
				return &contract.Models{Models: collected, SyncedAt: time.Now().Unix(), Protocol: protocol, AuthMode: auth, BaseURL: resolvedBase}, nil
			}
			cursor := str(value["last_id"])
			if cursor == "" {
				return nil, contract.Fail("model_response", "模型分页缺少下一页标识")
			}
			if cursors[cursor] {
				return nil, contract.Fail("model_response", "模型分页标识重复，未保存不完整列表")
			}
			cursors[cursor] = true
			q := url.Values{}
			q.Set("after_id", cursor)
			u.RawQuery = q.Encode()
		}
		if len(collected) > 0 {
			return nil, contract.Fail("model_limit", "模型分页未完成，未保存不完整列表")
		}
	}
	return nil, last
}
