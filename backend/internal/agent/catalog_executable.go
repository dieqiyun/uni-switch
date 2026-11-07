package agent

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// These launchers never start a client while applying configuration. Resolve
// its installed program now, with an explicit override in the generated script
// for portable installations or programs installed after configuration.
func installedClientExecutable(client string) string {
	home, _ := os.UserHomeDir()
	if client == "claudescience" {
		return findScienceExecutable(home, runtime.GOOS)
	}
	return findPrivateCursor(cursorInstallRoots(home, runtime.GOOS), runtime.GOOS)
}

func findScienceExecutable(home, goos string) string {
	paths := []string{}
	name := "claude-science"
	switch goos {
	case "windows":
		name += ".exe"
		local := envDirectory(home, "LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
		paths = append(paths, filepath.Join(local, "Programs", "ClaudeScience", name))
	case "darwin":
		for _, root := range []string{filepath.Join(home, "Applications"), "/Applications"} {
			paths = append(paths, filepath.Join(root, "Claude Science.app", "Contents", "Resources", "bin", name))
		}
	}
	paths = append(paths, filepath.Join(home, ".local", "bin", name))
	for _, path := range paths {
		if isFile(path) {
			return path
		}
	}
	if path, e := exec.LookPath(name); e == nil {
		return path
	}
	return ""
}

func cursorInstallRoots(home, goos string) []string {
	switch goos {
	case "windows":
		roots := []string{}
		for _, dir := range []string{envDirectory(home, "LOCALAPPDATA", filepath.Join(home, "AppData", "Local")), os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if filepath.IsAbs(dir) {
				roots = append(roots, filepath.Join(dir, "Programs"), dir)
			}
		}
		return roots
	case "darwin":
		return []string{filepath.Join(home, "Applications"), "/Applications"}
	default:
		return []string{"/opt", "/usr/share", "/usr/lib", filepath.Join(home, ".local", "share"), filepath.Join(home, "Applications"), filepath.Join(home, ".local", "bin"), filepath.Join(home, "bin"), filepath.Join(home, "Downloads"), home}
	}
}

func findPrivateCursor(roots []string, goos string) string {
	for _, root := range roots {
		entries, _ := os.ReadDir(root)
		for _, entry := range entries {
			if path := privateCursorAt(filepath.Join(root, entry.Name()), goos); path != "" {
				return path
			}
		}
	}
	return ""
}

var bundleExecutable = regexp.MustCompile(`<key>CFBundleExecutable</key>\s*<string>([^<]+)</string>`)

func privateCursorAt(dir, goos string) string {
	name := strings.ToLower(filepath.Base(dir))
	if goos == "linux" && strings.HasSuffix(name, ".appimage") && strings.HasPrefix(strings.NewReplacer("_", "", "-", "", " ", "").Replace(name), "cursorprivateinference") && isFile(dir) {
		return dir
	}
	resource := filepath.Join(dir, "resources", "app", "product.json")
	mac := goos == "darwin" && strings.HasSuffix(name, ".app")
	if mac {
		resource = filepath.Join(dir, "Contents", "Resources", "app", "product.json")
	}
	var product struct{ NameShort, ApplicationName string }
	raw, e := os.ReadFile(resource)
	if e != nil || json.Unmarshal(raw, &product) != nil || !strings.EqualFold(strings.TrimSpace(product.NameShort), "Cursor Private Inference") {
		return ""
	}
	app := product.ApplicationName
	if app == "" {
		app = "cursor"
	}
	if app == "." || app == ".." || strings.ContainsAny(app, `/\`) {
		return ""
	}
	exes := []string{filepath.Join(dir, app)}
	if goos == "windows" {
		exes = []string{filepath.Join(dir, "Cursor Private Inference.exe"), filepath.Join(dir, strings.ToUpper(app[:1])+app[1:]+".exe")}
	} else if mac {
		app = strings.ToUpper(app[:1]) + app[1:]
		if plist, e := os.ReadFile(filepath.Join(dir, "Contents", "Info.plist")); e == nil {
			if match := bundleExecutable.FindSubmatch(plist); match != nil {
				candidate := strings.TrimSpace(string(match[1]))
				if candidate != "" && candidate != "." && candidate != ".." && !strings.ContainsAny(candidate, `/\`) {
					app = candidate
				}
			}
		}
		exes = []string{filepath.Join(dir, "Contents", "MacOS", app)}
	}
	for _, path := range exes {
		if isFile(path) {
			return path
		}
	}
	return ""
}

func clientLaunchScript(env Object, client, command string, args ...string) string {
	variable := "UNI_SWITCH_CURSOR_LOCAL_BINARY"
	message := "Cursor Private Inference was not found. Install it or set " + variable + " to its absolute executable path."
	if client == "claudescience" {
		variable = "UNI_SWITCH_CLAUDE_SCIENCE_BINARY"
		message = "Claude Science was not found. Install it or set " + variable + " to the absolute claude-science executable path."
	}
	script := launchScript(env, command, args...)
	quote := powershellLiteral
	if runtime.GOOS == "windows" {
		preflight := "$clientBinary = $env:" + variable + "\nif (-not $clientBinary) { $clientBinary = " + quote(installedClientExecutable(client)) + " }\nif (-not $clientBinary -or -not (Test-Path -LiteralPath $clientBinary -PathType Leaf)) { throw " + quote(message) + " }\n"
		script = strings.Replace(script, "& "+quote(command), preflight+"& $clientBinary", 1)
		if client == "claudescience" {
			script = strings.Replace(script, "& $clientBinary", "& $clientBinary 'stop'\n& $clientBinary", 1)
		}
	} else {
		quote = func(v string) string { return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'" }
		preflight := "client_binary=${" + variable + "-}\nif [ -z \"$client_binary\" ]; then client_binary=" + quote(installedClientExecutable(client)) + "; fi\nif [ -z \"$client_binary\" ] || [ ! -x \"$client_binary\" ]; then printf '%s\\n' " + quote(message) + " >&2; exit 1; fi\n"
		script = strings.Replace(script, "exec "+quote(command), preflight+"exec \"$client_binary\"", 1)
		if client == "claudescience" {
			script = strings.Replace(script, "exec \"$client_binary\"", "\"$client_binary\" 'stop' || true\nexec \"$client_binary\"", 1)
		}
	}
	return script
}

func openCodeExecutable() string {
	home, _ := os.UserHomeDir()
	if path, e := exec.LookPath("opencode"); e == nil {
		if absolute, e := filepath.Abs(path); e == nil {
			return absolute
		}
	}
	if path := findBundledOpenCode(home, runtime.GOOS); path != "" {
		return path
	}
	return "opencode"
}

// GUI applications often have no shell PATH. Air can also use OpenCode
// downloaded into an IDE's or Zed's ACP registry.
func findBundledOpenCode(home, goos string) string {
	name := "opencode"
	if goos == "windows" {
		name += ".exe"
	}
	if path := filepath.Join(home, ".opencode", "bin", name); isFile(path) {
		return path
	}
	var patterns []string
	switch goos {
	case "windows":
		local := envDirectory(home, "LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
		patterns = []string{filepath.Join(local, "JetBrains", "*", "acp-agents", "opencode", "*", name), filepath.Join(local, "Zed", "external_agents", "registry", "opencode", "*", name)}
	case "darwin":
		patterns = []string{filepath.Join(home, "Library", "Caches", "JetBrains", "*", "acp-agents", "opencode", "*", name), filepath.Join(home, "Library", "Application Support", "Zed", "external_agents", "registry", "opencode", "*", name)}
	default:
		cache := envDirectory(home, "XDG_CACHE_HOME", filepath.Join(home, ".cache"))
		data := envDirectory(home, "XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
		patterns = []string{filepath.Join(cache, "JetBrains", "*", "acp-agents", "opencode", "*", name), filepath.Join(data, "zed", "external_agents", "registry", "opencode", "*", name)}
	}
	best, modified := "", time.Time{}
	for _, pattern := range patterns {
		paths, _ := filepath.Glob(pattern)
		for _, path := range paths {
			info, e := os.Stat(path)
			if e == nil && info.Mode().IsRegular() && (best == "" || info.ModTime().After(modified)) {
				best, modified = path, info.ModTime()
			}
		}
	}
	return best
}
