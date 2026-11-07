package agent

import (
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"path/filepath"
	"strings"
)

func buildOther(p Input, base string, docs []Object, outputs map[int]string) error {
	d := docs[0]
	ref := managedID + "/" + p.Model
	set := func(i int, k string, v any) error {
		if i >= len(docs) {
			return fail("invalid_config", "配置快照不完整")
		}
		return put(docs[i], k, v)
	}
	switch p.Client {
	case "vscode", "vscodeinsiders", "vscodium":
		if len(docs) < 2 {
			return fail("invalid_config", "缺少编辑器模型文件")
		}
		vendor, e := vscodeVendor(p.Client, filepath.Dir(p.Files[0].Path))
		if e != nil {
			return e
		}
		ms := []any{}
		for _, m := range p.Models {
			c, o, im, _, _, eff := modelInfo(m)
			if c == 0 {
				c = 128000
			}
			if o == 0 {
				o = 8192
			}
			o = min(o, c/4)
			v := Object{"id": m.ID, "name": m.ID, "url": base + "/chat/completions", "toolCalling": true, "vision": im, "contextWindow": c, "maxOutputTokens": o, "requestHeaders": Object{"Authorization": "Bearer " + p.APIKey}}
			if vendor == "customoai" {
				delete(v, "contextWindow")
				v["maxInputTokens"] = c - o
				v["requestHeaders"] = Object{"x-api-key": p.APIKey}
			}
			if len(eff) > 0 {
				v["supportsReasoningEffort"] = eff
				v["reasoningEffortFormat"] = "chat-completions"
			}
			ms = append(ms, v)
		}
		group := Object{"name": managedID, "vendor": vendor, "models": ms}
		if vendor == "customendpoint" {
			group["apiType"] = "chat-completions"
		}
		for i, f := range p.Files {
			if f.Format == "jsonc_array" {
				want := Clone(group)
				for _, v := range array(docs[i]["items"]) {
					old := object(v)
					if old["name"] == managedID {
						for k, x := range old {
							if _, ok := want[k]; !ok {
								want[k] = x
							}
						}
					}
				}
				if e := replaceOwned(docs[i], "items", "name", []any{want}, func(v Object) bool { return v["name"] == managedID }); e != nil {
					return e
				}
			} else {
				docs[i]["chat.defaultModel"] = p.Model
			}
		}
	case "copilotjetbrains":
		models := Object{}
		for _, m := range p.Models {
			c, o, im, _, _, _ := modelInfo(m)
			if c == 0 {
				c = 128000
			}
			if o == 0 {
				o = 16000
			}
			models[m.ID] = Object{"deploymentUrl": base + "/chat/completions", "isRegistered": true, "isCustomModel": true, "modelCapabilities": Object{"name": m.ID, "maxInputTokens": c, "maxOutputTokens": min(o, c/4), "toolCalling": true, "vision": im}}
		}
		d[managedID+"-provider-config"] = Object{"groupName": managedID, "apiType": "chatCompletions"}
		d[managedID+"-api-key"] = p.APIKey
		d[managedID+"-models-config"] = models
	case "snow":
		if len(docs) != 3 {
			return fail("invalid_config", "缺少 Snow 配置快照")
		}
		profile := Object{}
		for k, v := range d {
			profile[k] = v
		}
		if p.Files[1].Content != nil {
			profile = docs[1]
		}
		sc, e := mapAt(profile, "snowcfg")
		if e != nil {
			return e
		}
		delete(sc, "oauth")
		c, o, im := 200000, 64000, false
		for _, m := range p.Models {
			if m.ID == p.Model {
				a, b, i, _, _, _ := modelInfo(m)
				if a > 0 {
					c = a
				}
				if b > 0 {
					o = b
				}
				im = i
			}
		}
		for k, v := range (Object{"baseUrl": base, "baseUrlMode": "base", "apiKey": p.APIKey, "requestMethod": "chat", "advancedModel": p.Model, "basicModel": p.Model, "maxContextTokens": c, "maxTokens": min(c, o), "supportsVision": im}) {
			sc[k] = v
		}
		docs[0] = profile
		docs[1] = profile
		docs[2]["activeProfile"] = managedID
	case "minimax":
		ms := Object{}
		flat := gateway.FlatModelIDs(p.Models)
		for _, m := range p.Models {
			c, o, im, _, re, eff := modelInfo(m)
			v := Object{"name": m.ID, "reasoning": re, "enabled": true}
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
			if im {
				v["capabilities"] = Object{"support_image": true}
			}
			if len(eff) > 0 {
				v["thinking"] = Object{"effortOptions": eff}
			}
			key := flat[m.ID]
			for k, x := range object(Get(d, "custom_provider."+managedID+".models."+key)) {
				if _, ok := v[k]; !ok {
					v[k] = x
				}
			}
			ms[key] = v
		}
		if e := mergeProvider(d, "custom_provider."+managedID, Object{"name": p.Name, "kind": "custom", "enabled": true, "api": "anthropic-messages", "options": Object{"apiKey": p.APIKey, "baseURL": base, "authMode": "api-key"}, "models": ms}); e != nil {
			return e
		}
		d["defaultModel"] = "custom_provider:" + managedID + "/" + flat[p.Model]
	case "cline":
		if len(docs) < 2 {
			return fail("invalid_config", "缺少 Cline 配置")
		}
		for i := 0; i < 2; i++ {
			if p.Files[i].Content != nil && num(docs[i]["version"]) != 1 {
				return fail("unsupported_schema", "Cline 配置需要 version 1")
			}
			docs[i]["version"] = 1
		}
		if d["modes"] == nil {
			d["modes"] = Object{}
		}
		settings := Object{"provider": "openai-compatible", "apiKey": p.APIKey, "model": p.Model, "baseUrl": base}
		if e := set(0, "providers.openai-compatible", Object{"settings": settings, "tokenSource": "manual"}); e != nil {
			return e
		}
		d["lastUsedProvider"] = "openai-compatible"
		ms := Object{}
		for _, m := range p.Models {
			c, o, im, _, re, _ := modelInfo(m)
			caps := []string{"streaming", "tools"}
			if im {
				caps = append(caps, "images")
			}
			if re {
				caps = append(caps, "reasoning")
			}
			v := Object{"id": m.ID, "name": m.ID, "capabilities": caps}
			if c > 0 {
				v["contextWindow"] = c
			}
			if o > 0 {
				v["maxTokens"] = o
			}
			ms[m.ID] = v
		}
		if e := set(1, "providers.openai-compatible", Object{"provider": Object{"name": p.Name, "baseUrl": base, "defaultModelId": p.Model}, "models": ms}); e != nil {
			return e
		}
		if len(docs) == 4 {
			for _, mode := range []string{"act", "plan"} {
				docs[2][mode+"ModeApiProvider"] = "openai"
				docs[2][mode+"ModeOpenAiModelId"] = p.Model
				delete(docs[2], mode+"ModeOpenAiModelInfo")
			}
			docs[2]["openAiBaseUrl"] = base
			docs[3]["openAiApiKey"] = p.APIKey
		}
	case "qwenpaw":
		if len(docs) != 2 {
			return fail("invalid_config", "缺少 QwenPaw 凭据文件")
		}
		if strings.HasPrefix(text(docs[1]["api_key"]), "ENC:") {
			return fail("encrypted_config", "QwenPaw 已加密此供应商凭据，请通过客户端管理该条目")
		}
		ms := []any{}
		for _, m := range p.Models {
			c, o, im, _, _, eff := modelInfo(m)
			v := Object{"id": m.ID, "name": m.ID, "source": "user", "supports_image": im}
			if c > 0 {
				v["context_length"] = c
			}
			if o > 0 {
				v["max_output_length"] = o
			}
			if len(eff) > 0 {
				v["thinking_param_style"] = "effort"
				v["reasoning_effort_options"] = eff
			}
			ms = append(ms, v)
		}
		for k, v := range (Object{"id": managedID, "name": p.Name, "base_url": base, "api_key": p.APIKey, "chat_model": "OpenAIChatModel", "is_custom": true, "extra_models": ms}) {
			docs[1][k] = v
		}
		if docs[1]["snapshot_schema_version"] == nil {
			docs[1]["snapshot_schema_version"] = 2
			docs[1]["models"] = []any{}
			docs[1]["enabled"] = true
		}
		d["active_model"] = Object{"provider_id": managedID, "model": p.Model}
	case "ante":
		ms := []any{}
		for _, m := range p.Models {
			c, o, _, _, _, eff := modelInfo(m)
			v := Object{"id": m.ID}
			if c > 0 {
				v["context_limit"] = c
				if o >= c {
					o = c - 1
				}
			}
			if o > 0 {
				v["max_tokens"] = o
			}
			if len(eff) > 0 {
				v["supported_efforts"] = eff
			}
			ms = append(ms, v)
		}
		if e := mergeProvider(d, "providers."+managedID, Object{"display_name": p.Name, "base_url": base, "wire_style": "OpenAiCompatible", "preferred_models": ms}); e != nil {
			return e
		}
		if e := set(1, "provider", managedID); e != nil {
			return e
		}
		if e := set(1, "model", p.Model); e != nil {
			return e
		}
	case "grok":
		if e := set(0, "features.campaigns", false); e != nil {
			return e
		}
		models, e := mapAt(d, "model")
		if e != nil {
			return e
		}
		for _, m := range p.Models {
			c, _, _, _, _, eff := modelInfo(m)
			v := Object{"model": m.ID, "name": m.ID, "base_url": base, "api_key": p.APIKey, "api_backend": "chat_completions"}
			if c > 0 {
				v["context_window"] = c
			}
			if len(eff) > 0 {
				v["reasoning_efforts"] = eff
			}
			models[managedID+"/"+m.ID] = v
		}
		return set(0, "models.default", ref)
	case "grokapp":
		ms := []any{}
		for _, m := range p.Models {
			c, _, im, _, _, _ := modelInfo(m)
			ms = append(ms, Object{"id": m.ID, "name": m.ID, "contextWindow": c, "supportsVision": im})
		}
		return mergeProvider(d, "model."+managedID, Object{"model": p.Model, "base_url": base, "name": p.Name, "api_key": p.APIKey, "api_backend": "chat_completions", "app_provider_mode": "generic", "app_models": strings.TrimSpace(pretty(ms))})
	case "t3code":
		return buildT3(p, base, docs)
	case "reasonix":
		ids := []any{}
		overrides := Object{}
		for _, m := range p.Models {
			c, o, im, _, _, eff := modelInfo(m)
			ids = append(ids, m.ID)
			overrides[m.ID] = Object{"context_window": c, "max_output_tokens": o, "vision": im, "supported_efforts": eff}
		}
		entry := Object{"name": managedID, "kind": "openai", "base_url": base, "api_key_env": "UNI_SWITCH_REASONIX_API_KEY", "default": p.Model, "models": ids, "reasoning_protocol": "openai", "no_proxy": true, "model_overrides": overrides}
		if e := replaceOwned(d, "providers", "name", []any{entry}, func(v Object) bool { return v["name"] == managedID }); e != nil {
			return e
		}
		d["default_model"] = ref
		if list := array(Get(d, "desktop.provider_access")); len(list) > 0 && !contains(list, managedID) {
			if e := put(d, "desktop.provider_access", append(list, managedID)); e != nil {
				return e
			}
		}
		if len(p.Files) != 2 {
			return fail("invalid_config", "缺少 Reasonix 环境文件")
		}
		out, e := setEnv(p.Files[1].Content, "UNI_SWITCH_REASONIX_API_KEY", p.APIKey)
		if e != nil {
			return e
		}
		outputs[1] = out
	case "air", "airide":
		if len(docs) != 2 {
			return fail("invalid_config", "缺少 ACP 配置文件")
		}
		q := p
		q.Client = "opencode"
		q.Files = q.Files[1:]
		plan, e := buildRegistered(q, base)
		if e != nil {
			return e
		}
		native := File{Path: p.Files[1].Path, Format: "jsonc", Content: plan.Changes[0].After}
		opencode, e := parseConfig(native)
		if e != nil {
			return e
		}
		opencode["enabled_providers"] = []any{managedID}
		opencode["small_model"] = ref
		outputs[1], e = renderConfig(native, opencode)
		if e != nil {
			return e
		}
		return mergeProvider(d, "agent_servers."+managedID, Object{"command": openCodeExecutable(), "args": []string{"acp"}, "env": Object{"OPENCODE_CONFIG": filepath.Clean(p.Files[1].Path)}})
	default:
		return buildSpecial(p, base, docs, outputs)
	}
	return nil
}
