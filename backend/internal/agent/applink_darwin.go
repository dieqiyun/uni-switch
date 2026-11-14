//go:build darwin && cgo

package agent

/*
#cgo CFLAGS: -Wno-deprecated-declarations
#cgo LDFLAGS: -framework CoreServices -framework CoreFoundation
#include <CoreServices/CoreServices.h>

// uni_switch_link_handler asks Launch Services which app opens link, without
// opening it: 1 found, 0 none (kLSApplicationNotFoundErr), -1 unknown.
static int uni_switch_link_handler(const char *link) {
	CFURLRef url = CFURLCreateWithBytes(NULL, (const UInt8 *)link, strlen(link), kCFStringEncodingUTF8, NULL);
	if (url == NULL) return -1;
	CFErrorRef err = NULL;
	CFURLRef app = LSCopyDefaultApplicationURLForURL(url, kLSRolesAll, &err);
	CFRelease(url);
	if (app != NULL) {
		CFRelease(app);
		return 1;
	}
	int r = -1;
	if (err != NULL) {
		if (CFErrorGetCode(err) == kLSApplicationNotFoundErr) r = 0;
		CFRelease(err);
	}
	return r;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

// systemLinkHandler asks Launch Services whether any app opens scheme's
// links. The app's path isn't judged: Launch Services forgets an app
// that is gone.
func systemLinkHandler(scheme string) (string, bool, error) {
	link := C.CString(scheme + "://x")
	defer C.free(unsafe.Pointer(link))
	switch C.uni_switch_link_handler(link) {
	case 1:
		return "", true, nil
	case 0:
		return "", false, nil
	}
	return "", false, errors.New("launch services: unknown")
}
