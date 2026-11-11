package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestVSCodeDetected(t *testing.T) {
	for _, tt := range []struct {
		name, layout, product, config, link string
		want                                bool
	}{
		{name: "missing"},
		{name: "unknown wrapper", layout: "wrapper", want: true},
		{name: "wrapper beside unrelated product", layout: "wrapper", product: `{"applicationName":"cursor"}`, want: true},
		{name: "mac code", layout: "mac", product: `{"applicationName":"code","nameShort":"Visual Studio Code"}`, want: true},
		{name: "mac cursor", layout: "mac", product: `{"applicationName":"cursor","nameShort":"Cursor"}`},
		{name: "linux code", layout: "linux", product: `{"applicationName":"code"}`, want: true},
		{name: "linux cursor", layout: "linux", product: `{"applicationName":"cursor"}`},
		{name: "windows code", layout: "windows", product: `{"applicationName":"code"}`, want: true},
		{name: "windows cursor", layout: "windows", product: `{"applicationName":"cursor"}`},
		{name: "cursor name", layout: "mac", product: `{"nameShort":"Cursor"}`},
		// Qoder IDE ships bin/code beside bin/qoder (ggbr on Discord); its
		// product.json as the 2026-10-29 macOS build has it
		{name: "mac qoder", layout: "mac", product: `{"nameShort":"Qoder","nameLong":"Qoder","applicationName":"qoder","dataFolderName":".qoder","darwinBundleIdentifier":"com.qoder.ide","win32DirName":"Qoder IDE"}`},
		{name: "linux qoder", layout: "linux", product: `{"applicationName":"qoder","nameShort":"Qoder"}`},
		{name: "windows qoder", layout: "windows", product: `{"applicationName":"qoder","nameShort":"Qoder"}`},
		{name: "qoder symlink", layout: "mac", product: `{"applicationName":"qoder"}`, link: "single"},
		{name: "mac windsurf", layout: "mac", product: `{"applicationName":"windsurf","nameShort":"Windsurf"}`},
		{name: "windows trae", layout: "windows", product: `{"applicationName":"trae","nameShort":"Trae"}`},
		{name: "linux kiro", layout: "linux", product: `{"applicationName":"kiro","nameShort":"Kiro"}`},
		{name: "windows codebuddy", layout: "windows", product: `{"applicationName":"codebuddy","nameShort":"CodeBuddy"}`},
		{name: "mac antigravity", layout: "mac", product: `{"applicationName":"antigravity","nameShort":"Antigravity"}`},
		{name: "insiders product", layout: "mac", product: `{"applicationName":"code-insiders","nameShort":"Code - Insiders"}`},
		{name: "qoder name", layout: "mac", product: `{"nameShort":"Qoder"}`},
		{name: "code name", layout: "mac", product: `{"nameShort":"Code"}`, want: true},
		{name: "visual studio code name", layout: "windows", product: `{"nameShort":"Visual Studio Code"}`, want: true},
		{name: "unbranded product", layout: "mac", product: `{}`, want: true},
		{name: "invalid product", layout: "mac", product: `{"applicationName":"cursor",`, want: true},
		{name: "missing product", layout: "mac", want: true},
		{name: "unreadable product", layout: "mac", product: "directory", want: true},
		{name: "code symlink", layout: "mac", product: `{"applicationName":"code"}`, link: "single", want: true},
		{name: "cursor symlink", layout: "mac", product: `{"applicationName":"cursor"}`, link: "single"},
		{name: "code chained symlink", layout: "mac", product: `{"applicationName":"code"}`, link: "chain", want: true},
		{name: "cursor chained symlink", layout: "mac", product: `{"applicationName":"cursor"}`, link: "chain"},
		{name: "broken symlink", layout: "mac", link: "broken"},
		{name: "configured cursor", layout: "mac", product: `{"applicationName":"cursor"}`, config: "settings", want: true},
		{name: "config directory and cursor", layout: "mac", product: `{"applicationName":"cursor"}`, config: "directory", want: true},
		{name: "settings without binary", config: "settings", want: true},
		{name: "directory without binary", config: "directory", want: true},
		{name: "taken config directory", layout: "wrapper", config: "file"},
		{name: "taken config parent", layout: "wrapper", config: "parent file"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "config", "Code", "User")
			switch tt.config {
			case "settings":
				writeFile(t, filepath.Join(dir, "settings.json"), "{}")
			case "directory":
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			case "file":
				writeFile(t, dir, "taken")
			case "parent file":
				writeFile(t, filepath.Dir(dir), "taken")
			}
			binDir := filepath.Join(root, "empty path")
			if err := os.MkdirAll(binDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if tt.layout != "" {
				app := filepath.Join(root, "Editor With Spaces")
				var product string
				switch tt.layout {
				case "mac":
					app = filepath.Join(app+".app", "Contents", "Resources", "app")
					product = filepath.Join(app, "product.json")
				case "linux", "windows":
					product = filepath.Join(app, "resources", "app", "product.json")
				case "wrapper":
					product = filepath.Join(app, "product.json")
				}
				binDir = filepath.Join(app, "bin")
				if tt.layout == "wrapper" {
					binDir = filepath.Join(app, "tools")
				}
				name := "code"
				if runtime.GOOS == "windows" {
					name += ".cmd"
					t.Setenv("PATHEXT", ".CMD")
				}
				bin := filepath.Join(binDir, name)
				writeFile(t, bin, "not a runnable editor\n")
				if err := os.Chmod(bin, 0o755); err != nil {
					t.Fatal(err)
				}
				if tt.product == "directory" {
					if err := os.MkdirAll(product, 0o755); err != nil {
						t.Fatal(err)
					}
				} else if tt.product != "" {
					writeFile(t, product, tt.product)
				}
				if tt.link != "" {
					binDir = filepath.Join(root, "path with spaces")
					if err := os.MkdirAll(binDir, 0o755); err != nil {
						t.Fatal(err)
					}
					target := bin
					if tt.link == "chain" {
						target = filepath.Join(binDir, "intermediate")
						vscodeTestSymlink(t, bin, target)
						// The outer link is relative to its own directory.
						target = "intermediate"
					} else if tt.link == "broken" {
						target = filepath.Join(root, "missing")
					}
					vscodeTestSymlink(t, target, filepath.Join(binDir, name))
				}
			}
			t.Setenv("PATH", binDir)
			if got := vscodeAt(dir).Detected(); got != tt.want {
				t.Errorf("Detected() = %v, want %v", got, tt.want)
			}
			if tt.config == "" {
				if _, err := os.Stat(filepath.Join(root, "config")); !os.IsNotExist(err) {
					t.Fatalf("detection created config: %v", err)
				}
			}
		})
	}
}

func vscodeTestSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink unavailable: %v", err)
		}
		t.Fatal(err)
	}
}

// Each of VS Code's builds is told by its own command's name in its
// product.json, and a fork's launcher of the same name is not it.
func TestVSCodeBuildLauncher(t *testing.T) {
	for _, tt := range []struct {
		app, product string
		want         bool
	}{
		{"code", `{"applicationName":"code","nameShort":"Code"}`, true},
		{"code-insiders", `{"applicationName":"code-insiders","nameShort":"Code - Insiders"}`, true},
		{"codium", `{"applicationName":"codium","nameShort":"VSCodium"}`, true},
		{"codium", `{"applicationName":"code"}`, false},
		{"code-insiders", `{"applicationName":"qoder"}`, false},
		{"codium", `{"applicationName":"cursor"}`, false},
		{"codium", `{"nameShort":"VSCodium"}`, true},
		{"code", `{"nameShort":"Code - OSS"}`, true},
		{"code", `{"nameShort":"Windsurf"}`, false},
	} {
		root := t.TempDir()
		app := filepath.Join(root, "Editor.app", "Contents", "Resources", "app")
		writeFile(t, filepath.Join(app, "product.json"), tt.product)
		bin := filepath.Join(app, "bin", tt.app)
		writeFile(t, bin, "#!/bin/sh\n")
		if got := vscodeCodeBinary(bin, tt.app); got != tt.want {
			t.Errorf("vscodeCodeBinary(%s, %s) = %v, want %v", tt.app, tt.product, got, tt.want)
		}
	}
}
