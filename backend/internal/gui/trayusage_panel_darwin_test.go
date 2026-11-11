//go:build darwin && cgo && !nogui

package gui

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// A separate process runs AppKit on its main thread. It uses an isolated
// config and a minimal page, without starting uni-switch's backend or status item.
func TestTrayCellClickReleasedPanel(t *testing.T) {
	out, err := runAppKit(t, 20*time.Second, []string{"UNI_SWITCH_TEST_TRAY_PANEL=1"})
	if err != nil || !strings.Contains(string(out), "tray quota: recreated and focused twice") {
		t.Fatalf("released panel: %v\n%s", err, out)
	}
}

func init() {
	if os.Getenv("UNI_SWITCH_TEST_TRAY_PANEL") != "1" {
		return
	}
	seen := make(chan string, 2)
	app := application.New(application.Options{
		Name: "uni-switch tray regression",
		Mac:  application.MacOptions{ActivationPolicy: application.ActivationPolicyProhibited},
		Assets: application.AssetOptions{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/seen" {
				seen <- r.URL.Query().Get("id")
				return
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<script>function panelQuotaFocus(id) { fetch('/seen?id=' + encodeURIComponent(id)); }</script><script type="module" src="/wails/runtime.js"></script>`)
		})},
	})
	h := &host{app: app, tray: &application.SystemTray{}}
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		application.InvokeAsync(func() { h.trayCellClick("codex|first@test") })
	})
	go func() {
		if id := <-seen; id != "codex|first@test" {
			panic("wrong initial target: " + id)
		}
		// Open is windowOpen, shown whether covered or not: Wails' IsVisible
		// is the occlusion, and under a window over the whole screen (a
		// remote-desktop server's) the panel just opened read as closed and
		// the second click showed it again (#1661).
		open := func(w *application.WebviewWindow) func() bool { return func() bool { return windowOpen(w) } }
		var first *application.WebviewWindow
		application.InvokeSync(func() {
			first = h.panel
			if !windowOpen(first) {
				panic("initial quota click did not open the panel")
			}
			h.trayCellClick("codex|first@test") // the second click closes it
		})
		// Native hide dispatches asynchronously, even from the main thread.
		deadline := time.Now().Add(2 * time.Second)
		for application.InvokeSyncWithResult(open(first)) {
			if time.Now().After(deadline) {
				panic("second quota click did not close the panel")
			}
			time.Sleep(10 * time.Millisecond)
		}
		application.InvokeSync(func() {
			// Same release state as lighten: the closed panel no longer exists.
			first.Close()
			h.panel = nil
			h.tray.AttachWindow(nil)
			h.trayCellClick("codex|second@test")
			if h.panel == nil || h.panel == first {
				panic("panel was not recreated")
			}
		})
		if id := <-seen; id != "codex|second@test" {
			panic("wrong recreated target: " + id)
		}
		fmt.Println("tray quota: recreated and focused twice")
		app.Quit()
	}()
	if err := app.Run(); err != nil {
		panic(err)
	}
	os.Exit(0)
}
