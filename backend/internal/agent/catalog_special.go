package agent

import (
	"encoding/base64"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func launchScript(env Object, command string, args ...string) string {
	q := func(s string) string {
		if runtime.GOOS == "windows" {
			return powershellLiteral(s)
		}
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	var b strings.Builder
	if runtime.GOOS == "windows" {
		b.WriteString("# uni-switch generated launcher\n$ErrorActionPreference = 'Stop'\n")
	} else {
		b.WriteString("#!/bin/sh\n# uni-switch generated launcher\nset -eu\n")
	}
	keys := map[string]bool{}
	for k := range env {
		keys[k] = true
	}
	for _, k := range sortedKeys(keys) {
		if runtime.GOOS == "windows" {
			b.WriteString("$env:" + k + " = " + q(text(env[k])) + "\n")
		} else {
			b.WriteString("export " + k + "=" + q(text(env[k])) + "\n")
		}
	}
	if runtime.GOOS == "windows" {
		b.WriteString("& ")
	} else {
		b.WriteString("exec ")
	}
	b.WriteString(q(command))
	for _, arg := range args {
		b.WriteString(" " + q(arg))
	}
	if runtime.GOOS == "windows" {
		b.WriteString(" @args\nexit $LASTEXITCODE\n")
	} else {
		b.WriteString(" \"$@\"\n")
	}
	return b.String()
}

// PowerShell 5 reads a UTF-8 script without BOM as ANSI. Keep generated
// Windows launchers ASCII, including paths/values containing Chinese text.
func powershellLiteral(s string) string {
	for _, b := range []byte(s) {
		if b >= 128 {
			return "([System.Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('" + base64.StdEncoding.EncodeToString([]byte(s)) + "')))"
		}
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func ImportLink(client, name, base, key string) (string, error) {
	root := strings.TrimSuffix(base, "/v1")
	if client == "aionui" {
		v := url.Values{"api_key": {key}, "base_url": {root + "/v1"}, "name": {name}, "platform": {"custom"}}
		return "aionui://add-provider?" + v.Encode(), nil
	}
	if client != "cindy" && client != "lody" {
		return "", fail("invalid_client", "此客户端不支持链接导入")
	}
	ids := []string{"pi"}
	id := managedID
	if client == "lody" {
		u, e := url.Parse(root)
		if e != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Path != "" || u.RawQuery != "" {
			return "", fail("invalid_url", "Lody 需要专用本地入口")
		}
		key = "magpie-lody"
		id = "magpie"
		ids = []string{"pi", "dsh", "kimi-code", "grok", "bub"}
	}
	endpoint := func(protocol, base string, targets []string) Object {
		return Object{"protocol": protocol, "baseUrl": base, "targets": targets, "modelsUrl": root + "/v1/models"}
	}
	payload := Object{"kind": "custom", "id": id, "name": name, "auth": Object{"method": "apiKey", "apiKey": key}, "endpoints": []any{endpoint("anthropic-messages", root, []string{"claude-code"}), endpoint("openai-responses", root+"/v1", []string{"codex"}), endpoint("openai-chat", root+"/v1", ids)}}
	return client + "://provider/import?v=1&data=" + base64.RawURLEncoding.EncodeToString([]byte(Canonical(payload))), nil
}

func buildSpecial(p Input, base string, docs []Object, out map[int]string) error {
	d := docs[0]
	switch p.Client {
	case "gemini":
		if len(p.Files) != 2 {
			return fail("invalid_config", "缺少 Gemini 环境文件")
		}
		if e := put(d, "security.auth.selectedType", "gemini-api-key"); e != nil {
			return e
		}
		if e := put(d, "model.name", p.Model); e != nil {
			return e
		}
		s, e := setEnv(p.Files[1].Content, "GEMINI_API_KEY", p.APIKey)
		if e != nil {
			return e
		}
		out[1], e = setEnv(&s, "GOOGLE_GEMINI_BASE_URL", base)
		return e
	case "agy":
		if len(docs) != 2 {
			return fail("invalid_config", "缺少 agy 启动脚本")
		}
		d["modelProvider"] = "gemini"
		d["model"] = managedID + "/" + p.Model
		models, e := mapAt(d, "customModelsConfig")
		if e != nil {
			return e
		}
		ms, e := mapAt(models, "customModels")
		if e != nil {
			return e
		}
		for _, m := range p.Models {
			ms[managedID+"/"+m.ID] = Object{"apiProvider": "gemini", "modelName": m.ID}
		}
		out[1] = launchScript(Object{"GEMINI_API_KEY": p.APIKey, "GOOGLE_GEMINI_BASE_URL": base}, "agy", "--model", managedID+"/"+p.Model)
	case "claudescience":
		out[0] = clientLaunchScript(Object{"ANTHROPIC_BASE_URL": base, "ANTHROPIC_AUTH_TOKEN": p.APIKey, "ANTHROPIC_API_KEY": p.APIKey}, p.Client, "claude-science", "serve", "--detached")
	case "cursorlocal":
		out[0] = clientLaunchScript(Object{"CURSOR_LOCAL_AGENT_BASE_URL": base, "CURSOR_LOCAL_AGENT_API_KEY": p.APIKey}, p.Client, "cursor-private-inference")
	case "openchamber":
		if len(docs) != 4 {
			return fail("invalid_config", "缺少 OpenChamber 配置")
		}
		ref := managedID + "/" + p.Model
		d["defaultModel"] = ref
		if p.Files[1].Content != nil && num(docs[1]["version"]) != 1 {
			return fail("unsupported_schema", "OpenChamber preferences 需要 version 1")
		}
		docs[1]["version"] = 1
		entry := object(Get(docs[1], "fields.defaultModel"))
		if entry == nil {
			entry = Object{}
		}
		if entry["value"] != ref {
			entry["updatedAt"] = time.Now().UnixMilli()
		}
		entry["value"] = ref
		for _, v := range array(d["projects"]) {
			project := object(v)
			if text(project["defaultModel"]) != "" {
				project["defaultModel"] = ref
			}
		}
		if e := put(docs[1], "fields.defaultModel", entry); e != nil {
			return e
		}
		q := p
		q.Client = "opencode"
		q.Files = q.Files[2:3]
		plan, e := buildRegistered(q, base)
		if e != nil {
			return e
		}
		out[2] = *plan.Changes[0].After
		out[3] = launchScript(Object{"OPENCODE_CONFIG": p.Files[2].Path}, "openchamber")
	case "penguin":
		if e := mergeProvider(d, "providers."+managedID, Object{"base_url": base, "client_type": "openai-responses", "api_key": p.APIKey}); e != nil {
			return e
		}
		ms := []any{}
		for _, m := range p.Models {
			c, o, im, _, _, _ := modelInfo(m)
			v := Object{"provider": managedID, "model_id": m.ID, "display_name": m.ID, "vision": im}
			if c > 0 {
				v["context_window"] = c
			}
			if o > 0 {
				v["max_tokens"] = o
			}
			ms = append(ms, v)
		}
		if e := replaceOwned(d, "models", "provider", ms, func(v Object) bool { return v["provider"] == managedID }); e != nil {
			return e
		}
		d["default_model"] = Object{"provider": managedID, "model_id": p.Model}
	case "aside":
		if len(docs) != 2 {
			return fail("invalid_config", "缺少 Aside 模型文件")
		}
		if e := mergeProvider(docs[1], "providers."+managedID, p.piProvider(base)); e != nil {
			return e
		}
		return put(d, "defaultModel", Object{"provider": managedID, "modelId": p.Model})
	case "zed":
		if len(p.Files) > 1 {
			out[1] = p.APIKey
		}
		ms := []any{}
		for _, m := range p.Models {
			c, o, im, _, _, _ := modelInfo(m)
			if c == 0 {
				c = 128000
			}
			ms = append(ms, Object{"name": m.ID, "display_name": m.ID, "max_tokens": c + o, "max_output_tokens": o, "capabilities": Object{"tools": true, "images": im, "parallel_tool_calls": false, "prompt_cache_key": false, "chat_completions": true}})
		}
		if e := mergeProvider(d, "language_models.openai_compatible."+managedID, Object{"api_url": base, "available_models": ms}); e != nil {
			return e
		}
		return put(d, "agent.default_model", Object{"provider": managedID, "model": p.Model})
	case "zoocode":
		if len(docs) != 2 {
			return fail("invalid_config", "缺少 Zoo Code 导入文件")
		}
		d["zoo-code.autoImportSettingsPath"] = p.Files[1].Path
		docs[1]["providerProfiles"] = Object{"currentApiConfigName": managedID, "apiConfigs": Object{managedID: Object{"id": managedID, "apiProvider": "openai", "openAiBaseUrl": base, "openAiApiKey": p.APIKey, "openAiModelId": p.Model, "openAiStreamingEnabled": true}}}
	case "commandcode":
		if len(docs) != 2 {
			return fail("invalid_config", "缺少 Command Code 供应商文件")
		}
		models := Object{}
		for _, m := range p.Models {
			c, o, im, _, re, _ := modelInfo(m)
			models[m.ID] = Object{"name": m.ID, "contextWindow": c, "maxTokens": o, "reasoning": re, "input": func() []string {
				if im {
					return []string{"text", "image"}
				}
				return []string{"text"}
			}()}
		}
		if e := mergeProvider(docs[1], "provider."+managedID, Object{"name": p.Name, "api": "openai-completions", "baseURL": base, "apiKey": false, "models": models}); e != nil {
			return e
		}
		d["model"] = managedID + "/" + p.Model
		d["modelProvider"] = managedID
	case "fx":
		ms := Object{}
		for _, m := range p.Models {
			if len(ms) >= 256 {
				break
			}
			c, o, im, _, _, _ := modelInfo(m)
			v := Object{"supports_tool_use": true, "supports_vision": im}
			if c > 0 {
				v["context_window"] = c
			}
			if o > 0 && (c == 0 || o < c) {
				v["max_output_tokens"] = o
			}
			ms[m.ID] = v
		}
		if e := mergeProvider(d, "providers."+managedID, Object{"protocol": "openai-chat-completions", "base_url": base, "auth": Object{"type": "none"}, "tool_choice_mode": "send", "model_metadata": ms}); e != nil {
			return e
		}
		d["provider"] = managedID
		return put(d, "models."+managedID, p.Model)
	case "muse":
		if d["schema_version"] != nil && num(d["schema_version"]) != 1 {
			return fail("unsupported_schema", "Muse Code 配置需要 schema_version 1")
		}
		d["schema_version"] = 1
		d["endpoint_transport"] = Object{"base_url": base, "auth": "none"}
		d["model"] = p.Model
	case "empryo":
		ids := []any{}
		for _, m := range p.Models {
			ids = append(ids, m.ID)
		}
		if e := replaceOwned(d, "providers", "id", []any{Object{"id": managedID, "name": p.Name, "baseURL": base, "models": ids, "modelsAPI": base + "/models"}}, func(v Object) bool { return v["id"] == managedID }); e != nil {
			return e
		}
		d["defaultModel"] = managedID + "/" + p.Model
	case "openbot":
		if p.Files[0].Content != nil && num(d["version"]) != 1 {
			return fail("unsupported_schema", "OpenBot 配置需要 version 1")
		}
		d["version"] = 1
		ms := []any{}
		for _, m := range p.Models {
			if !openbotModelID(m.ID) {
				if m.ID == p.Model {
					return fail("unsupported_model_id", "OpenBot 不接受所选模型 ID 的字符或长度")
				}
				continue
			}
			if len(ms) >= 64 {
				break
			}
			c, _, _, _, _, _ := modelInfo(m)
			v := Object{"id": m.ID, "name": m.ID}
			if c >= 1024 && c <= 100000000 {
				v["contextWindow"] = c
			}
			ms = append(ms, v)
		}
		entry := Object{"id": managedID, "name": p.Name, "baseUrl": base, "models": ms, "secret": nil}
		for _, v := range array(d["providers"]) {
			if old := object(v); old["id"] == managedID && old["secret"] != nil {
				entry["secret"] = old["secret"]
			}
		}
		return replaceOwned(d, "providers", "id", []any{entry}, func(v Object) bool { return v["id"] == managedID })
	case "openmausbot":
		inst, e := mapAt(d, "instances")
		if e != nil {
			return e
		}
		if len(inst) == 0 {
			for id, driver := range map[string]string{"grok": "grokAgent", "kimi": "kimiAgent", "droid": "droidAgent", "cursor": "cursorAgent", "githubCopilot": "copilotAgent", "commandCode": "commandCodeAgent", "antigravity": "antigravityAgent", "opencodeGo": "opencodeGo", "openaiCompat": "openai-compat", "mistral": "mistral", "cerebras": "cerebras", "groq": "groq", "greenference": "greenference", "meta": "meta", "deepseek": "deepseekAgent", "museCode": "museCode", "codex": "codex", "claude": "claudeAgent", "qwen": "qwenAgent", "hermes": "hermesAgent", "cline": "clineAgent", "pi": "piAgent"} {
				inst[id] = Object{"driver": driver}
			}
			inst["chatgpt"] = Object{"driver": "codex", "displayName": "ChatGPT plan", "config": Object{"authMode": "chatgpt-plan"}}
			inst["claudeApi"] = Object{"driver": "claudeAgent", "displayName": "Claude (API key)", "access": "api", "icon": Object{"kind": "preset", "preset": "anthropic"}, "config": Object{"requireApiKey": true}}
			inst["xaiApi"] = Object{"driver": "grok", "displayName": "xAI", "access": "api", "icon": Object{"kind": "preset", "preset": "xai"}}
			for id, info := range map[string][4]string{"openai": {"OpenAI", "openai", "https://api.openai.com/v1", "OMB_OPENAI_API_KEY"}, "openrouter": {"OpenRouter", "openrouter", "https://openrouter.ai/api/v1", "OMB_OPENROUTER_API_KEY"}} {
				config := Object{"url": info[2], "apiKeyEnv": info[3]}
				if id == "openai" {
					config["catalog"] = "openai"
				}
				inst[id] = Object{"driver": "openai-compat", "displayName": info[0], "access": "api", "icon": Object{"kind": "preset", "preset": info[1]}, "config": config}
			}
		}
		ids := []string{p.Model}
		for _, m := range p.Models {
			if m.ID != p.Model {
				ids = append(ids, m.ID)
			}
		}
		inst[managedID] = Object{"driver": "claudeAgent", "displayName": p.Name, "access": "custom", "config": Object{"configDir": filepath.Join(filepath.Dir(p.Files[0].Path), "providers", managedID), "requireApiKey": true, "endpointModels": ids}, "environment": Object{"ANTHROPIC_BASE_URL": base, "ANTHROPIC_AUTH_TOKEN": p.APIKey, "ANTHROPIC_API_KEY": p.APIKey, "ANTHROPIC_MODEL": p.Model}}
	case "kirocrew":
		if len(docs) != 2 {
			return fail("invalid_config", "缺少 Kiro Crew 本地配置快照")
		}
		if Get(docs[1], "agent.acp_backend") != nil {
			return fail("harness_pinned", "Kiro Crew 的 config.local.json 已固定后端，请先在客户端确认并调整")
		}
		if p.Harness == "" {
			return fail("harness_required", "请先把相同供应商配置到 Codex、Claude Code、OpenCode、Pi 或 Goose")
		}
		return put(d, "agent.acp_backend", p.Harness)
	default:
		return fail("invalid_client", "未知客户端")
	}
	return nil
}

func openbotModelID(id string) bool {
	if len(id) == 0 || len(managedID)+1+len(id) > 160 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:/[],=-", r)) {
			return false
		}
	}
	return true
}
