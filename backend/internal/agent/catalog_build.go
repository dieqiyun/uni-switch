package agent

import (
	"regexp"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

const managedID = "uni-switch"

func modelInfo(m gateway.Model) (context, output int, image, tools, reasoning bool, efforts []any) {
	if m.Metadata == nil {
		return
	}
	context = num(m.Metadata.Profile["contextWindow"])
	output = int(m.Metadata.PublishedOutput)
	if context > 0 && output > context {
		output = context
	}
	image = m.Metadata.Capabilities["imageInput"] == true
	tools = m.Metadata.Profile["toolCalls"] == true
	efforts = array(m.Metadata.Profile["reasoningEfforts"])
	reasoning = len(efforts) > 0 || m.Metadata.Capabilities["reasoning"] == true
	return
}
func (p Input) piModels() []any {
	ms := []any{}
	for _, m := range p.Models {
		c, o, im, _, re, _ := modelInfo(m)
		v := Object{"id": m.ID, "name": m.ID, "reasoning": re, "input": []string{"text"}}
		if im {
			v["input"] = []string{"text", "image"}
		}
		if c > 0 {
			v["contextWindow"] = c
		}
		if o > 0 {
			v["maxTokens"] = o
		}
		ms = append(ms, v)
	}
	return ms
}
func (p Input) piProvider(base string) Object {
	return Object{"name": p.Name, "baseUrl": base, "api": map[string]string{"chat_completions": "openai-completions", "messages": "anthropic-messages", "responses": "openai-responses"}[p.Protocol], "apiKey": p.APIKey, "models": p.piModels()}
}
func put(doc Object, path string, v any) error { return Set(doc, path, v, false) }
func mergeProvider(doc Object, path string, want Object) error {
	old := Get(doc, path)
	if old != nil && object(old) == nil {
		return fail("invalid_config", "供应商字段必须是对象")
	}
	out := Object{}
	for k, v := range object(old) {
		out[k] = v
	}
	for k, v := range want {
		out[k] = v
	}
	return put(doc, path, out)
}
func replaceOwned(doc Object, key, field string, want []any, owned func(Object) bool) error {
	old, e := listAt(doc, key)
	if e != nil {
		return e
	}
	next := []any{}
	inserted := false
	for _, v := range old {
		if object(v) == nil {
			return fail("invalid_config", "配置列表包含无效条目")
		}
		if !owned(object(v)) {
			next = append(next, v)
		} else if !inserted {
			next = append(next, want...)
			inserted = true
		}
	}
	if !inserted {
		next = append(next, want...)
	}
	doc[key] = next
	return nil
}

func buildRegistered(p Input, base string) (Plan, error) {
	var plan Plan
	c, ok := LookupClient(p.Client)
	if !ok {
		return plan, fail("invalid_client", "未知客户端")
	}
	allowed := false
	for _, v := range c.Protocols {
		allowed = allowed || v == p.Protocol
	}
	if !allowed {
		return plan, fail("unsupported_protocol", c.Name+" 不支持所选配置协议")
	}
	if len(p.Files) == 0 {
		return plan, fail("invalid_config", "缺少配置文件快照")
	}
	docs := make([]Object, len(p.Files))
	outputs := map[int]string{}
	for i, f := range p.Files {
		if f.Format == "script" && f.Content != nil && strings.TrimSpace(*f.Content) != "" && !strings.Contains(*f.Content, "# uni-switch generated launcher") {
			return plan, fail("config_collision", "同名启动脚本不属于 uni-switch，已保留，请先更换配置目录")
		}
		if f.Format == "env" || f.Format == "script" || f.Format == "zed_credential" || f.Format == "hanako_chat" {
			continue
		}
		d, e := parseConfig(f)
		if e != nil {
			return plan, e
		}
		docs[i] = d
	}
	d := docs[0]
	ref := managedID + "/" + p.Model
	var err error
	set := func(i int, path string, v any) {
		if err == nil {
			if i >= len(docs) || docs[i] == nil {
				err = fail("invalid_config", "配置文件快照不完整")
			} else {
				err = put(docs[i], path, v)
			}
		}
	}
	provider := func(i int, path string, v Object) {
		if err == nil {
			if i >= len(docs) || docs[i] == nil {
				err = fail("invalid_config", "配置文件快照不完整")
			} else {
				err = mergeProvider(docs[i], path, v)
			}
		}
	}
	switch p.Client {
	case "newmax", "alma", "hanako":
		err = buildResource(p, base, docs, outputs)
	case "opencode", "mimocode", "deveco":
		ms := Object{}
		for _, m := range p.Models {
			c, o, im, _, re, eff := modelInfo(m)
			v := Object{"name": m.ID, "reasoning": re}
			limit := Object{}
			if c > 0 {
				limit["context"] = c
			}
			if o > 0 {
				limit["output"] = o
			}
			if len(limit) > 0 {
				v["limit"] = limit
			}
			in := []string{"text"}
			if im {
				in = append(in, "image")
			}
			v["modalities"] = Object{"input": in, "output": []string{"text"}}
			variants := Object{}
			for _, x := range eff {
				variants[text(x)] = Object{"reasoningEffort": x}
			}
			if len(variants) > 0 {
				v["variants"] = variants
			}
			ms[m.ID] = v
		}
		want := Object{"npm": "@ai-sdk/openai-compatible", "name": p.Name, "options": Object{"baseURL": base, "apiKey": p.APIKey, "setCacheKey": true}, "models": ms}
		native := openCodeNative(want)
		keepOpenCodeOptions(object(Get(d, "provider."+managedID)), want, false)
		provider(0, "provider."+managedID, want)
		if Get(d, "providers."+managedID) != nil {
			keepOpenCodeOptions(object(Get(d, "providers."+managedID)), native, true)
			provider(0, "providers."+managedID, native)
		}
		set(0, "model", ref)
		if list, ok := d["enabled_providers"].([]any); ok && len(list) > 0 && !contains(list, managedID) {
			d["enabled_providers"] = append(list, managedID)
		}
		if list, ok := d["disabled_providers"].([]any); ok {
			out := []any{}
			for _, v := range list {
				if v != managedID {
					out = append(out, v)
				}
			}
			d["disabled_providers"] = out
		}
	case "pi", "omo", "primeagent", "stepcode":
		set(0, "defaultProvider", managedID)
		set(0, "defaultModel", p.Model)
		provider(1, "providers."+managedID, p.piProvider(base))
		if list, ok := d["enabledModels"].([]any); ok && len(list) > 0 {
			for _, m := range p.Models {
				r := managedID + "/" + m.ID
				if !contains(list, r) {
					list = append(list, r)
				}
			}
			d["enabledModels"] = list
		}
	case "mothx":
		v := p.piProvider(base)
		v["api"] = "openai-chat"
		v["thinkingFormat"] = "openai"
		provider(0, "providers."+managedID, v)
		set(0, "defaultProvider", managedID)
		set(0, "defaultModel", p.Model)
	case "omp":
		if p.Files[1].Content == nil && len(docs) > 2 && docs[2] != nil {
			// Writing YAML first must carry the legacy JSON providers over,
			// otherwise omp's own first-start migration would be skipped.
			docs[1] = Clone(docs[2])
		}
		v := p.piProvider(base)
		delete(v, "name")
		for _, m := range array(v["models"]) {
			m := object(m)
			for _, model := range p.Models {
				if model.ID == m["id"] {
					_, _, _, _, _, eff := modelInfo(model)
					mode := "effort"
					if p.Protocol == "messages" {
						mode = "budget"
						if model.Metadata != nil && model.Metadata.Profile["thinkingFormat"] == "adaptive" {
							mode = "anthropic-adaptive"
						}
					}
					// Unknown/older omp versions accept up to xhigh. The
					// gateway maps that back to a model whose highest is max.
					levels := []any{}
					for _, level := range eff {
						if level == "max" {
							if !contains(eff, "xhigh") {
								levels = append(levels, "xhigh")
							}
							continue
						}
						if contains([]any{"off", "minimal", "low", "medium", "high", "xhigh"}, text(level)) {
							levels = append(levels, level)
						}
					}
					if len(levels) > 0 {
						m["thinking"] = Object{"mode": mode, "efforts": levels}
					}
				}
			}
		}
		provider(1, "providers."+managedID, v)
		set(0, "modelRoles.default", ref)
		if enabled := array(d["enabledModels"]); len(enabled) > 0 {
			for _, model := range p.Models {
				id := managedID + "/" + model.ID
				if !contains(enabled, id) {
					enabled = append(enabled, id)
				}
			}
			d["enabledModels"] = enabled
		}
	case "goose":
		models := []any{}
		thinks := regexp.MustCompile(`(?i)(?:^|[-/])(?:o\d+(?:$|-)|gpt-(?:5|6)(?:$|[-.]))`)
		for _, m := range p.Models {
			c, _, _, _, _, _ := modelInfo(m)
			v := Object{"name": m.ID, "reasoning": thinks.MatchString(m.ID)}
			if c > 0 {
				v["context_limit"] = c
			}
			models = append(models, v)
		}
		headers := Object{}
		if len(docs) > 1 {
			for k, v := range object(docs[1]["headers"]) {
				if !strings.EqualFold(k, "authorization") && !strings.EqualFold(k, "x-api-key") {
					headers[k] = v
				}
			}
		}
		headers["Authorization"] = "Bearer " + p.APIKey
		for k, v := range (Object{"name": managedID, "engine": "openai", "display_name": p.Name, "api_key_env": "", "base_url": base, "headers": headers, "requires_auth": false, "skip_canonical_filtering": true, "dynamic_models": false, "models": models}) {
			if len(docs) > 1 {
				docs[1][k] = v
			} else {
				err = fail("invalid_config", "缺少 Goose 供应商文件")
			}
		}
		if d["providers"] != nil || d["active_provider"] != nil {
			set(0, "active_provider", managedID)
			provider(0, "providers."+managedID, Object{"enabled": true, "configured": true, "model": p.Model})
			delete(d, "GOOSE_PROVIDER")
			delete(d, "GOOSE_MODEL")
		} else {
			d["GOOSE_PROVIDER"] = managedID
			d["GOOSE_MODEL"] = p.Model
		}
	case "crush":
		ms := []any{}
		for _, m := range p.Models {
			c, o, _, _, re, _ := modelInfo(m)
			v := Object{"id": m.ID, "name": m.ID, "can_reason": re}
			if c > 0 {
				v["context_window"] = c
			}
			if o > 0 {
				v["default_max_tokens"] = o
			}
			ms = append(ms, v)
		}
		provider(0, "providers."+managedID, Object{"type": "openai", "name": p.Name, "base_url": base, "api_key": p.APIKey, "models": ms})
		i := len(docs) - 1
		set(i, "models.large", Object{"provider": managedID, "model": p.Model})
		set(i, "models.small", Object{"provider": managedID, "model": p.Model})
	case "copilot":
		if len(docs) != 2 {
			return plan, fail("invalid_config", "缺少 Copilot 供应商文件")
		}
		ps, e := listAt(docs[1], "providers")
		if e != nil {
			return plan, e
		}
		kept := []any{}
		for _, v := range ps {
			if object(v) == nil {
				return plan, fail("invalid_config", "Copilot providers 条目无效")
			}
			if object(v)["name"] != managedID {
				kept = append(kept, v)
			}
		}
		docs[1]["providers"] = append(kept, Object{"name": managedID, "type": "openai", "baseUrl": base, "apiKey": p.APIKey})
		ms := []any{}
		for _, m := range p.Models {
			c, o, _, _, _, _ := modelInfo(m)
			v := Object{"id": m.ID, "provider": managedID, "name": m.ID}
			if c > 0 {
				v["maxContextWindowTokens"] = c
				v["maxPromptTokens"] = max(1, c-o)
			}
			if o > 0 {
				v["maxOutputTokens"] = o
			}
			ms = append(ms, v)
		}
		err = replaceOwned(docs[1], "models", "provider", ms, func(v Object) bool { return v["provider"] == managedID })
		set(0, "model", ref)
	case "hermes":
		mode := "chat_completions"
		if p.Protocol == "messages" {
			mode = "anthropic_messages"
		}
		ms := []any{}
		for _, m := range p.Models {
			ms = append(ms, m.ID)
		}
		provider(0, "providers."+managedID, Object{"name": p.Name, "base_url": base, "api_key": p.APIKey, "api_mode": mode, "models": ms})
		set(0, "model.provider", managedID)
		set(0, "model.default", p.Model)
	case "morph":
		set(0, "llm.inference_provider", "openai_response_compatible")
		set(0, "llm.endpoint", base)
		set(0, "llm.api_key", p.APIKey)
		set(0, "llm.model", p.Model)
		for _, m := range p.Models {
			if m.ID == p.Model {
				c, _, _, _, _, _ := modelInfo(m)
				if c > 0 {
					set(0, "llm.context_window_tokens", c)
				}
			}
		}
	case "kimi":
		provider(0, "providers."+managedID, Object{"type": "kimi", "base_url": base, "api_key": p.APIKey})
		models, e := mapAt(d, "models")
		if e != nil {
			return plan, e
		}
		for _, m := range p.Models {
			c, _, im, _, _, _ := modelInfo(m)
			v := Object{"provider": managedID, "model": m.ID}
			if c > 0 {
				v["max_context_size"] = c
			}
			if im {
				v["capabilities"] = []string{"image_in"}
			}
			models[managedID+"/"+m.ID] = v
		}
		d["default_model"] = ref
	case "qwen":
		mp, e := mapAt(d, "modelProviders")
		if e != nil {
			return plan, e
		}
		ms := []any{}
		for _, m := range p.Models {
			c, _, _, _, _, _ := modelInfo(m)
			v := Object{"id": m.ID, "name": m.ID, "baseUrl": base, "envKey": "UNI_SWITCH_QWEN_API_KEY"}
			if c > 0 {
				v["generationConfig"] = Object{"contextWindowSize": c}
			}
			ms = append(ms, v)
		}
		err = replaceOwned(mp, "openai", "envKey", ms, func(v Object) bool { return v["envKey"] == "UNI_SWITCH_QWEN_API_KEY" })
		set(0, "model.name", p.Model)
		set(0, "security.auth.selectedType", "openai")
		set(0, "env.UNI_SWITCH_QWEN_API_KEY", p.APIKey)
		if len(p.Files) != 2 {
			return plan, fail("invalid_config", "缺少 Qwen 环境文件")
		}
		outputs[1], err = setEnv(p.Files[1].Content, "UNI_SWITCH_QWEN_API_KEY", p.APIKey)
	case "openclaw":
		v := p.piProvider(base)
		delete(v, "name")
		for _, m := range array(v["models"]) {
			m := object(m)
			if m["contextWindow"] == nil {
				m["contextWindow"] = 128000
			}
			delete(m, "maxTokens")
		}
		provider(0, "models.providers."+managedID, v)
		cur := Get(d, "agents.defaults.model")
		if object(cur) != nil {
			set(0, "agents.defaults.model.primary", ref)
		} else {
			set(0, "agents.defaults.model", ref)
		}
		if allow := object(Get(d, "agents.defaults.models")); len(allow) > 0 {
			for _, m := range p.Models {
				allow[managedID+"/"+m.ID] = Object{}
			}
		}
		if allow := array(Get(d, "agents.defaults.modelPolicy.allow")); len(allow) > 0 && !contains(allow, managedID+"/*") {
			set(0, "agents.defaults.modelPolicy.allow", append(allow, managedID+"/*"))
		}
	case "jcode":
		ms := p.piModels()
		for _, v := range ms {
			v := object(v)
			if c := v["contextWindow"]; c != nil {
				v["context_window"] = c
			}
			delete(v, "contextWindow")
			delete(v, "maxTokens")
		}
		provider(0, "providers."+managedID, Object{"type": "openai-compatible", "base_url": base, "api_key": p.APIKey, "models": ms})
		set(0, "provider.default_provider", managedID)
		set(0, "provider.default_model", p.Model)
	case "droid":
		ms := []any{}
		for _, m := range p.Models {
			c, o, im, _, _, _ := modelInfo(m)
			typ := "generic-chat-completion-api"
			if p.Protocol == "messages" {
				typ = "anthropic"
			}
			if p.Protocol == "responses" {
				typ = "openai"
			}
			v := Object{"model": m.ID, "id": "custom:" + managedID + "/" + m.ID, "displayName": p.Name + " · " + m.ID, "baseUrl": base, "apiKey": p.APIKey, "provider": typ, "noImageSupport": !im}
			if c > 0 {
				v["maxContextLimit"] = c
			}
			if o > 0 {
				v["maxOutputTokens"] = o
			}
			ms = append(ms, v)
		}
		err = replaceOwned(d, "customModels", "id", ms, func(v Object) bool { return strings.HasPrefix(text(v["id"]), "custom:"+managedID+"/") })
		set(0, "sessionDefaultSettings.model", "custom:"+ref)
	case "qoder", "qodercn":
		ms := []any{}
		for _, m := range p.Models {
			c, o, im, tools, re, eff := modelInfo(m)
			caps := Object{"tools": tools, "vision": im}
			if re && len(eff) > 0 {
				caps["thinking"] = Object{"modes": []string{"enabled"}, "supportsEffort": true, "supportedEffortLevels": eff, "requiresBudgetForEnabled": false}
			}
			v := Object{"model": m.ID, "displayName": m.ID, "capabilities": caps}
			if c > 0 {
				v["contextWindow"] = c
			}
			if o > 0 {
				v["maxOutputTokens"] = o
			}
			ms = append(ms, v)
		}
		provider(0, "providers."+managedID, Object{"displayName": p.Name, "protocol": "openai", "baseUrl": base, "apiKey": p.APIKey, "model": p.Model, "models": ms})
		set(0, "model.name", ref)
	case "pencil":
		provider(0, "providers."+managedID, p.piProvider(base))
	case "workbuddyai", "codebuddy":
		q := p
		q.Client = "workbuddy"
		q.Files = q.Files[:1]
		result, e := Build(q)
		if e != nil {
			return plan, e
		}
		if p.Client == "workbuddyai" {
			return result, nil
		}
		d0, e := jsonObject(result.Changes[0].After)
		if e != nil {
			return plan, e
		}
		docs[0] = d0
		set(1, "model", p.Model)
	case "openpencil":
		if p.Files[0].Content != nil && num(d["version"]) != 1 {
			return plan, fail("unsupported_schema", "OpenPencil 配置需要 version 1")
		}
		d["version"] = 1
		ids := []any{p.Model}
		for _, m := range p.Models {
			if m.ID != p.Model && len(ids) < 64 {
				ids = append(ids, m.ID)
			}
		}
		kind := "openai-compat"
		if p.Protocol == "messages" {
			kind = "anthropic"
		}
		entry := Object{"id": managedID, "preset": "custom", "display_name": p.Name, "kind": kind, "api_key": p.APIKey, "model": p.Model, "models": ids, "base_url": base, "enabled": true}
		for _, v := range array(d["builtin_agents"]) {
			old := object(v)
			if old["id"] == managedID {
				if old["enabled"] != nil {
					entry["enabled"] = old["enabled"]
				}
				if old["display_name"] != nil {
					entry["display_name"] = old["display_name"]
				}
			}
		}
		err = replaceOwned(d, "builtin_agents", "id", []any{entry}, func(v Object) bool { return v["id"] == managedID })
	case "liveagent":
		if d["models"] != nil {
			return plan, fail("unsupported_schema", "LiveAgent 配置包含不支持的旧 models 格式")
		}
		v := p.piProvider(base)
		v["type"] = "codex"
		if p.Protocol == "messages" {
			v["type"] = "claude_code"
		}
		v["requestFormat"] = v["api"]
		v["activeModels"] = nil
		ids := []any{}
		for _, m := range array(v["models"]) {
			o := object(m)
			o["displayName"] = o["name"]
			delete(o, "name")
			delete(o, "reasoning")
			ids = append(ids, o["id"])
		}
		v["modelOrder"] = ids
		provider(0, "providers."+managedID, v)
		set(0, "defaultProvider", managedID)
		set(0, "defaultModel", p.Model)
	case "raycast":
		ms := []any{}
		for _, m := range p.Models {
			c, _, im, tools, re, _ := modelInfo(m)
			v := Object{"id": m.ID, "name": m.ID, "abilities": Object{"vision": Object{"supported": im}, "system_message": Object{"supported": true}, "tools": Object{"supported": tools}, "reasoning_effort": Object{"supported": re}, "temperature": Object{"supported": !re}}}
			if c > 0 {
				v["context"] = c
			}
			ms = append(ms, v)
		}
		entry := Object{"id": managedID, "name": p.Name, "base_url": base, "api_keys": Object{managedID: p.APIKey}, "models": ms}
		err = replaceOwned(d, "providers", "id", []any{entry}, func(v Object) bool { return v["id"] == managedID })
	case "astrbot":
		entry := Object{"id": managedID, "provider": "openai", "type": "openai_chat_completion", "provider_type": "chat_completion", "enable": true, "key": []string{p.APIKey}, "api_base": base, "timeout": 120, "proxy": "", "custom_headers": Object{}}
		err = replaceOwned(d, "provider_sources", "id", []any{entry}, func(v Object) bool { return v["id"] == managedID })
		if err != nil {
			return plan, err
		}
		ms := []any{}
		for _, m := range p.Models {
			c, _, im, _, _, _ := modelInfo(m)
			mods := []string{"text", "tool_use"}
			if im {
				mods = append(mods, "image")
			}
			ms = append(ms, Object{"id": managedID + "/" + m.ID, "enable": true, "provider_source_id": managedID, "model": m.ID, "modalities": mods, "custom_extra_body": Object{}, "max_context_tokens": c})
		}
		err = replaceOwned(d, "provider", "id", ms, func(v Object) bool { return v["provider_source_id"] == managedID })
		if d["agent_runner"] != nil {
			set(0, "agent_runner.config.model.provider_id", ref)
		} else {
			set(0, "provider_settings.default_provider_id", ref)
		}
	case "antigravity":
		set(0, "gateway.enabled", true)
		set(0, "gateway.url", strings.TrimSuffix(base, "/v1"))
		set(0, "gateway.apiKey", p.APIKey)
		set(0, "gateway.wireProtocol", "openai")
		set(0, "models.defaultModelId", p.Model)
		ids := []any{}
		for _, m := range p.Models {
			ids = append(ids, m.ID)
		}
		set(0, "models.list", ids)
	case "atomcode":
		provider(0, "provider_accounts."+managedID, Object{"provider": "openai-compatible", "base_url": base, "api_key": p.APIKey})
		models, e := mapAt(d, "models")
		if e != nil {
			return plan, e
		}
		for _, m := range p.Models {
			c, o, im, _, _, eff := modelInfo(m)
			if c == 0 {
				c = 200000
			}
			v := Object{"account": managedID, "model": m.ID, "context_window": c, "supports_vision": im}
			if o > 0 {
				v["max_tokens"] = min(c, o)
			}
			if len(eff) > 0 {
				v["reasoning_effort_levels"] = eff
			}
			models[managedID+"/"+m.ID] = v
		}
		d["default_provider"] = ref
		d["default_model"] = ref
	default:
		err = buildOther(p, base, docs, outputs)
	}
	if err != nil {
		return plan, err
	}
	for i, f := range p.Files {
		out, ok := outputs[i]
		if !ok {
			var e error
			if f.Format == "alma" || f.Format == "hanako_provider" {
				out = Canonical(docs[i])
			} else if f.Format == "newmax" {
				out, e = renderConfig(File{Path: f.Path, Format: "json", Content: f.Content}, docs[i])
			} else {
				out, e = renderConfig(f, docs[i])
			}
			if e != nil {
				return plan, e
			}
		}
		if len(out) > 4<<20 || strings.ContainsRune(out, 0) {
			return plan, fail("invalid_config", "配置超过限制")
		}
		s := out
		plan.Changes = append(plan.Changes, edit.Change{Path: f.Path, Before: f.Content, After: &s})
	}
	return plan, nil
}
