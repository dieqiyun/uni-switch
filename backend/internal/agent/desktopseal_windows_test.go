package agent

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

// desktopSealHere seals v at key k as Claude Desktop does on Windows: the
// user's DPAPI, with its entropy, over its prefix and
// {"configLibrary":{key,value}}.
func desktopSealHere(t *testing.T, k, v string) string {
	t.Helper()
	j, _ := json.Marshal(map[string]any{"configLibrary": map[string]string{"key": k, "value": v}})
	b := []byte(desktopSealedWith + string(j))
	in := windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
	e := []byte(desktopEntropy)
	entropy := windows.DataBlob{Size: uint32(len(e)), Data: &e[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, &entropy, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		t.Fatal(err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return desktopSealed + base64.StdEncoding.EncodeToString(unsafe.Slice(out.Data, out.Size))
}

func TestDesktopOpensWhatDesktopSealed(t *testing.T) {
	k := "inferenceGatewayApiKey"
	if got, known := desktopOpen(k, desktopSealHere(t, k, "uni-switch-x-claude-desktop")); !known || got != "uni-switch-x-claude-desktop" {
		t.Errorf("opened %q %v", got, known)
	}
	// sealed for another key: Desktop wouldn't open it there either
	if got, known := desktopOpen(k, desktopSealHere(t, "anthropicApiKey", "sk")); known {
		t.Errorf("another key's seal opened to %q", got)
	}
}

// On Windows uni-switch opens the key Desktop sealed: uni-switch's own is on
// uni-switch, another one sealed is a change.
func TestClaudeDesktopSealedKeyOpened(t *testing.T) {
	home, p := desktopSandbox(t)
	os.MkdirAll(p.dir, 0o755)
	a := claudeDesktop(home)
	if err := a.Field("provider").Set("uni-switch"); err != nil {
		t.Fatal(err)
	}
	k := "inferenceGatewayApiKey"
	edit.SetJSON(p.prof, edit.KV{Path: k, Value: desktopSealHere(t, k, agentKey("claude-desktop"))})
	if c := a.Check(); c != "" {
		t.Errorf("check with uni-switch's key sealed: %s", c)
	}
	edit.SetJSON(p.prof, edit.KV{Path: k, Value: desktopSealHere(t, k, "sk-someone-elses")})
	if c := a.Check(); !strings.Contains(c, "was changed") {
		t.Errorf("check with another key sealed: %q", c)
	}
}
