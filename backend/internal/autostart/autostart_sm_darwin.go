//go:build darwin && cgo

package autostart

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework ServiceManagement
#include <stdlib.h>
#import <Foundation/Foundation.h>
#import <ServiceManagement/ServiceManagement.h>

// SMAppService is macOS 13's; before it there is nothing to ask (-1)
static long uniSwitchLegacyStatus(const char *path) {
	if (@available(macOS 13.0, *)) {
		@autoreleasepool {
			NSURL *u = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
			return (long)[SMAppService statusForLegacyURL:u];
		}
	}
	return -1;
}
*/
import "C"

import "unsafe"

func init() {
	legacyStatus = func(path string) int {
		p := C.CString(path)
		defer C.free(unsafe.Pointer(p))
		return int(C.uniSwitchLegacyStatus(p))
	}
}
