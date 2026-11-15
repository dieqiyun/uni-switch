//go:build !windows

package gui

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// YouWhat on Discord: ~/.config wasn't the user's (sudo had made it), so
// adding a custom provider failed with "mkdir ~/.config/uni-switch: permission
// denied" and nothing said what to do. The failed save now names the
// folder (config_blocked), /api/state names it for the window's Repair,
// and Repair makes it writable again, after which the same save goes
// through, into the folder every reader looks in.
func TestBlockedConfigIsRepairedInOneClick(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes any folder")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	dotConfig := filepath.Join(home, ".config")
	if err := os.Mkdir(dotConfig, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dotConfig, 0o755) })
	h := Handler(nil, nil)
	call := func(method, path, body string) (int, string) {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec.Code, rec.Body.String()
	}
	const add = `{"id":"mine","name":"Mine","chat":"http://127.0.0.1:1/v1","key":"sk-a","models":["m1"],"new":true}`

	code, body := call("POST", "/api/provider/save", add)
	var failed struct{ Error, Code, Blocked string }
	json.Unmarshal([]byte(body), &failed)
	if code == 200 {
		t.Fatalf("save into a folder the user can't write went through: %s", body)
	}
	if failed.Code != "config_blocked" || failed.Blocked != "~/.config" {
		t.Fatalf("failed save = %d %s; want code config_blocked naming ~/.config, so the window can offer Repair", code, body)
	}
	_, body = call("GET", "/api/state", "")
	var s struct {
		ConfigBlocked string `json:"configBlocked"`
	}
	json.Unmarshal([]byte(body), &s)
	if s.ConfigBlocked != "~/.config" {
		t.Fatalf("state configBlocked = %q, want ~/.config", s.ConfigBlocked)
	}

	if code, body := call("POST", "/api/config/repair", "{}"); code != 204 {
		t.Fatalf("repair: %d %s", code, body)
	}
	if code, body := call("POST", "/api/provider/save", add); code != 200 {
		t.Fatalf("save after repair: %d %s", code, body)
	}
	if _, err := os.Stat(filepath.Join(dotConfig, "uni-switch", "providers.json")); err != nil {
		t.Fatalf("the provider isn't in ~/.config/uni-switch, where every reader looks: %v", err)
	}
	_, body = call("GET", "/api/state", "")
	s.ConfigBlocked = ""
	json.Unmarshal([]byte(body), &s)
	if s.ConfigBlocked != "" {
		t.Fatalf("state configBlocked = %q after repair, want empty", s.ConfigBlocked)
	}
}

// A folder another user owns (root, after sudo) can't be chmodded by the
// user: Repair asks the system's password prompt to give that folder alone
// back, not everything under it, while uni-switch's own folder is given back
// whole. uni-switch web's page, on another device, never shows the prompt.
func TestRepairOfAnotherUsersFolderAsksAnAdmin(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes any folder")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	dotConfig := filepath.Join(home, ".config")
	if err := os.Mkdir(dotConfig, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dotConfig, 0o755) })
	wasAdmin, wasElevate, wasMine := asAdmin, canElevate, mine
	t.Cleanup(func() { asAdmin, canElevate, mine = wasAdmin, wasElevate, wasMine })
	mine = func(string) bool { return false } // as if root's
	canElevate = func() bool { return true }
	var scripts []string
	asAdmin = func(why, script string) error {
		scripts = append(scripts, script)
		return os.Chmod(dotConfig, 0o755) // what chown + chmod u+rwx would leave
	}
	if err := repairConfig(true); err != nil {
		t.Fatal(err)
	}
	if len(scripts) != 1 || strings.Contains(scripts[0], "-R") || !strings.Contains(scripts[0], "'"+dotConfig+"'") {
		t.Fatalf("scripts = %q; want one chown of %s alone, not -R over other apps' folders", scripts, dotConfig)
	}
	if _, err := os.Stat(filepath.Join(dotConfig, "uni-switch")); err != nil {
		t.Fatalf("uni-switch's folder not made after repair: %v", err)
	}

	// uni-switch's own folder taken: given back whole
	own := filepath.Join(dotConfig, "uni-switch")
	os.Chmod(own, 0o555)
	t.Cleanup(func() { os.Chmod(own, 0o755) })
	scripts = nil
	asAdmin = func(why, script string) error { scripts = append(scripts, script); return os.Chmod(own, 0o755) }
	if err := repairConfig(true); err != nil {
		t.Fatal(err)
	}
	if len(scripts) != 1 || !strings.Contains(scripts[0], "chown -R ") {
		t.Fatalf("scripts = %q; want uni-switch's own folder given back whole", scripts)
	}

	// uni-switch web: no prompt, the command to run instead
	os.Chmod(own, 0o555)
	scripts = nil
	err := repairConfig(false)
	if err == nil || len(scripts) != 0 || !strings.Contains(err.Error(), "sudo chown") {
		t.Fatalf("web repair = %v, scripts %q; want no prompt and the command named", err, scripts)
	}
}
