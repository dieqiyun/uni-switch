package configuration

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

func hanakoRoot(path, format string) string {
	if format == "hanako_chat" {
		return filepath.Dir(filepath.Dir(filepath.Dir(path)))
	}
	return filepath.Dir(path)
}
func hanakoServer(root string) (string, string, bool) {
	raw, e := edit.Read(filepath.Join(root, "server-info.json"))
	if e != nil || raw == nil {
		return "", "", false
	}
	var s Object
	if parse(*raw, &s) != nil {
		return "", "", false
	}
	port := int64of(s["port"])
	token := str(s["token"])
	if port < 1 || port > 65535 || token == "" {
		return "", "", false
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	var identity Object
	if resourceHTTP("GET", base+"/api/server/identity", token, nil, &identity) != nil || str(identity["serverId"]) == "" {
		return "", "", false
	}
	return base, token, true
}
func readHanako(path, format string) (*string, error) {
	if err := agent.CheckPath(path); err != nil {
		return nil, err
	}
	raw, err := edit.Read(path)
	if err != nil {
		return nil, err
	}
	if format == "hanako_chat" {
		if raw == nil {
			return nil, fail("client_unavailable", "OpenHanako 尚无 agent，请先打开客户端创建")
		}
		doc, err := agent.Parse("yaml", raw)
		if err != nil {
			return nil, err
		}
		return ptr(encode(agent.Get(doc, "models.chat"))), nil
	}
	sealed := raw != nil && strings.HasPrefix(strings.TrimSpace(*raw), "HANA-SECRET-")
	provider := Object{}
	pluginPath := filepath.Join(filepath.Dir(path), "provider-plugins", "uni-switch", "providers", "uni-switch.json")
	if err := agent.CheckPath(pluginPath); err != nil {
		return nil, err
	}
	pluginRaw, err := edit.Read(pluginPath)
	if err != nil {
		return nil, err
	}
	if pluginRaw != nil {
		var p Object
		if parse(*pluginRaw, &p) != nil {
			return nil, fail("invalid_config", "OpenHanako 供应商插件不是有效 JSON")
		}
		for a, b := range map[string]string{"display_name": "displayName", "base_url": "defaultBaseUrl", "api": "defaultApi", "models": "models"} {
			if v, ok := p[b]; ok {
				provider[a] = v
			}
		}
	}
	deleted := false
	if raw != nil && !sealed {
		var doc Object
		if parse(*raw, &doc) != nil {
			return nil, fail("invalid_config", "OpenHanako catalog 格式无效")
		}
		if int64of(doc["catalogVersion"]) != 2 {
			return nil, fail("unsupported_schema", "OpenHanako 需要 catalogVersion 2")
		}
		for k, v := range obj(agent.Get(doc, "providers.uni-switch")) {
			provider[k] = v
		}
		for _, v := range arr(agent.Get(doc, "meta.deletedProviders")) {
			deleted = deleted || v == "uni-switch"
		}
	}
	var p any
	if len(provider) > 0 {
		p = provider
	}
	return ptr(encode(Object{"provider": p, "sealed": sealed, "deleted": deleted, "plugin": pluginRaw})), nil
}
func writeHanako(path, format string, after *string) error {
	root := hanakoRoot(path, format)
	base, token, live := hanakoServer(root)
	if format == "hanako_chat" {
		var chat any
		if after != nil {
			if parse(*after, &chat) != nil {
				return fail("invalid_config", "OpenHanako 模型快照无效")
			}
		}
		if live {
			return resourceHTTP("PUT", base+"/api/agents/"+filepath.Base(filepath.Dir(path))+"/config", token, Object{"models": Object{"chat": chat}}, nil)
		}
		raw, err := edit.Read(path)
		if err != nil {
			return err
		}
		out, err := agent.Mutate("yaml", raw, []agent.Mutation{{Path: []string{"models", "chat"}, Value: chat, Remove: chat == nil}})
		if err != nil {
			return err
		}
		return edit.WriteAtomic(path, &out)
	}
	var state Object
	if after == nil || parse(*after, &state) != nil {
		return fail("invalid_config", "OpenHanako 供应商快照无效")
	}
	p := obj(state["provider"])
	if live {
		return resourceHTTP("PUT", base+"/api/config", token, Object{"providers": Object{"uni-switch": p}}, nil)
	}
	if state["sealed"] == true {
		return fail("client_unavailable", "OpenHanako 已加密供应商资料，请启动客户端后重试；密文不会被覆盖")
	}
	raw, err := edit.Read(path)
	if err != nil {
		return err
	}
	original := raw
	if raw != nil && strings.HasPrefix(strings.TrimSpace(*raw), "HANA-SECRET-") {
		return fail("encrypted_config", "OpenHanako 已加密配置，请启动客户端后重试")
	}
	if raw == nil {
		raw = ptr(`{"catalogVersion":2,"providers":{}}`)
	}
	doc, err := agent.Parse("json", raw)
	if err != nil {
		return err
	}
	ids := []any{}
	for _, id := range arr(agent.Get(doc, "meta.deletedProviders")) {
		if id != "uni-switch" {
			ids = append(ids, id)
		}
	}
	if state["deleted"] == true || p == nil {
		ids = append(ids, "uni-switch")
	}
	ms := []agent.Mutation{{Path: []string{"providers", "uni-switch"}, Value: p, Remove: p == nil}}
	if agent.Get(doc, "meta.deletedProviders") != nil || len(ids) > 0 {
		ms = append(ms, agent.Mutation{Path: []string{"meta", "deletedProviders"}, Value: ids})
	}
	out, err := agent.Mutate("json", raw, ms)
	if err != nil {
		return err
	}
	pluginPath := filepath.Join(root, "provider-plugins", "uni-switch", "providers", "uni-switch.json")
	if err := agent.CheckPath(pluginPath); err != nil {
		return err
	}
	pluginBefore, err := edit.Read(pluginPath)
	if err != nil {
		return err
	}
	var pluginAfter *string
	if v, ok := state["plugin"].(string); ok {
		pluginAfter = &v
	}
	return edit.Apply([]edit.Change{{Path: path, Before: original, After: &out}, {Path: pluginPath, Before: pluginBefore, After: pluginAfter}})
}

func hanakoRestoreChat(path string, original *string) (*string, error) {
	base, token, live := hanakoServer(hanakoRoot(path, "hanako_chat"))
	if !live {
		return original, nil
	}
	var saved any
	if original != nil && parse(*original, &saved) != nil {
		return nil, fail("invalid_state", "OpenHanako 原模型快照无效")
	}
	wantID, wantProvider := str(saved), ""
	if m := obj(saved); m != nil {
		wantID, wantProvider = str(m["id"]), str(m["provider"])
	}
	var available struct {
		Models []Object `json:"models"`
	}
	if err := resourceHTTP("GET", base+"/api/models", token, nil, &available); err != nil {
		return nil, err
	}
	var fallback Object
	for _, m := range available.Models {
		id, provider := str(m["id"]), str(m["provider"])
		if id == "" || provider == "" || provider == "uni-switch" {
			continue
		}
		model := Object{"provider": provider, "id": id}
		if fallback == nil {
			fallback = model
		}
		if id == wantID && (wantProvider == "" || wantProvider == provider) {
			return ptr(encode(model)), nil
		}
	}
	if fallback == nil {
		return nil, fail("client_unavailable", "OpenHanako 没有可恢复的原生模型，请先在客户端添加供应商")
	}
	return ptr(encode(fallback)), nil
}
