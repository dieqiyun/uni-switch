package agent

import (
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func DiscoveryDefault(client, home, local string) string {
	switch client {
	case "codex":
		return filepath.Join(home, ".codex")
	case "claude_cli":
		return filepath.Join(home, ".claude")
	default:
		if local != "" {
			return local
		}
		if runtime.GOOS == "windows" {
			return filepath.Join(home, "AppData", "Local")
		}
		if runtime.GOOS == "darwin" {
			return filepath.Join(home, "Library", "Application Support")
		}
		return filepath.Join(home, ".config")
	}
}
func marker(dir string) bool {
	return isFile(filepath.Join(dir, "claude_desktop_config.json")) || isFile(filepath.Join(dir, "configLibrary", "_meta.json"))
}

// Read-only format inspection returns structural diagnostics, never file data.
func Inspect(client, dir string, explicit bool) (Object, error) {
	files, issues := []string{}, []string{}
	var modified any
	var stamp int64
	if client == "claude_desktop" && !explicit && !marker(dir) && !marker(filepath.Join(dir, "Claude")) && !marker(filepath.Join(dir, "Claude-3p")) {
		return Object{"files": files, "issues": issues, "modified": nil}, nil
	}
	specs, e := Specifications(client, dir, false, false)
	if e != nil {
		return nil, e
	}
	read := func(path, format string) (*string, error) {
		fi, e := os.Stat(path)
		if e != nil {
			return nil, fail("configuration_missing", "文件无法读取或没有访问权限")
		}
		if fi.Size() > 2<<20 {
			return nil, fail("config_too_large", "配置文件超过 2 MB，未解析")
		}
		if e = CheckPath(path); e != nil {
			return nil, e
		}
		raw, e := edit.Read(path)
		if e != nil {
			return nil, e
		}
		if raw == nil {
			return nil, fail("configuration_missing", "配置文件缺失")
		}
		if e = Validate(format, *raw); e != nil {
			return nil, e
		}
		return raw, nil
	}
	for _, s := range specs {
		if s.Format == "catalog" || client == "claude_desktop" && filepath.Base(s.Path) == ProfileID+".json" {
			continue
		}
		fi, e := os.Stat(s.Path)
		if e != nil || !fi.Mode().IsRegular() {
			continue
		}
		files = append(files, s.Path)
		if n := fi.ModTime().Unix(); n > stamp {
			stamp = n
			modified = n
		}
		format := "json"
		if client == "codex" {
			format = "toml"
		}
		raw, e := read(s.Path, format)
		if e != nil {
			issues = append(issues, e.Error())
			continue
		}
		if client == "claude_desktop" && filepath.Base(s.Path) == "_meta.json" {
			doc, e := jsonObject(raw)
			if e != nil {
				continue
			}
			id := text(doc["appliedId"])
			if id == "" {
				continue
			}
			if strings.ContainsAny(id, `/\:`) || id == "." || id == ".." || len(id) > 100 {
				issues = append(issues, "配置库当前选中的 profile ID 无效")
				continue
			}
			profile := filepath.Join(filepath.Dir(s.Path), id+".json")
			if isFile(profile) {
				files = append(files, profile)
				if _, e = read(profile, "json"); e != nil {
					issues = append(issues, "配置库当前选中的 profile 无法读取或格式无效")
				}
			} else {
				issues = append(issues, "配置库当前选中的 profile 文件缺失")
			}
		}
	}
	return Object{"files": files, "issues": issues, "modified": modified}, nil
}
