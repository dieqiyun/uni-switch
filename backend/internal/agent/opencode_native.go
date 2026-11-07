package agent

// OpenCode 2's providers map takes precedence over its legacy provider map.
// Update an existing native entry in its own schema, as Magpie does.
func openCodeNative(legacy Object) Object {
	settings := Clone(object(legacy["options"]))
	cache := settings["setCacheKey"]
	delete(settings, "setCacheKey")
	out := Object{"name": legacy["name"], "package": "aisdk:" + text(legacy["npm"]), "settings": settings}
	if cache != nil {
		out["compatibility"] = Object{"supportsPromptCacheKey": cache}
	}
	models := Object{}
	for id, v := range object(legacy["models"]) {
		m := object(v)
		next := Object{"name": m["name"]}
		if mods := object(m["modalities"]); mods != nil {
			next["capabilities"] = Object{"tools": true, "input": mods["input"], "output": mods["output"]}
		}
		if m["limit"] != nil {
			next["limit"] = m["limit"]
		}
		variants := []any{}
		keys := map[string]bool{}
		for k := range object(m["variants"]) {
			keys[k] = true
		}
		for _, k := range sortedKeys(keys) {
			variants = append(variants, Object{"id": k, "settings": m["variants"].(map[string]any)[k]})
		}
		next["variants"] = variants
		models[id] = next
	}
	out["models"] = models
	return out
}

func keepOpenCodeOptions(old, want Object, modern bool) {
	keys := []string{"options"}
	if modern {
		keys = []string{"settings", "compatibility"}
	}
	for _, key := range keys {
		merged := Object{}
		for k, v := range object(old[key]) {
			merged[k] = v
		}
		for k, v := range object(want[key]) {
			if (k == "setCacheKey" || k == "supportsPromptCacheKey") && merged[k] != nil {
				continue
			}
			merged[k] = v
		}
		if len(merged) > 0 {
			want[key] = merged
		}
	}
}
