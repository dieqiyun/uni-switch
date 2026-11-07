package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/google/uuid"
	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

const ProfileID = "e82de475-47fa-4c54-9000-13571c000001"
const DisplayKey = "desktop.enabled-reasoning-efforts"

var CodexKeys = []string{"model", "model_provider", "model_reasoning_effort", "service_tier", "features.fast_mode", "model_context_window", "model_auto_compact_token_limit", "model_catalog_json", "model_providers.uni_switch", "web_search", DisplayKey}

type Spec struct {
	Path   string   `json:"path"`
	Format string   `json:"format"`
	Keys   []string `json:"keys"`
}
type ManagedFile struct {
	Path     string   `json:"path"`
	Format   string   `json:"format"`
	Keys     []string `json:"keys"`
	Original *string  `json:"original"`
	Expected *string  `json:"expected"`
}

func DefaultDirectory(client string) (string, error) {
	home, e := os.UserHomeDir()
	if e != nil {
		return "", fail("missing_home", "无法确定用户目录")
	}
	env := func(k, f string) string {
		v := os.Getenv(k)
		if filepath.IsAbs(v) {
			return v
		}
		return f
	}
	switch client {
	case "codex":
		return env("CODEX_HOME", filepath.Join(home, ".codex")), nil
	case "claude_cli":
		return env("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude")), nil
	case "claude_desktop":
		if runtime.GOOS == "windows" {
			return env("LOCALAPPDATA", filepath.Join(home, "AppData", "Local")), nil
		}
		if runtime.GOOS == "darwin" {
			return filepath.Join(home, "Library", "Application Support"), nil
		}
		return env("XDG_CONFIG_HOME", filepath.Join(home, ".config")), nil
	case "zcode":
		if f := os.Getenv("ZCODE_PERSONAL_PROVIDER_CONFIG_FILE"); filepath.IsAbs(f) {
			return filepath.Dir(f), nil
		}
		return filepath.Join(env("ZCODE_DATA_BASE_DIR", home), ".zcode", "v2"), nil
	case "dsh":
		return env("DSH_HOME", filepath.Join(home, ".dsh")), nil
	case "workbuddy":
		return env("WORKBUDDY_CONFIG_DIR", filepath.Join(home, ".workbuddy")), nil
	}
	return extraDirectory(client, home)
}
func isFile(path string) bool { s, e := os.Stat(path); return e == nil && s.Mode().IsRegular() }
func desktopDir(root string, third bool) string {
	name := filepath.Base(root)
	meta := isFile(filepath.Join(root, "configLibrary", "_meta.json"))
	if isFile(filepath.Join(root, "claude_desktop_config.json")) || meta {
		isThird := strings.Contains(name, "-3p") || meta
		if isThird && !strings.Contains(name, "-3p") || isThird == third {
			return root
		}
		sibling := strings.ReplaceAll(name, "-3p", "")
		if third {
			sibling = name + "-3p"
		}
		if sibling != "" {
			return filepath.Join(filepath.Dir(root), sibling)
		}
	}
	name = "Claude"
	if third {
		name = "Claude-3p"
	}
	exact := filepath.Join(root, name)
	if _, e := os.Stat(exact); e == nil {
		return exact
	}
	if runtime.GOOS == "windows" {
		entries, _ := os.ReadDir(root)
		candidates := []string{}
		for _, v := range entries {
			if v.IsDir() && strings.HasPrefix(v.Name(), "Claude") && strings.Contains(v.Name(), "-3p") == third {
				p := filepath.Join(root, v.Name())
				if isFile(filepath.Join(p, "claude_desktop_config.json")) || isFile(filepath.Join(p, "configLibrary", "_meta.json")) {
					candidates = append(candidates, p)
				}
			}
		}
		sort.Strings(candidates)
		if len(candidates) == 1 {
			return candidates[0]
		}
	}
	return exact
}
func Specifications(client, dir string, binding, editor bool) ([]Spec, error) {
	out := []Spec{}
	add := func(path, format string, keys []string) {
		out = append(out, Spec{path, format, append([]string{}, keys...)})
	}
	switch client {
	case "codex":
		add(filepath.Join(dir, "config.toml"), "toml", CodexKeys)
		add(filepath.Join(dir, "uni-switch-models.json"), "catalog", nil)
		if editor {
			add(filepath.Join(dir, "auth.json"), "json", nil)
		}
	case "claude_cli":
		add(filepath.Join(dir, "settings.json"), "json", clientKeys)
	case "claude_desktop":
		normal, third := desktopDir(dir, false), desktopDir(dir, true)
		add(filepath.Join(normal, "claude_desktop_config.json"), "json", []string{"deploymentMode"})
		if normal != third {
			add(filepath.Join(third, "claude_desktop_config.json"), "json", []string{"deploymentMode"})
		}
		add(filepath.Join(third, "configLibrary", ProfileID+".json"), "json", profileKeys)
		add(filepath.Join(third, "configLibrary", "_meta.json"), "meta", []string{"appliedId", "entries"})
	case "zcode":
		p := filepath.Join(dir, "provider_config.json")
		if binding && filepath.IsAbs(os.Getenv("ZCODE_PERSONAL_PROVIDER_CONFIG_FILE")) {
			p = os.Getenv("ZCODE_PERSONAL_PROVIDER_CONFIG_FILE")
		}
		add(p, "json", nil)
	case "dsh":
		add(filepath.Join(dir, "cordis.patch.yml"), "yaml", nil)
		add(filepath.Join(dir, ".credentials.yaml"), "yaml", nil)
	case "workbuddy":
		add(filepath.Join(dir, "models.json"), "json", nil)
	default:
		return extraSpecifications(client, dir, binding)
	}
	if editor {
		for i := range out {
			if out[i].Format == "catalog" || out[i].Format == "meta" {
				out[i].Format = "json"
			}
		}
		if client == "claude_desktop" {
			initial := append([]Spec{}, out...)
			for _, s := range initial {
				if filepath.Base(s.Path) != "_meta.json" {
					continue
				}
				if e := CheckPath(s.Path); e != nil {
					return nil, e
				}
				v, e := edit.Read(s.Path)
				if e != nil {
					return nil, e
				}
				doc, _ := jsonObject(v)
				id := text(doc["appliedId"])
				if _, e := uuid.Parse(id); e == nil {
					path := filepath.Join(filepath.Dir(s.Path), id+".json")
					found := false
					for _, s := range out {
						found = found || s.Path == path
					}
					if !found {
						add(path, "json", nil)
					}
				}
			}
		}
	}
	return out, nil
}
func CheckPath(path string) error { return edit.CheckPath(path) }
func Revision(path string, content *string) string {
	h := sha256.New()
	h.Write([]byte(path))
	h.Write([]byte{0})
	if content == nil {
		h.Write([]byte{0})
	} else {
		h.Write([]byte{1})
		h.Write([]byte(*content))
	}
	return hex.EncodeToString(h.Sum(nil))
}
func Validate(format, text string) error {
	if len(text) > 4<<20 || strings.ContainsRune(text, 0) {
		return fail("invalid_config", "配置文件超过 4 MiB 或包含无效字符")
	}
	switch format {
	case "json", "meta", "catalog":
		_, e := jsonObject(&text)
		return e
	case "jsonc", "jsonc_array":
		_, e := parseConfig(File{Format: format, Content: &text})
		return e
	case "env", "script":
		return nil
	case "toml":
		var v Object
		if toml.Unmarshal([]byte(text), &v) != nil {
			return fail("invalid_toml", "TOML 格式无效，请检查后保存")
		}
	case "yaml":
		var v yaml.Node
		if yaml.Unmarshal([]byte(text), &v) != nil {
			return fail("invalid_yaml", "YAML 格式无效，请检查后保存")
		}
	default:
		return fail("invalid_format", "不支持此配置格式")
	}
	return nil
}
