package agent

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func envDirectory(home, key, fallback string) string {
	v := os.Getenv(key)
	if v == "~" || strings.HasPrefix(v, "~/") || strings.HasPrefix(v, `~\`) {
		v = filepath.Join(home, v[1:])
	}
	if filepath.IsAbs(v) {
		return filepath.Clean(v)
	}
	return fallback
}

func configHome(home string) string {
	if runtime.GOOS == "windows" {
		return envDirectory(home, "APPDATA", filepath.Join(home, "AppData", "Roaming"))
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support")
	}
	return envDirectory(home, "XDG_CONFIG_HOME", filepath.Join(home, ".config"))
}

func extraDirectory(client, home string) (string, error) {
	cfg := envDirectory(home, "XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	defs := map[string][2]string{
		"opencode":         {"OPENCODE_CONFIG_DIR", filepath.Join(cfg, "opencode")},
		"mimocode":         {"MIMOCODE_HOME", filepath.Join(cfg, "mimocode")},
		"deveco":           {"DEVECO_CONFIG_DIR", filepath.Join(cfg, "deveco")},
		"pi":               {"PI_CODING_AGENT_DIR", filepath.Join(home, ".pi", "agent")},
		"omo":              {"OMO_CODING_AGENT_DIR", envDirectory(home, "SENPI_CODING_AGENT_DIR", filepath.Join(home, ".omo", "agent"))},
		"primeagent":       {"PRIME_AGENT_CODING_AGENT_DIR", filepath.Join(home, ".prime", "agent")},
		"stepcode":         {"STEPCODE_CONFIG_DIR", filepath.Join(home, ".stepcode")},
		"mothx":            {"MOTHX_DIR", filepath.Join(configHome(home), "mothx")},
		"omp":              {"PI_CODING_AGENT_DIR", filepath.Join(home, ".omp", "agent")},
		"goose":            {"GOOSE_CONFIG_DIR", filepath.Join(cfg, "goose")},
		"crush":            {"CRUSH_GLOBAL_CONFIG", filepath.Join(cfg, "crush")},
		"copilot":          {"COPILOT_HOME", filepath.Join(home, ".copilot")},
		"hermes":           {"HERMES_HOME", filepath.Join(home, ".hermes")},
		"morph":            {"", filepath.Join(home, ".morph")},
		"kimi":             {"KIMI_CODE_HOME", filepath.Join(home, ".kimi-code")},
		"qwen":             {"QWEN_CODE_HOME", filepath.Join(home, ".qwen")},
		"openclaw":         {"OPENCLAW_STATE_DIR", filepath.Join(home, ".openclaw")},
		"jcode":            {"JCODE_HOME", filepath.Join(home, ".jcode")},
		"droid":            {"FACTORY_HOME", filepath.Join(home, ".factory")},
		"qoder":            {"QODER_CONFIG_DIR", filepath.Join(home, ".qoder")},
		"qodercn":          {"QODERCN_CONFIG_DIR", filepath.Join(home, ".qoder-cn")},
		"pencil":           {"", filepath.Join(home, ".pencil")},
		"openpencil":       {"", filepath.Join(configHome(home), "openpencil")},
		"workbuddyai":      {"", filepath.Join(home, ".workbuddy-ai")},
		"codebuddy":        {"CODEBUDDY_CONFIG_DIR", filepath.Join(home, ".codebuddy")},
		"liveagent":        {"LIVEAGENT_HOME", envDirectory(home, "K_BRAIN_HOME", filepath.Join(home, ".liveagent"))},
		"raycast":          {"", filepath.Join(home, ".config", "raycast", "ai")},
		"astrbot":          {"ASTRBOT_ROOT", filepath.Join(home, ".astrbot")},
		"antigravity":      {"", filepath.Join(home, ".antigravity")},
		"atomcode":         {"ATOMCODE_HOME", filepath.Join(home, ".atomcode")},
		"vscode":           {"", filepath.Join(configHome(home), "Code", "User")},
		"vscodeinsiders":   {"", filepath.Join(configHome(home), "Code - Insiders", "User")},
		"vscodium":         {"", filepath.Join(configHome(home), "VSCodium", "User")},
		"copilotjetbrains": {"", filepath.Join(home, ".config", "github-copilot")},
		"snow":             {"SNOW_CONFIG_DIR", filepath.Join(home, ".snow")},
		"minimax":          {"MINIMAX_DATA_DIR", filepath.Join(home, ".minimax")},
		"cline":            {"CLINE_DIR", filepath.Join(home, ".cline", "data", "settings")},
		"qwenpaw":          {"QWENPAW_WORKING_DIR", filepath.Join(home, ".qwenpaw")},
		"ante":             {"ANTE_HOME", filepath.Join(home, ".ante")},
		"grok":             {"GROK_HOME", filepath.Join(home, ".grok")},
		"grokapp":          {"GROK_APP_HOME", filepath.Join(configHome(home), "grokapp", "grok-app", "data", "agent-home")},
		"t3code":           {"T3CODE_HOME", filepath.Join(home, ".t3")},
		"dsbot":            {"DS_BOT_HOME", filepath.Join(home, ".ds-bot")},
		"reasonix":         {"REASONIX_HOME", filepath.Join(home, ".reasonix")},
		"air":              {"", filepath.Join(configHome(home), "JetBrains", "Air")},
		"airide":           {"", filepath.Join(home, ".jetbrains")},
		"gemini":           {"", filepath.Join(home, ".gemini")},
		"agy":              {"", filepath.Join(home, ".gemini", "antigravity-cli")},
		"claudescience":    {"", filepath.Join(home, ".claude-science")},
		"cursorlocal":      {"", filepath.Join(home, ".cursor")},
		"openchamber":      {"OPENCHAMBER_DATA_DIR", filepath.Join(home, ".config", "openchamber")},
		"penguin":          {"PENGUIN_HOME", filepath.Join(home, ".penguin", "projects", "default")},
		"aside":            {"", filepath.Join(home, ".aside", "u", "0")},
		"zed":              {"ZED_CONFIG_DIR", filepath.Join(home, ".config", "zed")},
		"zoocode":          {"", filepath.Join(configHome(home), "Code", "User")},
		"commandcode":      {"", filepath.Join(home, ".commandcode")},
		"fx":               {"", filepath.Join(home, ".fx")},
		"muse":             {"", filepath.Join(cfg, "muse")},
		"empryo":           {"", filepath.Join(home, ".empryo")},
		"openbot":          {"", filepath.Join(configHome(home), "OpenBot")},
		"kirocrew":         {"KIRO_CREW_HOME", filepath.Join(home, ".kiro", "crew")},
		"openmausbot":      {"OMB_DATA_DIR", filepath.Join(home, ".openmausbot")},
		"cindy":            {"", filepath.Join(configHome(home), "Cindy")},
		"aionui":           {"", filepath.Join(configHome(home), "AionUi")},
		"lody":             {"", filepath.Join(home, ".lody")},
		"newmax":           {"NEWMAX_USER_DATA_DIR", filepath.Join(home, ".newmax")},
		"hanako":           {"HANA_HOME", filepath.Join(home, ".hanako")},
		"alma":             {"ALMA_DATA_DIR", filepath.Join(configHome(home), "alma")},
	}
	if d, ok := defs[client]; ok && d[0] != "" && filepath.IsAbs(os.Getenv(d[0])) && client != "mimocode" && client != "penguin" && client != "omp" && client != "hermes" {
		return filepath.Clean(os.Getenv(d[0])), nil
	}
	if client == "mothx" && runtime.GOOS != "windows" {
		return envDirectory(home, "MOTHX_DIR", filepath.Join(home, ".mothx")), nil
	}
	if client == "droid" {
		return filepath.Join(envDirectory(home, "FACTORY_HOME_OVERRIDE", home), ".factory"), nil
	}
	if client == "penguin" {
		root := envDirectory(home, "PENGUIN_HOME", filepath.Join(home, ".penguin", "data"))
		id := strings.TrimSpace(os.Getenv("PENGUIN_PROJECT_ID"))
		if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
			id = "default_project"
		}
		return filepath.Join(root, id), nil
	}
	if client == "omp" {
		root := filepath.Join(home, ".omp")
		if d := os.Getenv("PI_CONFIG_DIR"); d != "" {
			root = filepath.Join(home, d)
		}
		profile, ok := os.LookupEnv("OMP_PROFILE")
		if !ok {
			profile = os.Getenv("PI_PROFILE")
		}
		if profile != "" && profile != "default" && profile != "." && profile != ".." && !strings.ContainsAny(profile, `/\`) {
			return filepath.Join(root, "profiles", profile, "agent"), nil
		}
		return envDirectory(home, "PI_CODING_AGENT_DIR", filepath.Join(root, "agent")), nil
	}
	if client == "hermes" {
		fallback := filepath.Join(home, ".hermes")
		if runtime.GOOS == "windows" {
			fallback = filepath.Join(envDirectory(home, "LOCALAPPDATA", filepath.Join(home, "AppData", "Local")), "hermes")
		}
		root := envDirectory(home, "HERMES_HOME", fallback)
		if filepath.Base(filepath.Dir(root)) != "profiles" {
			b, _ := os.ReadFile(filepath.Join(root, "active_profile"))
			id := strings.TrimSpace(string(b))
			if id != "" && id != "." && id != ".." && !strings.ContainsAny(id, `/\`) && isDirectory(filepath.Join(root, "profiles", id)) && !isDirectory(filepath.Join(root, "profiles", ".deleted", id)) {
				root = filepath.Join(root, "profiles", id)
			}
		}
		return root, nil
	}
	if client == "qwenpaw" {
		fallback := filepath.Join(home, ".qwenpaw")
		if isDirectory(filepath.Join(home, ".copaw")) {
			fallback = filepath.Join(home, ".copaw")
		}
		return envDirectory(home, "QWENPAW_WORKING_DIR", fallback), nil
	}
	if client == "openclaw" {
		base := envDirectory(home, "OPENCLAW_HOME", home)
		fallback := filepath.Join(base, ".openclaw")
		if p := os.Getenv("OPENCLAW_PROFILE"); p != "" && p != "default" && p != "." && p != ".." && !strings.ContainsAny(p, `/\`) {
			fallback += "-" + p
		}
		return envDirectory(base, "OPENCLAW_STATE_DIR", fallback), nil
	}
	if client == "dsbot" {
		if p := os.Getenv("DS_BOT_HOME"); filepath.IsAbs(p) {
			return p, nil
		}
		user := envDirectory(home, "DS_BOT_SHELL_DATA", filepath.Join(configHome(home), "DS Bot"))
		if os.Getenv("DS_BOT_SHELL_DATA") == "" && !isFile(filepath.Join(user, "locations.json")) && isDirectory(filepath.Join(configHome(home), "DeepSeek Bot")) {
			user = filepath.Join(configHome(home), "DeepSeek Bot")
		}
		b, e := os.ReadFile(filepath.Join(user, "locations.json"))
		if e == nil {
			var data Object
			if json.Unmarshal(b, &data) != nil || !filepath.IsAbs(text(data["dataDir"])) {
				return "", fail("invalid_config", "DS Bot 的 locations.json 无法解析，请在客户端确认数据目录")
			}
			return text(data["dataDir"]), nil
		}
		if !os.IsNotExist(e) {
			return "", fail("file_error", "无法读取 DS Bot 位置记录")
		}
	}
	if client == "vscode" || client == "vscodeinsiders" || client == "vscodium" {
		name := map[string]string{"vscode": "code", "vscodeinsiders": "code-insiders", "vscodium": "codium"}[client]
		if bin, e := exec.LookPath(name); e == nil {
			if resolved, e := filepath.EvalSymlinks(bin); e == nil {
				bin = resolved
			}
			root := filepath.Dir(bin)
			if filepath.Base(root) == "bin" {
				root = filepath.Dir(root)
			}
			data := filepath.Join(root, "data")
			if isDirectory(filepath.Join(data, "user-data", "User")) {
				return filepath.Join(data, "user-data", "User"), nil
			}
		}
	}
	if client == "copilotjetbrains" && runtime.GOOS == "windows" {
		return filepath.Join(envDirectory(home, "LOCALAPPDATA", filepath.Join(home, "AppData", "Local")), "github-copilot"), nil
	}
	if client == "reasonix" && runtime.GOOS == "windows" && os.Getenv("REASONIX_HOME") == "" {
		return filepath.Join(configHome(home), "reasonix"), nil
	}
	if client == "goose" && runtime.GOOS == "windows" {
		return filepath.Join(configHome(home), "Block", "goose", "config"), nil
	}
	if client == "crush" && runtime.GOOS == "windows" {
		return filepath.Join(envDirectory(home, "LOCALAPPDATA", filepath.Join(home, "AppData", "Local")), "crush"), nil
	}
	if client == "mimocode" && filepath.IsAbs(os.Getenv("MIMOCODE_HOME")) {
		return filepath.Join(os.Getenv("MIMOCODE_HOME"), "config"), nil
	}
	if client == "kimi" && !isDirectory(filepath.Join(home, ".kimi-code")) && isDirectory(filepath.Join(home, ".kimi")) {
		return filepath.Join(home, ".kimi"), nil
	}
	if d, ok := defs[client]; ok {
		return envDirectory(home, d[0], d[1]), nil
	}
	return "", fail("invalid_client", "未知客户端")
}
func isDirectory(path string) bool { s, e := os.Stat(path); return e == nil && s.IsDir() }

func extraSpecifications(client, dir string, binding bool) ([]Spec, error) {
	var specs []Spec
	add := func(name, format string) { specs = append(specs, Spec{Path: filepath.Join(dir, name), Format: format}) }
	switch client {
	case "newmax":
		add("newmax.db", "newmax")
	case "alma":
		add("local-api", "alma")
	case "hanako":
		add("provider-catalog.json", "hanako_provider")
		id := "default"
		raw, _ := os.ReadFile(filepath.Join(dir, "user", "preferences.json"))
		var prefs Object
		_ = json.Unmarshal(raw, &prefs)
		valid := func(s string) bool {
			return s != "" && !strings.ContainsAny(s, `/\`) && isFile(filepath.Join(dir, "agents", s, "config.yaml"))
		}
		if v := text(prefs["primaryAgent"]); valid(v) {
			id = v
		} else {
			entries, _ := os.ReadDir(filepath.Join(dir, "agents"))
			for _, e := range entries {
				if e.IsDir() && valid(e.Name()) {
					id = e.Name()
					break
				}
			}
		}
		add(filepath.Join("agents", id, "config.yaml"), "hanako_chat")
	case "cindy", "aionui", "lody":
		return []Spec{}, nil
	case "gemini":
		add("settings.json", "json")
		add(".env", "env")
	case "agy":
		add("settings.json", "json")
		add(launchName(), "script")
	case "claudescience", "cursorlocal":
		add(launchName(), "script")
	case "openchamber":
		add("settings.json", "json")
		add("preferences.json", "json")
		add("uni-switch-opencode.json", "jsonc")
		add(launchName(), "script")
	case "penguin":
		add(".project_config.toml", "toml")
	case "aside":
		add("settings.json", "json")
		add("models.json", "json")
	case "zed":
		add("settings.json", "jsonc")
	case "zoocode":
		add("settings.json", "jsonc")
		add("globalStorage/zoocode.uni-switch/import.json", "json")
	case "commandcode":
		add("settings.json", "json")
		add("providers.json", "json")
	case "fx":
		add("settings.json", "json")
	case "muse":
		add("settings.json", "json")
	case "empryo":
		add("config.json", "json")
	case "openbot":
		add("openbot-custom-providers-v1.json", "json")
	case "kirocrew":
		add("config.json", "json")
		add("config.local.json", "json")
	case "openmausbot":
		add("config.json", "json")
	case "vscode", "vscodeinsiders", "vscodium":
		add("settings.json", "jsonc")
		add("chatLanguageModels.json", "jsonc_array")
		specs = append(specs, vscodeProfileSpecs(dir)...)
	case "copilotjetbrains":
		add("byok.json", "json")
	case "snow":
		add("config.json", "jsonc")
		add("profiles/uni-switch.json", "jsonc")
		add("active-profile.json", "json")
	case "minimax":
		add("config.yaml", "yaml")
	case "cline":
		add("providers.json", "json")
		add("models.json", "json")
		if isFile(filepath.Join(dir, "globalState.json")) {
			add("globalState.json", "json")
			add("secrets.json", "json")
		}
	case "qwenpaw":
		profiles := Profiles(client, dir)
		if len(profiles) > 0 {
			specs = append(specs, Spec{Path: filepath.Join(profiles[0].Directory, "agent.json"), Format: "json"})
		} else {
			add("workspaces/default/agent.json", "json")
		}
		secret := dir + ".secret"
		if d := dotenvValue(filepath.Join(dir, ".env"), "QWENPAW_SECRET_DIR"); d != "" {
			home, _ := os.UserHomeDir()
			if d == "~" || strings.HasPrefix(d, "~/") || strings.HasPrefix(d, `~\`) {
				d = filepath.Join(home, d[1:])
			}
			if filepath.IsAbs(d) {
				secret = d
			}
		}
		if binding {
			home, _ := os.UserHomeDir()
			secret = envDirectory(home, "QWENPAW_SECRET_DIR", secret)
		}
		specs = append(specs, Spec{Path: filepath.Join(secret, "providers", "custom", "uni-switch.json"), Format: "json"})
	case "ante":
		add("catalog.json", "json")
		add("settings.json", "json")
	case "grok", "grokapp":
		add("config.toml", "toml")
	case "t3code":
		add("userdata/settings.json", "json")
		add("userdata/uni-switch-codex-models.json", "json")
	case "dsbot":
		add("profiles/ds-bot/cordis.patch.yml", "yaml")
		add(".credentials.yaml", "yaml")
	case "reasonix":
		add("config.toml", "toml")
		add(".env", "env")
	case "air", "airide":
		add("acp.json", "json")
		add("uni-switch-opencode.json", "json")
	case "opencode", "mimocode", "deveco":
		name := client + ".json"
		for _, n := range []string{client + ".jsonc", client + ".json", "config.json"} {
			if isFile(filepath.Join(dir, n)) {
				name = n
				break
			}
		}
		add(name, "jsonc")
	case "pi", "omo", "primeagent":
		add("settings.json", "json")
		add("models.json", "json")
	case "stepcode":
		add("config.toml", "toml")
		add("models.json", "json")
	case "omp":
		for _, n := range []string{"config", "models"} {
			ext := ".yml"
			if !isFile(filepath.Join(dir, n+".yml")) && isFile(filepath.Join(dir, n+".yaml")) {
				ext = ".yaml"
			}
			add(n+ext, "yaml")
		}
		if !isFile(specs[1].Path) && isFile(filepath.Join(dir, "models.json")) {
			add("models.json", "json")
		}
	case "goose":
		add("config.yaml", "yaml")
		add("custom_providers/uni-switch.json", "json")
	case "crush":
		add("crush.json", "jsonc")
		if binding && runtime.GOOS != "windows" {
			home, _ := os.UserHomeDir()
			p := filepath.Join(envDirectory(home, "XDG_DATA_HOME", filepath.Join(home, ".local", "share")), "crush", "crush.json")
			if p != specs[0].Path {
				specs = append(specs, Spec{Path: p, Format: "jsonc"})
			}
		}
	case "copilot":
		add("settings.json", "json")
		add("providers.json", "json")
		if p := os.Getenv("COPILOT_PROVIDERS_CONFIG"); binding && filepath.IsAbs(p) {
			specs[1].Path = p
		}
	case "hermes", "morph":
		add("config.yaml", "yaml")
		if p := os.Getenv("MISTER_MORPH_CONFIG"); client == "morph" && binding && filepath.IsAbs(p) {
			specs[0].Path = p
		}
	case "kimi", "jcode", "atomcode":
		add("config.toml", "toml")
	case "qwen":
		add("settings.json", "json")
		add(".env", "env")
	case "openclaw":
		add("openclaw.json", "jsonc")
		if p := os.Getenv("OPENCLAW_CONFIG_PATH"); binding && filepath.IsAbs(p) {
			specs[0].Path = p
		}
	case "workbuddyai", "pencil":
		add("models.json", "json")
	case "codebuddy":
		add("models.json", "json")
		add("settings.json", "json")
	case "liveagent":
		add("config.json", "jsonc")
	case "raycast":
		add("providers.yaml", "yaml")
	case "astrbot":
		add("data/cmd_config.json", "json")
	case "mothx", "droid", "qoder", "qodercn", "openpencil", "antigravity":
		add("settings.json", "json")
	default:
		return nil, fail("invalid_client", "未知客户端")
	}
	return specs, nil
}

func FileFormat(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".toml":
		return "toml"
	case ".yml", ".yaml":
		return "yaml"
	case ".jsonc":
		return "jsonc"
	case ".env":
		return "env"
	}
	if filepath.Base(path) == ".env" {
		return "env"
	}
	return "json"
}

func launchName() string {
	if runtime.GOOS == "windows" {
		return "uni-switch-launch.ps1"
	}
	return "uni-switch-launch.sh"
}
