package agent

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/jsonc"
	"github.com/tidwall/sjson"
	"gopkg.in/yaml.v3"
)

func parseConfig(f File) (Object, error) {
	if f.Content == nil {
		return Object{}, nil
	}
	s := strings.TrimPrefix(*f.Content, "\ufeff")
	if len(s) > 4<<20 {
		return nil, fail("config_too_large", "配置超过 4 MiB")
	}
	switch f.Format {
	case "toml":
		return Parse("toml", &s)
	case "yaml":
		var v Object
		if yaml.Unmarshal([]byte(s), &v) != nil {
			return nil, fail("invalid_yaml", "YAML 顶层必须是对象")
		}
		if v == nil {
			v = Object{}
		}
		return v, nil
	case "jsonc_array":
		var v []any
		if json.Unmarshal(jsonc.ToJSON([]byte(s)), &v) != nil || v == nil {
			return nil, fail("invalid_json", "模型配置顶层必须是数组")
		}
		return Object{"items": v}, nil
	case "jsonc":
		s = string(jsonc.ToJSON([]byte(s)))
	}
	return jsonObject(&s)
}

// JSONC and TOML changes are made at field spans so unrelated comments survive.
func renderConfig(f File, doc Object) (string, error) {
	if f.Format == "jsonc_array" {
		if f.Content == nil {
			return pretty(doc["items"]), nil
		}
		raw := *f.Content
		items := array(doc["items"])
		clean := string(jsonc.ToJSON([]byte(raw)))
		old := gjson.Parse(clean).Array()
		for i, v := range items {
			var existing any
			if i < len(old) {
				_ = json.Unmarshal([]byte(old[i].Raw), &existing)
			}
			if equalJSON(existing, v) {
				continue
			}
			clean = string(jsonc.ToJSON([]byte(raw)))
			changed, e := sjson.Set(clean, strconv.Itoa(i), v)
			if e != nil {
				return "", e
			}
			raw = spliceJSONC(raw, clean, changed)
		}
		for i := len(old) - 1; i >= len(items); i-- {
			clean = string(jsonc.ToJSON([]byte(raw)))
			changed, e := sjson.Delete(clean, strconv.Itoa(i))
			if e != nil {
				return "", e
			}
			raw = spliceJSONC(raw, clean, changed)
		}
		var verify []any
		if json.Unmarshal(jsonc.ToJSON([]byte(raw)), &verify) != nil || !equalJSON(verify, doc["items"]) {
			return "", fail("invalid_config", "模型列表更新校验失败")
		}
		return raw, nil
	}
	before, e := parseConfig(f)
	if e != nil {
		return "", e
	}
	if equalJSON(before, doc) && f.Content != nil {
		return *f.Content, nil
	}
	if f.Format == "toml" {
		var ms []Mutation
		var walk func(Object, Object, []string)
		walk = func(a, b Object, path []string) {
			keys := map[string]bool{}
			for k := range a {
				keys[k] = true
			}
			for k := range b {
				keys[k] = true
			}
			for _, k := range sortedKeys(keys) {
				av, bv := a[k], b[k]
				p := append(append([]string{}, path...), k)
				if equalJSON(av, bv) {
					continue
				}
				if object(av) != nil && object(bv) != nil {
					walk(object(av), object(bv), p)
				} else if object(bv) != nil {
					walk(Object{}, object(bv), p)
				} else {
					ms = append(ms, Mutation{Path: p, Value: bv, Remove: bv == nil})
				}
			}
		}
		walk(before, doc, nil)
		return Mutate("toml", f.Content, ms)
	}
	if f.Format == "yaml" {
		var root yaml.Node
		if f.Content != nil {
			if yaml.Unmarshal([]byte(*f.Content), &root) != nil {
				return "", fail("invalid_yaml", "YAML 格式无效")
			}
		}
		if len(root.Content) == 0 {
			root = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
		}
		mergeYAML(root.Content[0], before, doc)
		b, e := yaml.Marshal(&root)
		return string(b), e
	}
	if f.Content == nil {
		return pretty(doc), nil
	}
	raw := strings.TrimPrefix(*f.Content, "\ufeff")
	var walk func(Object, Object, string) error
	walk = func(a, b Object, path string) error {
		keys := map[string]bool{}
		for k := range a {
			keys[k] = true
		}
		for k := range b {
			keys[k] = true
		}
		for _, k := range sortedKeys(keys) {
			av, bv := a[k], b[k]
			if equalJSON(av, bv) {
				continue
			}
			p := escapeJSONKey(k)
			if path != "" {
				p = path + "." + p
			}
			if object(av) != nil && object(bv) != nil {
				if e := walk(object(av), object(bv), p); e != nil {
					return e
				}
				continue
			}
			clean := string(jsonc.ToJSON([]byte(raw)))
			var changed string
			var e error
			if _, exists := b[k]; !exists {
				changed, e = sjson.Delete(clean, p)
			} else {
				changed, e = sjson.Set(clean, p, bv)
			}
			if e != nil {
				return fail("invalid_config", "无法更新 JSON 字段")
			}
			l, r := 0, 0
			for l < len(clean) && l < len(changed) && clean[l] == changed[l] {
				l++
			}
			for r < len(clean)-l && r < len(changed)-l && clean[len(clean)-1-r] == changed[len(changed)-1-r] {
				r++
			}
			raw = raw[:l] + changed[l:len(changed)-r] + raw[len(raw)-r:]
		}
		return nil
	}
	if e = walk(before, doc, ""); e != nil {
		return "", e
	}
	var verify Object
	if json.Unmarshal(jsonc.ToJSON([]byte(raw)), &verify) != nil || !equalJSON(verify, doc) {
		return "", fail("invalid_config", "JSON 更新校验失败")
	}
	return raw, nil
}

