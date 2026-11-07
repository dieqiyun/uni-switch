package agent

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPrivateCursorDiscoveryRequiresPrivateProductAndRealExecutable(t *testing.T) {
	for _, goos := range []string{"windows", "darwin", "linux"} {
		t.Run(goos, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "Cursor")
			product := filepath.Join(dir, "resources", "app", "product.json")
			exe := filepath.Join(dir, "cursor")
			if goos == "windows" {
				exe = filepath.Join(dir, "Cursor.exe")
			} else if goos == "darwin" {
				dir += ".app"
				product = filepath.Join(dir, "Contents", "Resources", "app", "product.json")
				exe = filepath.Join(dir, "Contents", "MacOS", "CustomCursor")
				writeExecutableFixture(t, filepath.Join(dir, "Contents", "Info.plist"), `<plist><key>CFBundleExecutable</key><string>CustomCursor</string></plist>`)
			}
			writeExecutableFixture(t, product, `{"nameShort":"Cursor","applicationName":"cursor"}`)
			writeExecutableFixture(t, exe, "synthetic-never-execute")
			if got := findPrivateCursor([]string{root}, goos); got != "" {
				t.Fatal("ordinary Cursor accepted", got)
			}
			writeExecutableFixture(t, product, `{"nameShort":"Cursor Private Inference","applicationName":"cursor"}`)
			if got := findPrivateCursor([]string{root}, goos); got != exe {
				t.Fatal("private build not found", got)
			}
			if e := os.Remove(exe); e != nil {
				t.Fatal(e)
			}
			if got := findPrivateCursor([]string{root}, goos); got != "" {
				t.Fatal("missing program accepted", got)
			}
		})
	}
	root := t.TempDir()
	writeExecutableFixture(t, filepath.Join(root, "Cursor-3.0.AppImage"), "synthetic")
	if got := findPrivateCursor([]string{root}, "linux"); got != "" {
		t.Fatal("ordinary AppImage accepted")
	}
	image := filepath.Join(root, "Cursor_Private_Inference-3.0.AppImage")
	writeExecutableFixture(t, image, "synthetic")
	if got := findPrivateCursor([]string{root}, "linux"); got != image {
		t.Fatal("private AppImage missing")
	}
}

func writeExecutableFixture(t *testing.T, path, content string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte(content), 0700); e != nil {
		t.Fatal(e)
	}
}

func TestScienceInstallDiscoveryAndLaunchersOnlyGenerateScripts(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "Local"))
	for _, goos := range []string{"windows", "darwin", "linux"} {
		home := filepath.Join(root, goos)
		exe := filepath.Join(home, ".local", "bin", "claude-science")
		if goos == "windows" {
			exe = filepath.Join(root, "Local", "Programs", "ClaudeScience", "claude-science.exe")
		} else if goos == "darwin" {
			exe = filepath.Join(home, "Applications", "Claude Science.app", "Contents", "Resources", "bin", "claude-science")
		}
		writeExecutableFixture(t, exe, "synthetic-never-execute")
		if got := findScienceExecutable(home, goos); got != exe {
			t.Fatal("installed daemon missing", goos, got)
		}
	}
	for _, client := range []string{"claudescience", "cursorlocal"} {
		script := clientLaunchScript(Object{"SYNTHETIC_KEY": "literal'$`\\value"}, client, "placeholder", "serve")
		if !strings.Contains(script, "UNI_SWITCH_") || strings.Contains(script, "& 'placeholder'") || strings.Contains(script, "exec 'placeholder'") {
			t.Fatal("launcher bypassed executable resolution", script)
		}
		if client == "claudescience" && !strings.Contains(script, "'stop'") {
			t.Fatal("daemon not restarted by launcher")
		}
		if runtime.GOOS == "windows" && !strings.Contains(script, "Test-Path -LiteralPath") {
			t.Fatal("missing binary not checked")
		}
	}
}

func TestAirFindsBundledACPExecutable(t *testing.T) {
	for _, goos := range []string{"windows", "darwin", "linux"} {
		t.Run(goos, func(t *testing.T) {
			home := t.TempDir()
			local := filepath.Join(home, "Local")
			t.Setenv("LOCALAPPDATA", local)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
			t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
			name := "opencode"
			root := filepath.Join(home, ".cache", "JetBrains", "IDE2026.3", "acp-agents", "opencode")
			if goos == "windows" {
				name += ".exe"
				root = filepath.Join(local, "JetBrains", "IDE2026.3", "acp-agents", "opencode")
			} else if goos == "darwin" {
				root = filepath.Join(home, "Library", "Caches", "JetBrains", "IDE2026.3", "acp-agents", "opencode")
			}
			first, latest := filepath.Join(root, "1", name), filepath.Join(root, "2", name)
			writeExecutableFixture(t, first, "synthetic")
			writeExecutableFixture(t, latest, "synthetic")
			old := time.Now().Add(-time.Hour)
			if e := os.Chtimes(first, old, old); e != nil {
				t.Fatal(e)
			}
			if got := findBundledOpenCode(home, goos); got != latest {
				t.Fatal("latest registry executable missing", got)
			}
			installed := filepath.Join(home, ".opencode", "bin", name)
			writeExecutableFixture(t, installed, "synthetic")
			if got := findBundledOpenCode(home, goos); got != installed {
				t.Fatal("installer executable missing", got)
			}
		})
	}
}

func TestWindowsLauncherPreservesUnicodeAndLiteralArguments(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell 5 execution contract")
	}
	dir := t.TempDir()
	program := filepath.Join(dir, "中文 client's program.ps1")
	record := filepath.Join(dir, "calls.jsonl")
	writeExecutableFixture(t, program, `[System.IO.File]::AppendAllText($env:UNI_SWITCH_QA_RECORD, ((@{value=$env:SYNTHETIC_KEY; arguments=@($args)} | ConvertTo-Json -Compress) + [Environment]::NewLine), [System.Text.UTF8Encoding]::new($false))`)
	t.Setenv("UNI_SWITCH_CLAUDE_SCIENCE_BINARY", program)
	t.Setenv("UNI_SWITCH_QA_RECORD", record)
	value := "中文 $literal `tick` 'quote' \"double\" \\path"
	script := clientLaunchScript(Object{"SYNTHETIC_KEY": value}, "claudescience", "claude-science", "serve", "--detached")
	for _, b := range []byte(script) {
		if b >= 128 {
			t.Fatal("PowerShell 5 launcher contains ambiguous UTF-8 bytes")
		}
	}
	launcher := filepath.Join(dir, "launch.ps1")
	writeExecutableFixture(t, launcher, script)
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", launcher, "literal argument")
	if output, e := cmd.CombinedOutput(); e != nil {
		t.Fatal("synthetic launcher failed", e, string(output))
	}
	raw, e := os.ReadFile(record)
	if e != nil {
		t.Fatal(e)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatal("stop/serve ordering lost")
	}
	for i, line := range lines {
		var call struct {
			Value     string
			Arguments []string
		}
		if e := json.Unmarshal([]byte(line), &call); e != nil || call.Value != value {
			t.Fatal("literal environment value changed", e)
		}
		want := "stop"
		if i == 1 {
			want = "serve"
		}
		if len(call.Arguments) == 0 || call.Arguments[0] != want || i == 1 && (len(call.Arguments) != 3 || call.Arguments[2] != "literal argument") {
			t.Fatal("launcher arguments changed")
		}
	}
}
