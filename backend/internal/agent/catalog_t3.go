package agent

import (
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"strings"
)

func buildT3(p Input, base string, docs []Object) error {
	if len(docs) != 2 {
		return fail("invalid_config", "缺少 T3 Code 的 Codex 模型目录")
	}
	d := docs[0]
	custom := []any{}
	catalog := []any{}
	for i, m := range p.Models {
		c, _, im, _, _, eff := modelInfo(m)
		if c <= 0 {
			c = 256000
		}
		slug := m.ID
		if strings.Contains(m.ID, "claude-") && strings.Contains(m.ID, ".") {
			slug = gateway.LegacyAlias(m.ID)
		}
		if c >= 1000000 {
			slug += "[1m]"
		}
		v := Object{"slug": slug, "name": m.ID}
		descriptors := []any{}
		options := []any{}
		for _, e := range eff {
			if contains([]any{"low", "medium", "high", "xhigh", "max"}, text(e)) {
				options = append(options, Object{"id": e, "label": e, "isDefault": e == "medium"})
			}
		}
		if len(options) > 0 {
			if !contains(eff, "medium") {
				object(options[0])["isDefault"] = true
			}
			descriptors = append(descriptors, Object{"id": "effort", "label": "Reasoning", "type": "select", "options": options})
		}
		if len(eff) > 0 {
			descriptors = append(descriptors, Object{"id": "thinking", "label": "Thinking", "type": "boolean", "currentValue": true})
		}
		if len(descriptors) > 0 {
			v["capabilities"] = Object{"optionDescriptors": descriptors}
		}
		custom = append(custom, v)
		levels := []any{}
		var def any
		for _, e := range eff {
			levels = append(levels, Object{"effort": e, "description": e})
			if def == nil || e == "medium" {
				def = e
			}
		}
		input := []string{"text"}
		if im {
			input = append(input, "image")
		}
		var patch any
		if p.Upstream == "anthropic" {
			patch = "freeform"
		}
		entry := Object{"slug": m.ID, "display_name": m.ID, "description": "uni-switch", "visibility": "list", "supported_in_api": true, "priority": i + 1, "default_reasoning_level": def, "supported_reasoning_levels": levels, "shell_type": "shell_command", "availability_nux": nil, "upgrade": nil, "base_instructions": baseInstructions, "model_messages": nil, "support_verbosity": false, "default_verbosity": nil, "apply_patch_tool_type": patch, "supports_reasoning_summary_parameter": false, "default_reasoning_summary": "none", "context_window": c, "max_context_window": c, "auto_compact_token_limit": c, "truncation_policy": Object{"mode": "tokens", "limit": 10000}, "experimental_supported_tools": []any{}, "input_modalities": input, "supports_parallel_tool_calls": false}
		if m.ID == p.Model {
			entry["priority"] = 0
		}
		speed(entry, false)
		catalog = append(catalog, entry)
	}
	copyPaths := func(driver string, config Object) {
		for _, key := range []string{"binaryPath", "homePath"} {
			v := Get(d, "providerInstances."+driver+".config."+key)
			if v == nil {
				v = Get(d, "providers."+driver+"."+key)
			}
			if text(v) != "" {
				config[key] = v
			}
		}
	}
	claudeConfig := Object{"customModels": custom}
	copyPaths("claudeAgent", claudeConfig)
	env := []any{Object{"name": "ANTHROPIC_BASE_URL", "value": strings.TrimSuffix(base, "/v1"), "sensitive": false}, Object{"name": "ANTHROPIC_AUTH_TOKEN", "value": p.APIKey, "sensitive": true}}
	for _, v := range array(Get(d, "providerInstances."+managedID+".environment")) {
		k := text(object(v)["name"])
		if k != "" && !strings.HasPrefix(k, "ANTHROPIC_") && !strings.HasPrefix(k, "CLAUDE_CODE_USE_") {
			env = append(env, v)
		}
	}
	claude := Object{"driver": "claudeAgent", "displayName": p.Name, "enabled": true, "environment": env, "config": claudeConfig}
	arg := func(k, v string) string {
		// JSON escaping also forms a TOML basic string, except DEL must
		// be escaped explicitly. Then quote that assignment for T3's tokenizer.
		value := strings.ReplaceAll(Canonical(v), "\x7f", `\u007f`)
		return `-c "` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`").Replace(k+"="+value) + `"`
	}
	args := []string{arg("model_provider", managedID), arg("model", p.Model), arg("model_providers."+managedID+".name", p.Name), arg("model_providers."+managedID+".base_url", strings.TrimSuffix(base, "/v1")+"/v1"), arg("model_providers."+managedID+".wire_api", "responses"), arg("model_providers."+managedID+".experimental_bearer_token", p.APIKey), arg("model_catalog_json", p.Files[1].Path)}
	codexConfig := Object{"launchArgs": strings.Join(args, " ")}
	copyPaths("codex", codexConfig)
	codex := Object{"driver": "codex", "displayName": p.Name + " · Codex", "enabled": true, "config": codexConfig}
	for id, want := range map[string]Object{managedID: claude, managedID + "-codex": codex} {
		old := object(Get(d, "providerInstances."+id))
		for _, k := range []string{"displayName", "enabled"} {
			if v, ok := old[k]; ok {
				want[k] = v
			}
		}
		for k, v := range object(old["config"]) {
			if _, ok := object(want["config"])[k]; !ok {
				object(want["config"])[k] = v
			}
		}
		if e := mergeProvider(d, "providerInstances."+id, want); e != nil {
			return e
		}
	}
	docs[1]["models"] = catalog
	return nil
}