func spliceJSONC(raw, clean, changed string) string {
	l, r := 0, 0
	for l < len(clean) && l < len(changed) && clean[l] == changed[l] {
		l++
	}
	for r < len(clean)-l && r < len(changed)-l && clean[len(clean)-1-r] == changed[len(changed)-1-r] {
		r++
	}
	return raw[:l] + changed[l:len(changed)-r] + raw[len(raw)-r:]
}
func sortedKeys(m map[string]bool) []string {
	k := make([]string, 0, len(m))
	for s := range m {
		k = append(k, s)
	}
	sort.Strings(k)
	return k
}
func escapeJSONKey(s string) string {
	r := strings.NewReplacer("\\", "\\\\", ".", "\\.", ":", "\\:", "*", "\\*", "?", "\\?")
	s = r.Replace(s)
	if _, e := strconv.Atoi(s); e == nil {
		return ":" + s
	}
	return s
}
func mergeYAML(node *yaml.Node, before, after Object) {
	for i := len(node.Content) - 2; i >= 0; i -= 2 {
		k := node.Content[i].Value
		if _, ok := after[k]; !ok {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
		}
	}
	keys := map[string]bool{}
	for k := range after {
		keys[k] = true
	}
	for _, k := range sortedKeys(keys) {
		if equalJSON(before[k], after[k]) {
			continue
		}
		old := nodeGet(node, k)
		if old != nil && old.Kind == yaml.MappingNode && object(after[k]) != nil {
			mergeYAML(old, object(before[k]), object(after[k]))
			continue
		}
		n := &yaml.Node{}
		_ = n.Encode(after[k])
		if old != nil {
			n.HeadComment = old.HeadComment
			n.LineComment = old.LineComment
			n.FootComment = old.FootComment
		}
		nodeSet(node, k, n)
	}
}

// dotenv values are JSON quoted; clients using dotenv never execute shell text.
func setEnv(raw *string, key, value string) (string, error) {
	s := ""
	if raw != nil {
		s = *raw
	}
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := []string{}
	found := false
	for _, l := range lines {
		t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "export "))
		if strings.HasPrefix(t, key+"=") || strings.HasPrefix(t, key+" =") {
			if !found {
				out = append(out, key+"="+strconv.Quote(value))
				found = true
			}
			continue
		}
		out = append(out, l)
	}
	if !found {
		if len(out) > 0 && out[len(out)-1] == "" {
			out = out[:len(out)-1]
		}
		out = append(out, key+"="+strconv.Quote(value), "")
	}
	return strings.Join(out, "\n"), nil
}
