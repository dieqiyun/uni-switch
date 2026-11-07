// Package agent builds our clients' configuration projections, following the
// reviewed Magpie agent modules. Go configuration coordinates discovery,
// applied snapshots, confirmation and durable file transactions.
package agent

import (
	"encoding/json"
	"net/url"
	"reflect"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"gopkg.in/yaml.v3"
)

type Object = map[string]any
type File struct {
	Path    string  `json:"path"`
	Format  string  `json:"format"`
	Content *string `json:"content"`
}
type Input struct {
	Harness  string          `json:"harness,omitempty"`
	Routed   bool            `json:"routed,omitempty"`
	Client   string          `json:"client"`
	Name     string          `json:"name"`
	Model    string          `json:"model"`
	Models   []gateway.Model `json:"models"`
	Protocol string          `json:"protocol"`
	Upstream string          `json:"upstream"`
	BaseURL  string          `json:"baseUrl"`
	APIKey   string          `json:"apiKey"`
	AuthMode string          `json:"authMode"`
	Files    []File          `json:"files"`
	Previous *edit.Change    `json:"previous"`
}
type Plan struct {
	ImportLink      string        `json:"importLink,omitempty"`
	Changes         []edit.Change `json:"changes"`
	WorkbuddyRoutes *edit.Change  `json:"workbuddyRoutes"`
}

