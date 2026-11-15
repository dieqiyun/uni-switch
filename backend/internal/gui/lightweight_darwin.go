//go:build !nogui

package gui

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

static WKWebView *findWebView(NSView *v) {
	if ([v isKindOfClass:[WKWebView class]]) return (WKWebView *)v;
	for (NSView *s in v.subviews) {
		WKWebView *w = findWebView(s);
		if (w) return w;
	}
	return nil;
}

// dropWebView takes the window's webview out of it, on the main thread: a
// Wails window closed keeps its WKWebView, and its web content process,
// alive (seen: both pages' processes, 65 MB each, still there minutes
// after). The window's own pointer to it is cleared first (Wails' dealloc
// reads it), then what holds the webview: its delegates, its script
// handlers, its superview; it is freed and its process ends.
static void dropWebView(void *win) {
	NSWindow *w = (NSWindow *)win;
	WKWebView *web = findWebView(w.contentView);
	if (!web) return;
	if ([w respondsToSelector:@selector(setWebView:)]) [w performSelector:@selector(setWebView:) withObject:nil];
	[web stopLoading];
	web.navigationDelegate = nil;
	web.UIDelegate = nil;
	[web.configuration.userContentController removeAllScriptMessageHandlers];
	[web removeFromSuperview];
}
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

// dropWebView ends w's webview before it is closed; on the main thread.
func dropWebView(w *application.WebviewWindow) {
	if p := w.NativeWindow(); p != nil {
		C.dropWebView(p)
	}
}
