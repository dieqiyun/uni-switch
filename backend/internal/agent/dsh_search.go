package agent

// dsh's web search is a plugin of its own, web-search-deepseek: each search
// is one Anthropic Messages request with the web_search_20250305 server
// tool to <baseURL>/messages, DeepSeek's by default, signed with the key
// its apiKeyEnv names (DEEPSEEK_API_KEY). A dsh that reaches its models
// through uni-switch alone has no such key, and every search failed asking for
// one (Packing1 on Discord). While dsh starts on a model of uni-switch's, uni-switch
// points the row at the gateway, with its own key and that model, or the
// searcher Settings › Web search names when the user named one
// (dshSearchModel): the gateway answers the server tool as Anthropic does,
// by the model's own search or uni-switch's (web_search_tool_result blocks
// either way). A row of
// the user's, or a DeepSeek key dsh has of its own, is left to search as it
// does.

import (
	"path/filepath"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// dshSearchRow is dsh's DeepSeek web search row, and its plugin.
const (
	dshSearchRow    = "web-search-deepseek"
	dshSearchPlugin = "@deepseek-ai/dsh-web-search-deepseek"
	dshSearchKeyRef = "DEEPSEEK_API_KEY"
)

// dshSearchModel is the model uni-switch's search row asks, for a dsh that
// starts on start (a model of uni-switch's, without its prefix): the searcher
// Settings › Web search names, when the user named one, else start. Asking
// start sent every search to the model dsh works on, which, unable to
// search by itself, asked the named searcher in turn: two requests a
// search, and a model that takes one call at a time timed out (我只会玩单机
// on Discord).
func dshSearchModel(start string) string {
	if m := gateway.NamedSearchModel(); m != "" {
		return m
	}
	return start
}

// dshSearchLines is uni-switch's web-search-deepseek entry: the gateway's
// Messages API (the plugin adds /messages to the base) on its key, asking
// model.
func dshSearchLines(model, gw string) []string {
	return []string{
		"- id: " + dshSearchRow + " " + dshMark,
		"  name: " + yamlQuote(dshSearchPlugin),
		"  config:",
		"    apiKeyEnv: " + dshKeyRef,
		"    baseURL: " + yamlQuote(gw+"/v1"),
		"    model: " + yamlQuote(model),
	}
}

// dshOwnSearchKey reports whether dsh has a DeepSeek key of its own to
// search with: in its .env, or in its own key store (dsh's Models page).
func dshOwnSearchKey(dir string) bool {
	if v, ok := edit.GetEnvFile(filepath.Join(dir, ".env"), dshSearchKeyRef); ok && strings.TrimSpace(v) != "" {
		return true
	}
	creds := filepath.Join(dir, ".credentials.yaml")
	if v, ok := edit.GetYAML(creds, "refs."+dshSearchKeyRef); ok && v != "" {
		return true
	}
	return edit.GetYAMLMap(creds, "records."+dshSearchKeyRef) != nil
}

// dshProfileHome is the dsh home a profile's patch list is in:
// <home>/profiles/<name>/cordis.patch.yml.
func dshProfileHome(path string) string {
	return filepath.Dir(filepath.Dir(filepath.Dir(path)))
}

// dshPutSearch points dsh's web search at the gateway, asking model, or with
// model "" takes uni-switch's entry out. An entry of the user's stays as it is,
// and so does dsh's own search when it has a DeepSeek key to search with.
func dshPutSearch(items []dshItem, dir, model, gw string) []dshItem {
	i := dshFindLast(items, dshSearchRow)
	if i >= 0 && !items[i].uniSwitch {
		return items
	}
	if model == "" || dshOwnSearchKey(dir) {
		if i >= 0 {
			items = append(items[:i], items[i+1:]...)
		}
		return items
	}
	it := dshItem{id: dshSearchRow, uniSwitch: true, lines: dshSearchLines(model, gw)}
	if i >= 0 {
		items[i] = it
	} else {
		items = append(items, it)
	}
	return items
}

// dshSearchAgain writes uni-switch's search entry again for the gateway at gw:
// the gateway's address may have moved since, and the searcher Settings
// names may have been picked or cleared. With none named, the entry asks
// the uni-switch model dsh starts on, else keeps the model it asks.
func dshSearchAgain(items []dshItem, dir, gw string) []dshItem {
	i := dshFindLast(items, dshSearchRow)
	if i < 0 || !items[i].uniSwitch {
		return items
	}
	model := dshConfig(items[i])["model"]
	if start, ok := strings.CutPrefix(dshStart(dir, items), uniSwitchID+"/"); ok {
		model = start
	}
	if model == "" {
		return items
	}
	return dshPutSearch(items, dir, dshSearchModel(model), gw)
}

// dshSearchesHere reports whether dsh's web search goes through uni-switch: its
// first profile has uni-switch's entry.
func dshSearchesHere(dir string) bool {
	files := dshProfiles(dir)
	if len(files) == 0 {
		return false
	}
	_, items, err := dshRead(files[0])
	if err != nil {
		return false
	}
	i := dshFindLast(items, dshSearchRow)
	return i >= 0 && items[i].uniSwitch
}
