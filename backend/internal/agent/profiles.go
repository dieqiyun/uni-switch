package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type Location struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Directory string `json:"directory"`
}

func Profiles(client, dir string) []Location {
	out := []Location{}
	add := func(id, name, path string) {
		if !filepath.IsAbs(path) {
			return
		}
		for _, v := range out {
			if v.Directory == path {
				return
			}
		}
		out = append(out, Location{id, name, path})
	}
	switch client {
	case "qwenpaw":
		def := filepath.Join(dir, "workspaces", "default")
		var cfg Object
		b, _ := os.ReadFile(filepath.Join(dir, "config.json"))
		_ = json.Unmarshal(b, &cfg)
		profiles := object(Get(cfg, "agents.profiles"))
		keys := map[string]bool{}
		for k := range profiles {
			keys[k] = true
		}
		for _, id := range append([]string{"default"}, sortedKeys(keys)...) {
			p := object(profiles[id])
			if p["enabled"] == false && id != "default" {
				continue
			}
			path := text(p["workspace_dir"])
			if path == "" {
				path = filepath.Join(dir, "workspaces", id)
			}
			if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
				home, _ := os.UserHomeDir()
				path = filepath.Join(home, path[1:])
			}
			if id == "default" && path == "" {
				path = def
			}
			if id == "" || strings.ContainsAny(id, `/\`) {
				continue
			}
			add(id, id, path)
		}
	case "omp":
		add("current", "当前配置", dir)
		root := filepath.Dir(dir)
		if filepath.Base(filepath.Dir(filepath.Dir(dir))) == "profiles" {
			root = filepath.Dir(filepath.Dir(filepath.Dir(dir)))
		}
		if p := filepath.Join(root, "agent"); isDirectory(p) {
			add("default", "default", p)
		}
		entries, _ := os.ReadDir(filepath.Join(root, "profiles"))
		for _, e := range entries {
			p := filepath.Join(root, "profiles", e.Name(), "agent")
			if e.IsDir() && isDirectory(p) {
				add("profile:"+e.Name(), e.Name(), p)
			}
		}
	case "hermes":
		add("current", "当前配置", dir)
		root := dir
		if filepath.Base(filepath.Dir(dir)) == "profiles" {
			root = filepath.Dir(filepath.Dir(dir))
		}
		if isFile(filepath.Join(root, "config.yaml")) {
			add("default", "default", root)
		}
		entries, _ := os.ReadDir(filepath.Join(root, "profiles"))
		for _, e := range entries {
			p := filepath.Join(root, "profiles", e.Name())
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && isFile(filepath.Join(p, "config.yaml")) && !isDirectory(filepath.Join(root, "profiles", ".deleted", e.Name())) {
				add("profile:"+e.Name(), e.Name(), p)
			}
		}
	case "aside":
		add("current", "当前账户", dir)
		if filepath.Base(filepath.Dir(dir)) == "u" {
			entries, _ := os.ReadDir(filepath.Dir(dir))
			for _, e := range entries {
				if e.IsDir() && strings.IndexFunc(e.Name(), func(r rune) bool { return r < '0' || r > '9' }) < 0 {
					add("account:"+e.Name(), "账户 "+e.Name(), filepath.Join(filepath.Dir(dir), e.Name()))
				}
			}
		}
	}
	return out
}

func ProfileSpecifications(client, dir, profile string, binding bool) ([]Spec, error) {
	specs, e := Specifications(client, dir, binding, true)
	if e != nil || profile == "" {
		return specs, e
	}
	for _, p := range Profiles(client, dir) {
		if p.ID == profile {
			if client == "qwenpaw" {
				specs[0].Path = filepath.Join(p.Directory, "agent.json")
				return specs, nil
			}
			return Specifications(client, p.Directory, false, true)
		}
	}
	return nil, fail("profile_missing", "所选配置档案已移除，请先重新选择")
}

func vscodeProfileSpecs(dir string) []Spec {
	var data Object
	b, _ := os.ReadFile(filepath.Join(dir, "globalStorage", "storage.json"))
	_ = json.Unmarshal(b, &data)
	out := []Spec{}
	for _, v := range array(data["userDataProfiles"]) {
		p := object(v)
		path := text(p["location"])
		if o := object(p["location"]); o != nil {
			path = text(o["fsPath"])
			if path == "" {
				path = text(o["path"])
				if len(path) > 2 && path[0] == '/' && path[2] == ':' {
					path = path[1:]
				}
				path = filepath.FromSlash(path)
			}
		} else if path != "" {
			path = filepath.Join(dir, "profiles", path)
		}
		if !filepath.IsAbs(path) || !isDirectory(path) {
			continue
		}
		if Get(p, "useDefaultFlags.settings") != true {
			out = append(out, Spec{Path: filepath.Join(path, "settings.json"), Format: "jsonc"})
		}
		if Get(p, "useDefaultFlags.languageModels") != true {
			out = append(out, Spec{Path: filepath.Join(path, "chatLanguageModels.json"), Format: "jsonc_array"})
		}
	}
	return out
}

func vscodeVendor(client, dir string) (string, error) {
	if client != "vscodium" {
		return "customendpoint", nil
	}
	home, _ := os.UserHomeDir()
	ext := filepath.Join(home, ".vscode-oss", "extensions")
	if parent := filepath.Dir(filepath.Dir(filepath.Dir(dir))); isDirectory(filepath.Join(parent, "extensions")) {
		ext = filepath.Join(parent, "extensions")
	}
	paths, _ := filepath.Glob(filepath.Join(ext, "github.copilot-chat-*", "package.json"))
	best := ""
	var latest int64
	for _, p := range paths {
		if s, e := os.Stat(p); e == nil && (best == "" || s.ModTime().UnixNano() > latest) {
			best = p
			latest = s.ModTime().UnixNano()
		}
	}
	if best == "" {
		return "customendpoint", nil
	}
	b, e := os.ReadFile(best)
	if e != nil {
		return "customendpoint", nil
	}
	var pkg Object
	if json.Unmarshal(b, &pkg) != nil {
		return "customendpoint", nil
	}
	for _, want := range []string{"customendpoint", "customoai"} {
		for _, v := range array(Get(pkg, "contributes.languageModelChatProviders")) {
			if object(v)["vendor"] == want {
				return want, nil
			}
		}
	}
	return "", fail("unsupported_client_version", "已安装的 Copilot Chat 未声明自定义模型接口，请更新扩展")
}
