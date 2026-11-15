package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// notOnMac has WorkBuddy found as off a Mac, by its folder, for the test.
func notOnMac(t *testing.T) {
	was := workbuddyOnMac
	workbuddyOnMac = func() bool { return false }
	t.Cleanup(func() { workbuddyOnMac = was })
}

// @jiahao_jayden on X: an uninstalled WorkBuddy was on every row of the
// library. On a Mac WorkBuddy is there by its app: ~/.workbuddy stays
// behind when WorkBuddy.app is dragged to the Trash, and uni-switch writes
// there itself (models.json, mcp.json, skills/), so the folder, whatever
// it holds, is no sign of it. Each build is known by its own bundle id.
func TestWorkBuddyOnMacIsItsApp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("WORKBUDDY_CONFIG_DIR", "")
	apps, userApps := t.TempDir(), t.TempDir()
	was, wasMac := appFolders, workbuddyOnMac
	appFolders = func() []string { return []string{apps, userApps} }
	workbuddyOnMac = func() bool { return true }
	t.Cleanup(func() { appFolders, workbuddyOnMac = was, wasMac })
	plist := func(id string) string {
		return `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>` + id + `</string><key>CFBundleShortVersionString</key><string>5.6.2</string></dict></plist>`
	}
	// what an uninstall leaves, and what uni-switch wrote there itself
	for _, d := range []string{".workbuddy", ".workbuddy-ai"} {
		for _, f := range []string{"models.json", "mcp.json", "argv.json"} {
			p := filepath.Join(home, d, f)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("[]"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.MkdirAll(filepath.Join(home, d, "skills", "pdf"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cn, intl := workbuddy(home), workbuddyAI(home)
	if cn.Detected() || intl.Detected() {
		t.Fatalf("no app, only the folders: WorkBuddy %v, WorkBuddy AI %v", cn.Detected(), intl.Detected())
	}

	// the international build's app, in /Applications: it alone is there,
	// with no folder of its own yet too
	os.RemoveAll(filepath.Join(home, ".workbuddy-ai"))
	installApp(t, apps, "WorkBuddy AI.app", plist("com.workbuddy.workbuddy-ai"))
	if cn.Detected() || !intl.Detected() {
		t.Fatalf("WorkBuddy AI.app: WorkBuddy %v, WorkBuddy AI %v", cn.Detected(), intl.Detected())
	}

	// another app under WorkBuddy's name is not WorkBuddy
	installApp(t, userApps, "WorkBuddy.app", plist("com.example.other"))
	if cn.Detected() {
		t.Fatal("another bundle id taken for WorkBuddy")
	}
	// the China build's, in ~/Applications
	installApp(t, userApps, "WorkBuddy.app", plist("com.tencent.workbuddy.mac"))
	if !cn.Detected() {
		t.Fatal("WorkBuddy.app installed: not detected")
	}
}
