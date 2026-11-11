//go:build !nogui

package gui

import (
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// mainMacWindow: the main window's Mac options. A window belongs to the
// Space it was last shown on, and showing it again takes the user there; one
// first shown while another app was full screen (a restart to update, with
// the user in that app) belonged to that app's Space from then on, and every
// Open went back to it. It moves to the Space the user is on whenever it is
// shown instead, and can still be made full screen itself. Once shown it
// stays on its Space (showHere), so a click on the Dock icon goes to it there
// (reopenMain).
func mainMacWindow() application.MacWindow {
	return application.MacWindow{
		// no InvisibleTitleBarHeight: that strip drags from anywhere in
		// it, tabs included; the header marks what drags instead. On
		// macOS 26 and later its toolbar is taken off once it has placed
		// the traffic lights (plainTitlebar)
		TitleBar: application.MacTitleBarHiddenInset,
		CollectionBehavior: application.MacWindowCollectionBehaviorMoveToActiveSpace |
			application.MacWindowCollectionBehaviorFullScreenPrimary,
	}
}

// frameOnlyResizeJS leaves resizing the main window on Windows to its own
// frame. Wails' page script arms a resize along every edge inside the page
// (SM_CYSIZEFRAME pixels, its drag.ts) whenever the window is resizable on
// Windows, frame or no frame; meant for a frameless window, in the framed
// main window it made the top of the page, under the title bar, drag the
// window's height too (shangyuan0818 on Discord). Its switch is turned off,
// and kept off: Wails turns it on again once its script is ready, which may
// come before this or after. Linux and the Mac never arm it on a framed
// window.
const frameOnlyResizeJS = `(()=>{const w=window._wails;if(!w||typeof w.setResizable!=="function")return;` +
	`if(!w.setResizable.frameOnly){const f=w.setResizable;w.setResizable=()=>f(false);w.setResizable.frameOnly=true}` +
	`w.setResizable(false)})();`

type closeAction int

const (
	closeHide            closeAction = iota // hide the window now
	closeLeaveFullscreen                    // leave full screen, then hide it
)

// closeStep: what closing the main window does. A full-screen window on the
// Mac hidden as it is leaves its Space behind, black, with nothing in it; it
// leaves full screen first and is hidden once it has.
func closeStep(goos string, fullscreen bool) closeAction {
	if goos == "darwin" && fullscreen {
		return closeLeaveFullscreen
	}
	return closeHide
}

// mainClosedJS tells the page its window was closed: an account shown
// while Hide accounts is on (its eye) is hidden again, as each is shown for
// the window's session only (yogurt36 on Discord). The page stays loaded
// while the window is hidden, so it can't tell by itself.
const mainClosedJS = "window.hideAccounts?.closed?.()"

// hideMain hides the main window, and takes uni-switch out of the Dock if it is
// there only while the window is shown.
func (h *host) hideMain() {
	application.InvokeSync(func() {
		if h.main != nil {
			h.main.ExecJS(mainClosedJS)
			hideWebview(h.main)
		}
	})
	h.dock(settings.Load(), false)
}

// leaveFullscreenThenHide takes the closed full-screen window out of full
// screen; WindowDidExitFullScreen hides it. Should that never come (the
// system refused to leave), it is hidden after a while all the same.
func (h *host) leaveFullscreenThenHide() {
	h.closing.Store(true)
	application.InvokeSync(func() {
		if h.main != nil {
			h.main.UnFullscreen()
		}
	})
	time.AfterFunc(3*time.Second, func() {
		if h.closing.Swap(false) {
			application.InvokeAsync(h.hideMain)
		}
	})
}
