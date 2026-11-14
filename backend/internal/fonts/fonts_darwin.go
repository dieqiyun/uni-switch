//go:build darwin && cgo && !nogui

package fonts

/*
#cgo LDFLAGS: -framework AppKit -framework CoreText
#include <stdlib.h>
char *uniSwitchInstalledFonts(void);
*/
import "C"

import (
	"encoding/json"
	"errors"
	"unsafe"
)

const Available = true

func installed() ([]Face, error) {
	raw := C.uniSwitchInstalledFonts()
	if raw == nil {
		return nil, errors.New("CoreText could not list installed fonts")
	}
	defer C.free(unsafe.Pointer(raw))
	var faces []Face
	if err := json.Unmarshal([]byte(C.GoString(raw)), &faces); err != nil {
		return nil, err
	}
	return faces, nil
}
