package agent

func buildResource(p Input, base string, docs []Object, out map[int]string) error {
	d := docs[0]
	switch p.Client {
	case "newmax":
		ids := []any{}
		windows := Object{}
		for _, m := range p.Models {
			if m.ID == p.Model {
				ids = append(ids, m.ID)
			}
		}
		for _, m := range p.Models {
			if len(ids) < 20 && !contains(ids, m.ID) {
				ids = append(ids, m.ID)
			}
			c, _, _, _, _, _ := modelInfo(m)
			if c > 0 {
				windows[m.ID] = c
			}
		}
		const id = "uni-switch-gateway"
		entry := Object{"id": id, "name": p.Name, "baseUrl": base, "apiKey": p.APIKey, "apiKeys": []string{p.APIKey}, "apiKeyRotationEnabled": false, "apiKeyField": "ANTHROPIC_AUTH_TOKEN", "apiFormat": "anthropic", "models": ids, "enabledModels": ids, "enabled": true, "canDelete": true, "modelContextWindows": windows}
		for _, v := range array(d["providers"]) {
			old := object(v)
			if old["id"] == id {
				for k, x := range old {
					if _, ok := entry[k]; !ok || k == "name" || k == "enabled" || k == "canDelete" {
						entry[k] = x
					}
				}
			}
		}
		if e := replaceOwned(d, "providers", "id", []any{entry}, func(v Object) bool { return v["id"] == id }); e != nil {
			return e
		}
		order, e := listAt(d, "providerOrder")
		if e != nil {
			return e
		}
		if len(order) == 0 {
			order = []any{"default"}
		}
		if !contains(order, id) {
			order = append(order, id)
		}
		d["providerOrder"] = order
	case "alma":
		old := object(d["provider"])
		if old != nil && p.Previous == nil {
			return fail("config_collision", "Alma 已有同名 uni-switch 供应商，可能含不可恢复的加密密钥；请先在客户端更名该条目")
		}
		entry := Object{"name": managedID, "type": "openai", "baseURL": base, "apiKey": p.APIKey, "enabled": true}
		ids := []any{}
		models := []any{}
		for _, m := range p.Models {
			c, o, im, _, re, eff := modelInfo(m)
			caps := Object{"functionCalling": true}
			for _, v := range array(old["availableModels"]) {
				v := object(v)
				if v["id"] == m.ID {
					for k, x := range object(v["capabilityOverrides"]) {
						caps[k] = x
					}
				}
			}
			if c > 0 {
				caps["contextWindow"] = c
			}
			if o > 0 {
				caps["maxOutputTokens"] = o
			}
			if im {
				caps["vision"] = true
			}
			if re {
				caps["reasoning"] = true
			}
			if len(eff) > 0 {
				levels := []any{}
				for _, l := range eff {
					if contains([]any{"low", "medium", "high", "xhigh", "max"}, text(l)) {
						levels = append(levels, l)
					}
				}
				if len(levels) > 0 {
					caps["reasoningLevels"] = levels
				}
			}
			ids = append(ids, m.ID)
			models = append(models, Object{"id": m.ID, "name": m.ID, "capabilityOverrides": caps})
		}
		entry["models"] = ids
		entry["availableModels"] = models
		d["provider"] = entry
		d["defaultModel"] = managedID + ":" + p.Model
	case "hanako":
		if len(p.Files) != 2 {
			return fail("invalid_config", "缺少 OpenHanako agent 配置")
		}
		if d["sealed"] == true && object(d["provider"]) != nil && p.Previous == nil {
			return fail("encrypted_config", "已有同名加密供应商，无法取得完整恢复凭据；请先在 OpenHanako 移除此条目")
		}
		models := []any{}
		for _, m := range p.Models {
			c, o, im, _, re, eff := modelInfo(m)
			models = append(models, Object{"id": m.ID, "name": m.ID, "context": c, "maxOutput": o, "image": im, "reasoning": re, "xhigh": contains(eff, "xhigh")})
		}
		d["provider"] = Object{"display_name": p.Name, "base_url": base, "api": "openai-completions", "api_key": p.APIKey, "models": models}
		d["deleted"] = false
		d["plugin"] = nil
		if d["sealed"] == nil {
			d["sealed"] = false
		}
		out[1] = Canonical(Object{"provider": managedID, "id": p.Model})
	}
	return nil
}
