package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// previewOf is the disconnect preview of a, run as the GUI runs it: this
// test binary as the dry run's process (TestMain).
func previewOf(t *testing.T, a *Agent) []FileChange {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tmp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tmp)
	changes, err := DisconnectPreview(a, exe)
	if err != nil {
		t.Fatal(err)
	}
	return changes
}

// previewSays is whether a line of changes holds s, as put in or taken out.
func previewSays(changes []FileChange, op, s string) bool {
	for _, c := range changes {
		for _, l := range c.Lines {
			if l.Op == op && (strings.Contains(l.Text, s) || strings.Contains(l.Was, s)) {
				return true
			}
		}
	}
	return false
}

// mmdSrc is MiniMax Design's agent as plugins#799 has it: a connect-only
// agent whose file is under %APPDATA%, one key holding uni-switch's entry.
const mmdSrc = `
export const agent = { id: "minimax-design", name: "MiniMax Design", config: "$APPDATA/@hilo/desktop/hub-config.json", format: "json" };
export function connect({ gateway, models }) {
  return { "config.customModels.user-custom-uni-switch": { protocol: "openai-compatible", providerName: "uni-switch", baseUrl: gateway.v1, apiKey: gateway.key, models: (models || []).map(m => ({ id: m.id })) } };
}
`

// A built-in keyed by its file's path is the same: Droid's stash holds the
// model the user started on, which its preview puts back.
func TestDisconnectPreviewOfDroidPutsItsModelBack(t *testing.T) {
	_, path := droidHome(t)
	os.WriteFile(path, []byte(droidUser), 0o644)
	a := droid(os.Getenv("HOME"))
	setUpInUniSwitch(a.ID)
	if err := a.Apply("model", "uni-switch/anth/claude-sonnet-5"); err != nil {
		t.Fatal(err)
	}
	changes := previewOf(t, a)
	if !previewSays(changes, "~", `"model": "claude-opus-4-7"`) {
		t.Fatalf("the preview doesn't put Droid's model back: %+v", changes)
	}
}

// A key names a path under home the way the system does: Windows' without
// case, and only where a separator follows home.
func TestMovedKey(t *testing.T) {
	home := filepath.Join(string(filepath.Separator)+"h", "me")
	to := filepath.Join(string(filepath.Separator)+"t", "copy")
	in := "plugin:x:" + filepath.Join(home, "a", "b.json")
	if got := movedKey(in, home, to); got != "plugin:x:"+filepath.Join(to, "a", "b.json") {
		t.Fatalf("moved %q", got)
	}
	if got := movedKey("droid:"+filepath.Join(home, ".factory", "settings.json")+":model", home, to); got != "droid:"+filepath.Join(to, ".factory", "settings.json")+":model" {
		t.Fatalf("moved %q", got)
	}
	for _, k := range []string{"codex.model", "plugin:x:" + home + "x" + string(filepath.Separator) + "f", "plugin:x:" + filepath.Join(string(filepath.Separator)+"else", "f")} {
		if got := movedKey(k, home, to); got != k {
			t.Fatalf("%q moved to %q", k, got)
		}
	}
	if runtime.GOOS == "windows" {
		if got := movedKey(`plugin:x:C:\users\ME\AppData\f`, `C:\Users\me`, `C:\t`); got != `plugin:x:C:\t\AppData\f` {
			t.Fatalf("moved %q", got)
		}
	}
}
