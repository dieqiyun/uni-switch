package agent

import (
	"path/filepath"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

func strptr(s string) *string { return &s }
func Same(a, b *string) bool  { return a == nil && b == nil || a != nil && b != nil && *a == *b }
func Get(m Object, path string) any {
	parts := strings.Split(path, ".")
	var v any = m
	for _, p := range parts {
		v = object(v)[p]
	}
	return v
}
func Set(m Object, path string, v any, remove bool) error {
	parts := strings.Split(path, ".")
	for _, p := range parts[:len(parts)-1] {
		if m[p] == nil {
			if remove {
				return nil
			}
			m[p] = Object{}
		}
		m = object(m[p])
		if m == nil {
			return fail("invalid_shape", "配置父级必须是对象")
		}
	}
	k := parts[len(parts)-1]
	if remove {
		delete(m, k)
	} else {
		m[k] = v
	}
	return nil
}
func Parse(format string, text *string) (Object, error) {
	if format == "yaml" {
		return parseConfig(File{Format: format, Content: text})
	}
	if format == "toml" {
		s := ""
		if text != nil {
			s = strings.TrimPrefix(*text, "\ufeff")
		}
		return edit.TOML(s)
	}
	return jsonObject(text)
}
func Mutate(format string, raw *string, mutations []Mutation) (string, error) {
	if format == "yaml" {
		f := File{Format: format, Content: raw}
		doc, e := parseConfig(f)
		if e != nil {
			return "", e
		}
		for _, m := range mutations {
			if e = Set(doc, strings.Join(m.Path, "."), m.Value, m.Remove); e != nil {
				return "", e
			}
		}
		return renderConfig(f, doc)
	}
	if format == "toml" {
		s := ""
		if raw != nil {
			s = *raw
		}
		var e error
		for _, m := range mutations {
			s, e = edit.TOMLSet(s, m.Path, m.Value, m.Remove)
			if e != nil {
				return "", e
			}
		}
		return s, nil
	}
	doc, e := jsonObject(raw)
	if e != nil {
		return "", e
	}
	for _, m := range mutations {
		if e = Set(doc, strings.Join(m.Path, "."), m.Value, m.Remove); e != nil {
			return "", e
		}
	}
	return pretty(doc), nil
}
func displayMutation(doc Object) (Mutation, error) {
	p, e := Primary(PrimaryInput{Client: "codex", Config: doc})
	if e != nil {
		return Mutation{}, e
	}
	for _, m := range p.Mutations {
		if strings.Join(m.Path, ".") == DisplayKey {
			return m, nil
		}
	}
	return Mutation{}, fail("invalid_reasoning_display", "思考强度显示设置无效")
}
func DesktopModels(ids []string, mapped bool, models []gateway.Model) ([]any, error) {
	result := []any{}
	for _, id := range ids {
		name, large := splitContext(id)
		original := name
		if mapped {
			m := gateway.Model{ID: id, Enabled: true}
			for _, v := range models {
				if v.ID == id {
					m = v
					break
				}
			}
			name = gateway.DesktopID(m)
		}
		at := -1
		for i, v := range result {
			if text(v) == name || text(object(v)["name"]) == name {
				at = i
				break
			}
		}
		if at >= 0 {
			if large {
				if object(result[at]) == nil {
					result[at] = Object{"name": name}
				}
				object(result[at])["supports1m"] = true
			}
			continue
		}
		var v any = name
		if mapped {
			v = Object{"name": name, "labelOverride": original}
		} else if large {
			v = Object{"name": name}
		}
		if large {
			object(v)["supports1m"] = true
		}
		result = append(result, v)
	}
	return result, nil
}
func desktopValid(s string) bool {
	s, _ = splitContext(s)
	s = strings.TrimPrefix(strings.ToLower(s), "anthropic/")
	for _, role := range []string{"sonnet", "opus", "haiku", "fable"} {
		if tail, ok := strings.CutPrefix(s, "claude-"+role+"-"); ok && tail != "" {
			return true
		}
	}
	return strings.HasPrefix(s, "mythos-uni-switch-") || strings.HasPrefix(s, "anthropic/uni-switch-") || strings.HasPrefix(s, "uni-switch-")
}
func PlanPrimary(client, dir string, p Provider, cache string) ([]ManagedFile, error) {
	family := "claude"
	if client == "codex" {
		family = "codex"
	}
	if p.Summary["family"] != family {
		return nil, fail("wrong_family", "该配置不属于此客户端")
	}
	rows := array(p.Options()["models"])
	if client == "codex" && len(rows) > 0 {
		enabled := false
		for _, m := range rows {
			enabled = enabled || object(m)["enabled"] != false
		}
		if !enabled {
			return nil, fail("no_enabled_models", "请至少启用一个模型")
		}
	}
	if client == "claude_desktop" {
		for _, id := range p.IDs() {
			if !desktopValid(id) {
				return nil, fail("desktop_model", "Claude 桌面端需要可识别的完整 Claude 模型 ID，其他模型需要代理转换")
			}
		}
	}
	specs, e := Specifications(client, dir, false, false)
	if e != nil {
		return nil, e
	}
	files := []ManagedFile{}
	for _, s := range specs {
		if e = CheckPath(s.Path); e != nil {
			return nil, e
		}
		before, e := edit.Read(s.Path)
		if e != nil {
			return nil, e
		}
		keys := s.Keys
		if client == "codex" && p.Upstream() != "anthropic" {
			keys = without(keys, "web_search")
		}
		after := ""
		if s.Format == "catalog" {
			catalog, e := ModelCatalog(p, cache)
			if e != nil {
				return nil, e
			}
			after = pretty(catalog)
		} else {
			doc, e := Parse(s.Format, before)
			if e != nil {
				return nil, e
			}
			auth := Object{}
			if client == "codex" {
				raw, _ := edit.Read(filepath.Join(dir, "auth.json"))
				if v, e := jsonObject(raw); e == nil {
					auth = v
				}
			}
			kind := s.Format
			if filepath.Base(s.Path) == "claude_desktop_config.json" {
				kind = "deployment"
			}
			o := p.Options()
			strPtr := func(v any) *string {
				s, ok := v.(string)
				if !ok {
					return nil
				}
				return &s
			}
			boolPtr := func(v any) *bool {
				b, ok := v.(bool)
				if !ok {
					return nil
				}
				return &b
			}
			intPtr := func(v any) *int64 {
				if v == nil {
					return nil
				}
				n := int64(num(v))
				return &n
			}
			plan, e := Primary(PrimaryInput{Client: client, Kind: kind, Name: text(p.Summary["name"]), Model: p.Model(), IDs: p.IDs(), BaseURL: text(p.Summary["baseUrl"]), APIKey: p.APIKey, AuthMode: text(p.Summary["authMode"]), Protocol: p.Upstream(), Effort: strPtr(p.Summary["reasoningEffort"]), Fast: boolPtr(o["fastMode"]), Context: intPtr(o["contextWindow"]), Compact: intPtr(o["autoCompactTokenLimit"]), CatalogPath: filepath.Join(dir, "uni-switch-models.json"), ProfileID: ProfileID, Config: doc, Auth: auth})
			if e != nil {
				return nil, e
			}
			keys = append(keys, plan.ExtraKeys...)
			after, e = Mutate(s.Format, before, plan.Mutations)
			if e != nil {
				return nil, e
			}
		}
		files = append(files, ManagedFile{s.Path, s.Format, keys, before, &after})
	}
	return files, nil
}
func without(keys []string, omit ...string) []string {
	out := []string{}
	for _, k := range keys {
		keep := true
		for _, v := range omit {
			keep = keep && k != v
		}
		if keep {
			out = append(out, k)
		}
	}
	return out
}
func RepairCatalog(catalog Object, p Provider, cache string) error {
	resolved, e := p.Resolve(cache)
	if e != nil {
		return e
	}
	for _, v := range array(catalog["models"]) {
		m := object(v)
		id := text(m["slug"])
		if id == "" {
			continue
		}
		var md Object
		for _, r := range resolved {
			if r["id"] == id {
				md = object(r["metadata"])
				break
			}
		}
		if md == nil {
			q := Clone(p)
			q.Summary["model"] = id
			q.Options()["models"] = []any{}
			rows, e := q.Resolve(cache)
			if e != nil {
				return e
			}
			md = object(rows[0]["metadata"])
		}
		profile := object(md["profile"])
		inputs := profile["inputModalities"]
		if inputs == nil {
			inputs = []string{"text"}
		}
		m["input_modalities"] = inputs
		m["supports_parallel_tool_calls"] = object(md["capabilities"])["parallelToolCalls"] == true
		if n := num(profile["contextWindow"]); n > 0 {
			m["context_window"] = n
			mx := num(profile["maxContextWindow"])
			if mx == 0 {
				mx = n
			}
			m["max_context_window"] = mx
		}
		if n := num(md["publishedOutput"]); n > 0 {
			m["max_output_tokens"] = n
		}
	}
	return nil
}
func RepairReasoning(catalog Object) error {
	models := array(catalog["models"])
	if len(models) == 0 {
		return fail("invalid_catalog", "模型目录为空或格式无效，请重新同步模型并应用后重试")
	}
	names := []string{"无", "极低", "低", "中", "高", "更高", "最大", "最高"}
	for _, v := range models {
		m := object(v)
		if m == nil {
			return fail("invalid_catalog", "模型目录格式无效")
		}
		levels, ok := m["supported_reasoning_levels"].([]any)
		if m["supported_reasoning_levels"] != nil && !ok {
			return fail("invalid_catalog", "模型思考强度列表格式无效")
		}
		if levels == nil {
			levels = []any{}
		}
		for i, e := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"} {
			found := false
			for _, v := range levels {
				found = found || object(v)["effort"] == e
			}
			if !found {
				levels = append(levels, Object{"effort": e, "description": names[i]})
			}
		}
		m["supported_reasoning_levels"] = levels
	}
	return nil
}
func ModelCatalog(p Provider, cache string) (Object, error) {
	rows, e := p.Resolve(cache)
	if e != nil {
		return nil, e
	}
	models := []any{}
	for i, m := range rows {
		if m["enabled"] == false {
			continue
		}
		levels := []any{}
		for _, effort := range array(m["reasoningEfforts"]) {
			levels = append(levels, Object{"effort": effort, "description": effort})
		}
		var defaultLevel any
		if len(levels) > 0 {
			defaultLevel = object(levels[0])["effort"]
		}
		profile := object(object(m["metadata"])["profile"])
		context := num(profile["contextWindow"])
		if context == 0 {
			context = 256000
		}
		priority := i + 1
		if m["id"] == p.Model() {
			priority = 0
		}
		var patch any
		if p.Upstream() == "anthropic" {
			patch = "freeform"
		}
		v := Object{"slug": m["id"], "display_name": m["id"], "description": "供应商模型 · uni-switch", "visibility": "list", "supported_in_api": true, "priority": priority, "default_reasoning_level": defaultLevel, "supported_reasoning_levels": levels, "shell_type": "shell_command", "availability_nux": nil, "upgrade": nil, "base_instructions": baseInstructions, "model_messages": nil, "support_verbosity": false, "default_verbosity": nil, "apply_patch_tool_type": patch, "supports_reasoning_summary_parameter": false, "default_reasoning_summary": "none", "context_window": context, "max_context_window": context, "auto_compact_token_limit": context, "truncation_policy": Object{"mode": "tokens", "limit": 10000}, "experimental_supported_tools": []any{}, "input_modalities": []any{"text"}, "supports_parallel_tool_calls": false}
		speed(v, p.Options()["fastMode"] == true)
		models = append(models, v)
	}
	catalog := Object{"models": models}
	if len(models) > 0 {
		if e = RepairReasoning(catalog); e != nil {
			return nil, e
		}
	}
	if e = RepairCatalog(catalog, p, cache); e != nil {
		return nil, e
	}
	return catalog, nil
}
func speed(m Object, enabled bool) {
	m["additional_speed_tiers"] = []any{}
	m["service_tiers"] = []any{}
	if enabled {
		m["additional_speed_tiers"] = []any{"fast"}
		m["service_tiers"] = []any{Object{"id": "priority", "name": "Fast", "description": "供应商优先服务档位"}}
	}
}
func Projection(f ManagedFile, content *string) (any, error) {
	if f.Format == "catalog" && content == nil {
		return nil, nil
	}
	doc, e := Parse(f.Format, content)
	if e != nil {
		return nil, e
	}
	if f.Format == "catalog" {
		return doc, nil
	}
	if f.Format == "meta" {
		var entry any
		for _, v := range array(doc["entries"]) {
			if object(v)["id"] == ProfileID {
				entry = v
				break
			}
		}
		return Object{"appliedId": doc["appliedId"], "entry": entry}, nil
	}
	out := Object{}
	for _, k := range f.Keys {
		out[k] = Get(doc, k)
	}
	return out, nil
}
func equalJSON(a, b any) bool { return Canonical(a) == Canonical(b) }
func fillPresets(current, expected Object) {
	old := array(expected["models"])
	for i, v := range array(current["models"]) {
		if i >= len(old) {
			break
		}
		m := object(v)
		levels, ok := object(old[i])["supported_reasoning_levels"].([]any)
		if !ok {
			continue
		}
		present, ok := m["supported_reasoning_levels"].([]any)
		if !ok && m["supported_reasoning_levels"] != nil {
			continue
		}
		retained := []any{}
		for _, l := range levels {
			effort := text(object(l)["effort"])
			keep := !validEffort(effort)
			for _, a := range present {
				keep = keep || object(a)["effort"] == effort
			}
			if keep {
				retained = append(retained, l)
			}
		}
		if equalJSON(present, retained) || len(present) == 0 && len(retained) == 0 {
			m["supported_reasoning_levels"] = levels
		}
	}
}
func Changed(f ManagedFile, p *Provider) (bool, error) {
	current, e := edit.Read(f.Path)
	if e != nil {
		return false, e
	}
	a, e := Projection(f, current)
	if e != nil {
		return false, e
	}
	b, e := Projection(f, f.Expected)
	if e != nil {
		return false, e
	}
	if equalJSON(a, b) {
		return false, nil
	}
	if p == nil || p.Summary["family"] != "codex" {
		return true, nil
	}
	if f.Format == "catalog" {
		if current == nil || f.Expected == nil {
			return true, nil
		}
		c, e := jsonObject(current)
		if e != nil {
			return false, e
		}
		old, e := jsonObject(f.Expected)
		if e != nil {
			return false, e
		}
		fillPresets(c, old)
		return !equalJSON(c, old), nil
	}
	if f.Format != "toml" {
		return true, nil
	}
	doc, e := Parse("toml", current)
	if e != nil {
		return false, e
	}
	old, e := Parse("toml", f.Expected)
	if e != nil {
		return false, e
	}
	catalogPath := filepath.Join(filepath.Dir(f.Path), "uni-switch-models.json")
	included := false
	if text(old["model_catalog_json"]) == catalogPath {
		raw, e := edit.Read(catalogPath)
		if e != nil {
			return false, e
		}
		c, e := jsonObject(raw)
		if e != nil {
			return false, e
		}
		for _, v := range array(c["models"]) {
			included = included || object(v)["slug"] == doc["model"]
		}
	}
	if !included {
		return true, nil
	}
	if v := doc["model_reasoning_effort"]; v != nil && !validEffort(text(v)) {
		return true, nil
	}
	if v := doc["service_tier"]; v != nil {
		s := text(v)
		if s != "default" && s != "priority" && s != "fast" && s != "flex" {
			return true, nil
		}
	}
	f.Keys = without(f.Keys, "model", "model_reasoning_effort", "service_tier", DisplayKey)
	a, e = Projection(f, current)
	if e != nil {
		return false, e
	}
	b, e = Projection(f, f.Expected)
	return !equalJSON(a, b), e
}
func Restore(f ManagedFile) (edit.Change, error) {
	current, e := edit.Read(f.Path)
	if e != nil {
		return edit.Change{}, e
	}
	a, e := Projection(f, current)
	if e != nil {
		return edit.Change{}, e
	}
	b, e := Projection(f, f.Expected)
	if e != nil {
		return edit.Change{}, e
	}
	if !equalJSON(a, b) {
		return edit.Change{}, fail("external_change", "配置中的 API 字段已被其他工具修改，请先撤回修改后恢复")
	}
	after := f.Original
	if f.Format != "catalog" {
		doc, e := Parse(f.Format, current)
		if e != nil {
			return edit.Change{}, e
		}
		original, e := Parse(f.Format, f.Original)
		if e != nil {
			return edit.Change{}, e
		}
		if f.Format == "meta" {
			entries := []any{}
			for _, v := range array(doc["entries"]) {
				if object(v)["id"] != ProfileID {
					entries = append(entries, v)
				}
			}
			for _, v := range array(original["entries"]) {
				if object(v)["id"] == ProfileID {
					entries = append(entries, v)
				}
			}
			if original["entries"] == nil && len(entries) == 0 {
				delete(doc, "entries")
			} else {
				doc["entries"] = entries
			}
			_ = Set(doc, "appliedId", original["appliedId"], original["appliedId"] == nil)
			after = strptr(pretty(doc))
		} else {
			mutations := []Mutation{}
			for _, k := range f.Keys {
				v := Get(original, k)
				mutations = append(mutations, Mutation{strings.Split(k, "."), v, v == nil})
			}
			s, e := Mutate(f.Format, current, mutations)
			if e != nil {
				return edit.Change{}, e
			}
			after = &s
			doc, e = Parse(f.Format, after)
			if e != nil {
				return edit.Change{}, e
			}
			if f.Format != "toml" && original["env"] == nil && len(object(doc["env"])) == 0 {
				delete(doc, "env")
				after = strptr(pretty(doc))
			}
		}
		final, e := Parse(f.Format, after)
		if e != nil {
			return edit.Change{}, e
		}
		if f.Original == nil && len(final) == 0 {
			after = nil
		}
		if f.Format == "toml" && f.Original != nil && after != nil && strings.TrimRight(*after, "\r\n") == strings.TrimRight(*f.Original, "\r\n") {
			after = f.Original
		}
	}
	return edit.Change{Path: f.Path, Before: current, After: after}, nil
}
func Preserve(f ManagedFile, p Provider, keep bool) (*string, error) {
	before, e := Parse("toml", f.Original)
	if e != nil {
		return nil, e
	}
	ms := []Mutation{}
	for _, k := range []string{"model", "model_reasoning_effort", "service_tier"} {
		if k == "model" {
			valid := false
			for _, v := range array(p.Options()["models"]) {
				m := object(v)
				valid = valid || m["enabled"] != false && m["id"] == before[k]
			}
			if !keep || !valid {
				continue
			}
		}
		if v := before[k]; v != nil {
			ms = append(ms, Mutation{[]string{k}, v, false})
		}
	}
	s, e := Mutate("toml", f.Expected, ms)
	return &s, e
}
func RepairFiles(dir string, p Provider, cache string, baseline []ManagedFile, fast *bool) ([]ManagedFile, error) {
	catalogPath, configPath := filepath.Join(dir, "uni-switch-models.json"), filepath.Join(dir, "config.toml")
	before, e := edit.Read(catalogPath)
	if e != nil {
		return nil, e
	}
	var catalog Object
	if before == nil {
		catalog, e = ModelCatalog(p, cache)
	} else {
		catalog, e = jsonObject(before)
	}
	if e != nil {
		return nil, e
	}
	for _, f := range baseline {
		if f.Format == "catalog" && f.Expected != nil {
			old, e := jsonObject(f.Expected)
			if e != nil {
				return nil, e
			}
			fillPresets(catalog, old)
		}
	}
	if e = RepairCatalog(catalog, p, cache); e != nil {
		return nil, e
	}
	if e = RepairReasoning(catalog); e != nil {
		return nil, e
	}
	if fast != nil {
		for _, v := range array(catalog["models"]) {
			speed(object(v), *fast)
		}
	}
	config, e := edit.Read(configPath)
	if e != nil {
		return nil, e
	}
	doc, e := Parse("toml", config)
	if e != nil {
		return nil, e
	}
	display, e := displayMutation(doc)
	if e != nil {
		return nil, e
	}
	mutations := []Mutation{display, {Path: []string{"model_catalog_json"}, Value: catalogPath}}
	if fast != nil {
		tier := "default"
		if *fast {
			tier = "priority"
		}
		mutations = append(mutations, Mutation{Path: []string{"service_tier"}, Value: tier}, Mutation{Path: []string{"features", "fast_mode"}, Value: true})
	}
	after, e := Mutate("toml", config, mutations)
	if e != nil {
		return nil, e
	}
	files := []ManagedFile{{catalogPath, "catalog", []string{}, before, strptr(pretty(catalog))}}
	if fast != nil || !Same(config, &after) {
		keys := []string{"model_catalog_json", DisplayKey}
		for _, f := range baseline {
			if f.Format == "toml" {
				keys = append([]string{}, f.Keys...)
				break
			}
		}
		if fast != nil && !has(keys, DisplayKey) {
			keys = append(keys, DisplayKey)
		}
		files = append(files, ManagedFile{configPath, "toml", keys, config, &after})
	}
	return files, nil
}
func has(a []string, k string) bool {
	for _, v := range a {
		if v == k {
			return true
		}
	}
	return false
}
func ClaudeNeedsUpdate(files []ManagedFile, target string, p Provider, cache string) (bool, error) {
	models, e := p.Models(cache, false)
	if e != nil {
		return false, e
	}
	for _, f := range files {
		doc, e := jsonObject(f.Expected)
		if e != nil {
			return false, e
		}
		for _, k := range []string{"env.ANTHROPIC_BASE_URL", "inferenceGatewayBaseUrl"} {
			if base := text(Get(doc, k)); has(f.Keys, k) && strings.TrimSuffix(strings.TrimRight(base, "/"), "/v1") != base {
				return true, nil
			}
		}
		if target == "claude_desktop" && has(f.Keys, "inferenceModels") {
			expected, e := DesktopModels(p.IDs(), p.Converted(target), models)
			if e != nil {
				return false, e
			}
			if !equalJSON(doc["inferenceModels"], expected) {
				return true, nil
			}
		}
		if target == "claude_cli" && filepath.Base(f.Path) == "settings.json" {
			for _, role := range []string{"sonnet", "opus", "haiku", "fable"} {
				if Get(doc, "env.ANTHROPIC_DEFAULT_"+strings.ToUpper(role)+"_MODEL") != roleModel(p.IDs(), role, p.Model()) {
					return true, nil
				}
			}
		}
	}
	return false, nil
}
func Selection(dir string) (any, error) {
	raw, e := edit.Read(filepath.Join(dir, "config.toml"))
	if e != nil {
		return nil, e
	}
	doc, e := Parse("toml", raw)
	if e != nil {
		return nil, e
	}
	if doc["model_provider"] != "uni_switch" || text(doc["model"]) == "" {
		return nil, nil
	}
	var effort any
	if validEffort(text(doc["model_reasoning_effort"])) {
		effort = doc["model_reasoning_effort"]
	}
	return Object{"model": doc["model"], "reasoningEffort": effort}, nil
}
func Preferences(client, dir string) (Object, error) {
	specs, e := Specifications(client, dir, false, false)
	if e != nil {
		return nil, e
	}
	path := specs[0].Path
	if client == "claude_desktop" {
		for _, s := range specs {
			if s.Format == "meta" {
				raw, e := edit.Read(s.Path)
				if e != nil {
					return nil, e
				}
				doc, e := jsonObject(raw)
				if e != nil {
					return nil, e
				}
				id := text(doc["appliedId"])
				if !validUUID(id) {
					return nil, fail("invalid_profile", "Claude profile ID 必须是 UUID")
				}
				path = filepath.Join(filepath.Dir(s.Path), id+".json")
			}
		}
	}
	raw, e := edit.Read(path)
	if e != nil {
		return nil, e
	}
	if raw == nil {
		return nil, fail("configuration_missing", "目标配置文件不存在")
	}
	format := "json"
	if client == "codex" {
		format = "toml"
	}
	doc, e := Parse(format, raw)
	if e != nil {
		return nil, e
	}
	options := Object{}
	var model, effort any = "", nil
	switch client {
	case "codex":
		model = doc["model"]
		effort = doc["model_reasoning_effort"]
		if tier, ok := doc["service_tier"].(string); ok {
			options["fastMode"] = tier == "priority" || tier == "fast"
		}
		options["contextWindow"] = doc["model_context_window"]
		options["autoCompactTokenLimit"] = doc["model_auto_compact_token_limit"]
		catalogPath := filepath.Join(dir, "uni-switch-models.json")
		if doc["model_catalog_json"] == catalogPath {
			text, e := edit.Read(catalogPath)
			if e != nil {
				return nil, e
			}
			catalog, e := jsonObject(text)
			if e != nil {
				return nil, e
			}
			models := []any{}
			for _, v := range array(catalog["models"]) {
				m := object(v)
				id := m["slug"]
				if id == nil {
					id = m["id"]
				}
				levels := []any{}
				for _, l := range array(m["supported_reasoning_levels"]) {
					levels = append(levels, object(l)["effort"])
				}
				models = append(models, Object{"id": id, "enabled": true, "contextWindow": m["context_window"], "reasoningEfforts": levels})
			}
			options["models"] = models
		}
	case "claude_cli":
		model = Get(doc, "env.ANTHROPIC_MODEL")
		if model == nil {
			model = doc["model"]
		}
	case "claude_desktop":
		models := []any{}
		for _, v := range array(doc["inferenceModels"]) {
			m := object(v)
			id := text(v)
			if id == "" {
				id = text(m["name"])
			}
			if label := text(m["labelOverride"]); label != "" {
				route := gateway.Model{ID: label, Enabled: true}
				cat, _ := gateway.Catalog(gateway.CatalogInput{Models: []gateway.Model{route}})
				for _, r := range cat["routes"].([]gateway.ModelRoute) {
					if id == r.DesktopID || id == r.LegacyID || id == r.AlternateDesktopID {
						id = label
					}
				}
			}
			_, large := splitContext(id)
			if m["supports1m"] == true && !large {
				id += "[1m]"
			}
			models = append(models, Object{"id": id, "enabled": true})
		}
		options["models"] = models
		if len(models) > 0 {
			model = object(models[0])["id"]
		}
	}
	if model == nil {
		model = ""
	}
	return Object{"model": model, "reasoningEffort": effort, "codexOptions": options}, nil
}

// Kept here to allow the native DTO boundary to remain data-only.