func fail(code, message string) error { return &edit.Error{Code: code, Message: message} }
func object(v any) Object             { m, _ := v.(map[string]any); return m }
func array(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	if a, ok := v.([]string); ok {
		out := make([]any, len(a))
		for i, s := range a {
			out[i] = s
		}
		return out
	}
	return nil
}
func text(v any) string { s, _ := v.(string); return s }
func num(v any) int {
	switch n := v.(type) {
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}
func pretty(v any) string { b, _ := json.MarshalIndent(v, "", "  "); return string(b) + "\n" }
func jsonObject(s *string) (Object, error) {
	v := Object{}
	if s == nil {
		return v, nil
	}
	d := json.NewDecoder(strings.NewReader(*s))
	d.UseNumber()
	if d.Decode(&v) != nil || v == nil {
		return nil, fail("invalid_json", "JSON 顶层必须是有效对象")
	}
	return v, nil
}
func mapAt(m Object, key string) (Object, error) {
	if m[key] == nil {
		m[key] = Object{}
	}
	v := object(m[key])
	if v == nil {
		return nil, fail("invalid_config", "配置字段必须是对象")
	}
	return v, nil
}
func listAt(m Object, key string) ([]any, error) {
	if m[key] == nil {
		m[key] = []any{}
	}
	v, ok := m[key].([]any)
	if !ok {
		return nil, fail("invalid_config", "配置列表格式无效")
	}
	return v, nil
}
func contains(a []any, s string) bool {
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}
func index(a []any, id string) int {
	for i, v := range a {
		if object(v)["id"] == id {
			return i
		}
	}
	return -1
}
func (p Input) base() (string, error) {
	u, e := url.Parse(p.BaseURL)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fail("invalid_url", "API 地址无效")
	}
	path := strings.TrimRight(u.Path, "/")
	explicit := false
	for _, tail := range []string{"/chat/completions", "/responses", "/messages"} {
		if strings.HasSuffix(path, tail) {
			path = strings.TrimSuffix(path, tail)
			explicit = true
			break
		}
	}
	if path == "" && !explicit && p.Protocol != "messages" {
		path = "/v1"
	}
	u.Path = path
	return strings.TrimRight(u.String(), "/"), nil
}
func (p Input) headers() Object {
	if p.AuthMode == "x-api-key" {
		return Object{"x-api-key": p.APIKey}
	}
	if p.Protocol == "messages" {
		return Object{"Authorization": "Bearer " + p.APIKey}
	}
	return Object{}
}
func (p Input) validate() error {
	if p.Client == "workbuddy" {
		if p.Protocol != "chat_completions" {
			return fail("unsupported_protocol", "WorkBuddy 自定义模型仅支持 Chat Completions")
		}
		if p.AuthMode != "bearer" {
			return fail("unsupported_auth", "WorkBuddy 需要 Bearer 认证")
		}
	}
	if p.Protocol != "messages" && p.Protocol != "responses" && p.Protocol != "chat_completions" && !(p.Routed && p.Protocol == "gemini") {
		return fail("unsupported_protocol", "未知接口")
	}
	if p.APIKey == "" {
		return fail("missing_key", "供应商缺少 API Key")
	}
	key := p.Protocol
	if key == "chat_completions" {
		key = "chatCompletions"
	}
	for _, m := range p.Models {
		var support any
		if m.Metadata != nil {
			support = object(m.Metadata.Profile["endpoints"])[key]
		}
		same := (p.Protocol == "messages") == (p.Upstream == "anthropic")
		if !p.Routed && (support == false || !same && support != true) {
			return fail("unsupported_protocol", "所选模型没有该接口的支持依据")
		}
	}
	return nil
}
func Build(p Input) (Plan, error) {
	var plan Plan
	if p.Client == "dsbot" {
		p.Client = "dsh"
	}
	if e := p.validate(); e != nil {
		return plan, e
	}
	if c, ok := LookupClient(p.Client); ok && c.Mode == "import" {
		link, e := ImportLink(p.Client, p.Name, p.BaseURL, p.APIKey)
		return Plan{ImportLink: link}, e
	}
	base, e := p.base()
	if e != nil {
		return plan, e
	}
	if p.Client != "zcode" && p.Client != "dsh" && p.Client != "workbuddy" {
		return buildRegistered(p, base)
	}
	expected := 1
	if p.Client == "dsh" {
		expected = 2
	}
	if len(p.Files) != expected {
		return plan, fail("invalid_config", "配置快照数量无效")
	}
	var output []string
	var routesBefore *string
	switch p.Client {
	case "zcode":
		doc, e := jsonObject(p.Files[0].Content)
		if e != nil {
			return plan, e
		}
		if p.Files[0].Content != nil && num(doc["schemaVersion"]) != 1 {
			return plan, fail("unsupported_schema", "ZCode 配置需要 schemaVersion 1")
		}
		doc["schemaVersion"] = 1
		config, e := mapAt(doc, "config")
		if e != nil {
			return plan, e
		}
		rulesMap, e := mapAt(config, "providerConfigRules")
		if e != nil {
			return plan, e
		}
		rules, e := listAt(rulesMap, "providerRules")
		if e != nil {
			return plan, e
		}
		ids := []any{}
		for _, m := range p.Models {
			ids = append(ids, m.ID)
		}
		api := map[string]string{"messages": "anthropic-messages", "responses": "openai-responses", "chat_completions": "openai-chat-completions"}[p.Protocol]
		rule := Object{"providerId": "uni-switch", "providerName": p.Name, "enabled": true, "config": Object{"group": "standard-personal", "access": Object{"type": "api-key", "apiKey": p.APIKey}, "api": Object{"type": api, "baseUrl": base, "headers": p.headers()}, "personalModelIds": ids, "modelOrder": ids, "visibility": "visible"}}
		at := -1
		for i, v := range rules {
			if object(v)["providerId"] == "uni-switch" {
				at = i
				break
			}
		}
		if at >= 0 {
			rules[at] = rule
		} else {
			rules = append(rules, rule)
		}
		rulesMap["providerRules"] = rules
		order, e := listAt(config, "providerOrder")
		if e != nil {
			return plan, e
		}
		if !contains(order, "uni-switch") {
			order = append([]any{"uni-switch"}, order...)
		}
		config["providerOrder"] = order
		modelRules, e := mapAt(config, "modelConfigRules")
		if e != nil {
			return plan, e
		}
		for _, k := range []string{"providerModelRules", "manualProviderModelRules"} {
			if _, e = listAt(modelRules, k); e != nil {
				return plan, e
			}
		}
		config["defaultModelSelection"] = Object{"providerId": "uni-switch", "modelId": p.Model}
		output = []string{pretty(doc)}
	case "workbuddy":
		doc, e := jsonObject(p.Files[0].Content)
		if e != nil {
			return plan, e
		}
		restricted := len(array(doc["availableModels"])) > 0
		if p.Previous != nil && p.Previous.Path == p.Files[0].Path {
			if e = retire(doc, *p.Previous); e != nil {
				return plan, e
			}
		}
		before := pretty(doc)
		routesBefore = &before
		models, e := listAt(doc, "models")
		if e != nil {
			return plan, e
		}
		for _, m := range p.Models {
			at := index(models, m.ID)
			entry := Object{}
			if at >= 0 {
				entry = object(models[at])
				if entry == nil {
					return plan, fail("invalid_config", "WorkBuddy 模型条目必须是对象")
				}
			}
			entry["id"] = m.ID
			entry["name"] = p.Name + " · " + m.ID
			entry["vendor"] = "OpenAI"
			entry["apiKey"] = p.APIKey
			entry["url"] = base + "/chat/completions"
			delete(entry, "api")
			for _, key := range []string{"maxInputTokens", "maxOutputTokens", "supportsToolCall", "supportsImages"} {
				delete(entry, key)
			}
			if m.Metadata == nil {
				return plan, fail("go_metadata", "缺少 Go 模型资料")
			}
			profile := m.Metadata.Profile
			if n := num(profile["contextWindow"]); n > 0 {
				entry["maxInputTokens"] = n
			}
			if n := m.Metadata.PublishedOutput; n > 0 {
				entry["maxOutputTokens"] = n
			}
			if b, ok := profile["toolCalls"].(bool); ok {
				entry["supportsToolCall"] = b
			}
			if b, ok := m.Metadata.Capabilities["imageInput"].(bool); ok {
				entry["supportsImages"] = b
			}
			if at >= 0 {
				models[at] = entry
			} else {
				models = append(models, entry)
			}
		}
		doc["models"] = models
		if doc["availableModels"] != nil {
			visible, e := listAt(doc, "availableModels")
			if e != nil {
				return plan, e
			}
			if restricted {
				for _, m := range p.Models {
					if !contains(visible, m.ID) {
						visible = append(visible, m.ID)
					}
				}
			}
			doc["availableModels"] = visible
		}
		output = []string{pretty(doc)}
	case "dsh":
		original := ""
		if p.Files[0].Content != nil {
			original = *p.Files[0].Content
		}
		stripped, e := removeManaged(original)
		if e != nil {
			return plan, e
		}
		if strings.TrimSpace(stripped) == "[]" || strings.TrimSpace(stripped) == "null" {
			stripped = ""
		}
		var parsed yaml.Node
		if yaml.Unmarshal([]byte(stripped), &parsed) != nil {
			return plan, fail("invalid_yaml", "DSH 补丁格式无效")
		}
		if len(parsed.Content) > 0 && parsed.Content[0].Kind != yaml.SequenceNode && parsed.Content[0].Tag != "!!null" {
			return plan, fail("invalid_config", "DSH 补丁顶层必须是列表")
		}
		if strings.Contains(stripped, "uni-switch-llm") || strings.Contains(stripped, "UNI_SWITCH_DSH_API_KEY") {
			return plan, fail("config_collision", "DSH 已有同名 uni-switch 路由")
		}
		models := []any{}
		for _, m := range p.Models {
			v := Object{"id": m.ID, "name": m.ID}
			if m.Metadata != nil {
				if n := num(m.Metadata.Profile["contextWindow"]); n > 0 {
					v["contextWindow"] = n
				}
				if n := m.Metadata.PublishedOutput; n > 0 {
					v["maxTokens"] = n
				}
				v["input"] = m.Metadata.Profile["inputModalities"]
			}
			if v["input"] == nil {
				v["input"] = []string{"text"}
			}
			models = append(models, v)
		}
		api := map[string]string{"messages": "anthropic-messages", "chat_completions": "openai-completions", "responses": "openai-responses"}[p.Protocol]
		if p.Protocol == "messages" {
			base = strings.TrimSuffix(base, "/v1")
		}
		block := []any{Object{"insert": []any{Object{"id": "uni-switch-llm", "name": "@deepseek-ai/dsh-llm-pi-ai", "config": Object{"providers": Object{"uni-switch": Object{"displayName": p.Name, "apiKeyEnv": "UNI_SWITCH_DSH_API_KEY", "api": api, "baseURL": base, "headers": p.headers(), "models": models}}}}}}, Object{"id": "agent-default-model", "config": Object{"provider": "uni-switch", "model": p.Model}}}
		generated, e := yaml.Marshal(block)
		if e != nil {
			return plan, fail("serialization", "DSH 配置生成失败")
		}
		if stripped != "" && !strings.HasSuffix(stripped, "\n") {
			stripped += "\n"
		}
		patch := stripped + "# uni-switch managed begin\n" + string(generated) + "# uni-switch managed end\n"
		creds := &yaml.Node{}
		raw := "version: 1\nrefs: {}\nrecords: {}\n"
		if p.Files[1].Content != nil {
			raw = *p.Files[1].Content
		}
		if yaml.Unmarshal([]byte(raw), creds) != nil || len(creds.Content) != 1 || creds.Content[0].Kind != yaml.MappingNode {
			return plan, fail("invalid_config", "DSH 凭据格式无效")
		}
		root := creds.Content[0]
		version, refs, records := nodeGet(root, "version"), nodeGet(root, "refs"), nodeGet(root, "records")
		if version == nil || version.Value != "1" || refs == nil || refs.Kind != yaml.MappingNode || records == nil || records.Kind != yaml.MappingNode {
			return plan, fail("unsupported_schema", "DSH 凭据需要 version: 1、refs 和 records")
		}
		for i := 0; i < len(root.Content); i += 2 {
			if k := root.Content[i].Value; k != "version" && k != "refs" && k != "records" {
				return plan, fail("unsupported_schema", "DSH 凭据存在未知顶层字段")
			}
		}
		nodeSet(refs, "UNI_SWITCH_DSH_API_KEY", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: p.APIKey})
		credential, e := yaml.Marshal(creds)
		if e != nil {
			return plan, fail("serialization", "DSH 凭据无法生成")
		}
		output = []string{patch, string(credential)}
	default:
		return plan, fail("invalid_client", "未知客户端")
	}
	for i, s := range output {
		if len(s) > 4<<20 || strings.ContainsRune(s, 0) {
			return plan, fail("invalid_config", "配置超过大小限制或包含无效字符")
		}
		s := s
		plan.Changes = append(plan.Changes, edit.Change{Path: p.Files[i].Path, Before: p.Files[i].Content, After: &s})
	}
	if routesBefore != nil {
		plan.WorkbuddyRoutes = &edit.Change{Path: p.Files[0].Path, Before: routesBefore, After: plan.Changes[0].After}
	}
	return plan, nil
}
func nodeGet(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
func nodeSet(m *yaml.Node, key string, v *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = v
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)
}
func removeManaged(s string) (string, error) {
	begin, end := "# uni-switch managed begin", "# uni-switch managed end"
	a, b := strings.Index(s, begin), strings.Index(s, end)
	if a < 0 && b < 0 {
		return s, nil
	}
	if a < 0 || b < a || strings.Count(s, begin) != 1 || strings.Count(s, end) != 1 || a > 0 && s[a-1] != '\n' {
		return "", fail("invalid_config", "DSH 管理区块标记不完整")
	}
	tail := s[b+len(end):]
	tail = strings.TrimPrefix(tail, "\r\n")
	tail = strings.TrimPrefix(tail, "\n")
	return s[:a] + tail, nil
}
func retire(doc Object, snapshot edit.Change) error {
	original, e := jsonObject(snapshot.Before)
	if e != nil {
		return e
	}
	applied, e := jsonObject(snapshot.After)
	if e != nil {
		return e
	}
	models, e := listAt(doc, "models")
	if e != nil {
		return e
	}
	retired := map[string]bool{}
	for _, old := range array(applied["models"]) {
		id := text(object(old)["id"])
		if id == "" {
			continue
		}
		at := index(array(original["models"]), id)
		var prior any
		if at >= 0 {
			prior = array(original["models"])[at]
		}
		if reflect.DeepEqual(prior, old) {
			continue
		}
		for i, entry := range models {
			if reflect.DeepEqual(entry, old) {
				if prior == nil {
					models = append(models[:i], models[i+1:]...)
				} else {
					models[i] = prior
				}
				retired[id] = true
				break
			}
		}
	}
	doc["models"] = models
	if visible, ok := doc["availableModels"].([]any); ok {
		kept := []any{}
		for _, id := range visible {
			if !retired[text(id)] || contains(array(original["availableModels"]), text(id)) {
				kept = append(kept, id)
			}
		}
		doc["availableModels"] = kept
	}
	return nil
}
