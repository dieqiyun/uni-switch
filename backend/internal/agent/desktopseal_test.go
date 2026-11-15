package agent

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// a value Claude Desktop sealed with a keystore uni-switch can't open (another
// user's DPAPI on Windows, the Keychain's "Claude Safe Storage" on macOS)
var desktopForeignSeal = desktopSealed + base64.StdEncoding.EncodeToString([]byte("not this user's keystore"))

// Claude Desktop 4.4212 seals inferenceGatewayApiKey in uni-switch's profile at
// its next start. Desktop still runs on uni-switch: the check doesn't say the
// key was changed, and Sync and Reconnect leave nothing to undo.
func TestClaudeDesktopSealedKeyIsNotChanged(t *testing.T) {
	home, p := desktopSandbox(t)
	os.MkdirAll(p.dir, 0o755)
	a := claudeDesktop(home)
	if err := a.Field("provider").Set("uni-switch"); err != nil {
		t.Fatal(err)
	}
	if err := edit.SetJSON(p.prof, edit.KV{Path: "inferenceGatewayApiKey", Value: "  " + desktopForeignSeal}); err != nil {
		t.Fatal(err)
	}
	if c := a.Check(); c != "" {
		t.Errorf("check with the key sealed: %s", c)
	}
	before, _ := os.ReadFile(p.prof)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p.prof); string(b) != string(before) {
		t.Errorf("sync rewrote the profile:\n%s", b)
	}

	// a key in the clear that isn't uni-switch's is still a change
	edit.SetJSON(p.prof, edit.KV{Path: "inferenceGatewayApiKey", Value: "sk-someone-elses"})
	if c := a.Check(); !strings.Contains(c, "inferenceGatewayApiKey") || !strings.Contains(c, "was changed") {
		t.Errorf("check with another key: %q", c)
	}
	// and so is one gone
	edit.DelJSON(p.prof, "inferenceGatewayApiKey")
	if c := a.Check(); !strings.Contains(c, "is gone") {
		t.Errorf("check with no key: %q", c)
	}
	// and an address elsewhere, with the key sealed
	edit.SetJSON(p.prof, edit.KV{Path: "inferenceGatewayApiKey", Value: desktopForeignSeal},
		edit.KV{Path: "inferenceGatewayBaseUrl", Value: "http://elsewhere:1"})
	if c := a.Check(); !strings.Contains(c, "elsewhere") {
		t.Errorf("check with another address: %q", c)
	}
}

// An imported profile for uni-switch's gateway Desktop applies, its key
// sealed: on uni-switch, and Sync doesn't write a key over Desktop's.
func TestClaudeDesktopImportedSealedKeyStays(t *testing.T) {
	const imported = "3f6c2a8e-9b1d-4e57-a0c4-7d2e5f81b9a6"
	home, p := desktopSandbox(t)
	os.MkdirAll(p.dir, 0o755)
	a := claudeDesktop(home)
	if err := a.Field("provider").Set("uni-switch"); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(p.library, imported+".json")
	prof := `{"inferenceProvider":"gateway","inferenceGatewayBaseUrl":"` + gateway.URL() +
		`","inferenceGatewayApiKey":"` + desktopForeignSeal + `","inferenceGatewayAuthScheme":"bearer"}`
	desktopWrite(t, f, prof)
	desktopWrite(t, p.meta, `{"appliedId":"`+imported+`","entries":[{"id":"`+desktopProfileID+`","name":"uni-switch"},{"id":"`+imported+`","name":"uni-switch"}]}`)
	if c := a.Check(); c != "" {
		t.Errorf("check: %s", c)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(f); string(b) != prof {
		t.Errorf("sync wrote the imported profile:\n%s", b)
	}
}

func TestDesktopOpenNotSealed(t *testing.T) {
	for _, v := range []string{"", "uni-switch-claude-desktop", "__sealed__:v2:abc", "x__sealed__:v1:"} {
		if got, known := desktopOpen("inferenceGatewayApiKey", v); !known || got != v {
			t.Errorf("%q: %q %v", v, got, known)
		}
	}
	for _, v := range []string{desktopSealed, desktopSealed + "%%%", desktopForeignSeal} {
		if got, known := desktopOpen("inferenceGatewayApiKey", v); known || got != "" {
			t.Errorf("%q: %q %v, want unknown", v, got, known)
		}
	}
}
