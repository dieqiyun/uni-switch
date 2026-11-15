//go:build !nogui

package gui

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
#import <Cocoa/Cocoa.h>

// The drag's source: a copy, wherever it is dropped. An object of its
// own, not the webview, which would take the session for one of its
// page's drags when it ends.
@interface UniSwitchFileDrag : NSObject <NSDraggingSource>
@end
@implementation UniSwitchFileDrag
- (NSDragOperation)draggingSession:(NSDraggingSession *)session sourceOperationMaskForDraggingContext:(NSDraggingContext)context {
	return NSDragOperationCopy;
}
@end

static UniSwitchFileDrag *fileDragSource;

// dragFile starts the system's drag of the file at path from under the
// mouse, as Finder drags it, while the button the page saw go down is
// still held. Says 0 when it isn't, or there is no window to drag from.
static int dragFile(const char *path) {
	NSString *p = [NSString stringWithUTF8String:path];
	__block int ok = 0;
	dispatch_sync(dispatch_get_main_queue(), ^{
		if (([NSEvent pressedMouseButtons] & 1) == 0) return;
		NSWindow *win = [NSApp keyWindow];
		NSView *view = win.contentView;
		if (view == nil) return;
		if (fileDragSource == nil) fileDragSource = [UniSwitchFileDrag new];
		NSPoint inWin = [win mouseLocationOutsideOfEventStream];
		NSPoint at = [view convertPoint:inWin fromView:nil];
		NSImage *icon = [[NSWorkspace sharedWorkspace] iconForFile:p];
		icon.size = NSMakeSize(48, 48);
		NSDraggingItem *item = [[NSDraggingItem alloc] initWithPasteboardWriter:[NSURL fileURLWithPath:p isDirectory:YES]];
		[item setDraggingFrame:NSMakeRect(at.x - 24, at.y - 24, 48, 48) contents:icon];
		// the page's own mouse-down went by in WebKit; the session starts from
		// a drag at where the mouse is now
		NSEvent *ev = [NSEvent mouseEventWithType:NSEventTypeLeftMouseDragged location:inWin modifierFlags:0
			timestamp:[[NSProcessInfo processInfo] systemUptime] windowNumber:win.windowNumber context:nil
			eventNumber:0 clickCount:1 pressure:1];
		NSDraggingSession *s = [view beginDraggingSessionWithItems:@[item] event:ev source:fileDragSource];
		s.animatesToStartingPositionsOnCancelOrFail = YES;
		ok = 1;
	});
	return ok;
}
*/
import "C"

import "unsafe"

// canDragFile: the page may hand a file to the system's drag here.
const canDragFile = true

// dragFile drags the file or folder at path out of uni-switch's window, onto
// whatever takes a file from Finder (a browser's extensions page).
func dragFile(path string) bool {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	return C.dragFile(p) != 0
}
