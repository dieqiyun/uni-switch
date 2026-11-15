package agent

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// Claude Desktop 4.4212 (10-29) keeps the secrets of its configLibrary
// profiles encrypted at rest. At every start it sweeps configLibrary and
// writes each secret it finds in the clear (inferenceGatewayApiKey among
// them) back as
//
//	"__sealed__:v1:" + base64(protect("anthropic.claude.crossProfileSecret.v1:" +
//	  {"configLibrary":{"key":"inferenceGatewayApiKey","value":"<the key>"}}))
//
// (its custom-3p:keys sweep; protect is the user's DPAPI on Windows, its
// native userDpapiProtect, and Electron's safeStorage, the Keychain, on
// macOS), and opens it again when it reads the profile.
// It sends the gateway the key as it was, so the value uni-switch wrote no
// longer reads as uni-switch's key in the file while Desktop still runs on it:
// uni-switch said the key "was changed", Reconnect wrote it in the clear and
// Desktop sealed it again at its next start (a Windows user on Discord).
const (
	desktopSealed     = "__sealed__:v1:"
	desktopSealedWith = "anthropic.claude.crossProfileSecret.v1:"
)

// errDesktopSealed: Desktop sealed a value with a keystore uni-switch can't
// open here.
var errDesktopSealed = errors.New("sealed by Claude Desktop")

// desktopOpen is the value v Desktop keeps at key k of a configLibrary
// profile, as Desktop reads it: v itself, or what a sealed v opens to.
// known is false for a sealed value uni-switch can't open (another system's
// keystore, another user's, or not one of Desktop's): what it holds is
// unknown, which is not the same as changed.
func desktopOpen(k, v string) (open string, known bool) {
	t := strings.TrimLeft(v, " \t\r\n")
	if !strings.HasPrefix(t, desktopSealed) {
		return v, true
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(t[len(desktopSealed):]))
	if err != nil {
		return "", false
	}
	plain, err := desktopUnprotect(b)
	if err != nil {
		return "", false
	}
	s, ok := strings.CutPrefix(string(plain), desktopSealedWith)
	if !ok {
		return "", false
	}
	var m struct {
		ConfigLibrary struct {
			Key   string  `json:"key"`
			Value *string `json:"value"`
		} `json:"configLibrary"`
	}
	// Desktop opens a value only for the key it was sealed for
	if json.Unmarshal([]byte(s), &m) != nil || m.ConfigLibrary.Key != k || m.ConfigLibrary.Value == nil {
		return "", false
	}
	return *m.ConfigLibrary.Value, true
}
