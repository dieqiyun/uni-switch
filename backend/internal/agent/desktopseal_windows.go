package agent

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// desktopEntropy is the entropy Claude Desktop's native userDpapiProtect
// (claude-native's user_dpapi) gives CryptProtectData: its secret's
// purpose, the one string beside CryptProtectData in the binary (4.4212).
const desktopEntropy = "anthropic.claude.crossProfileSecret.v1"

// desktopUnprotect opens what Claude Desktop sealed with the user's DPAPI
// (userDpapiProtect), never prompting.
func desktopUnprotect(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, errDesktopSealed
	}
	in := windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
	e := []byte(desktopEntropy)
	entropy := windows.DataBlob{Size: uint32(len(e)), Data: &e[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, &entropy, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}
