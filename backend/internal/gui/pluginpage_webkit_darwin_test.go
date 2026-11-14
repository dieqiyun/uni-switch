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

// A plugin's page in the real window: WKWebView serves it on wails://, not
// the http:// Playwright tests on, and its sandboxed frame has an origin of
// its own. Under pagePolicy, served as the app serves it (ownWindow names
// the scheme), its stylesheet and its scripts load there.
func TestPluginPageLoadsItsFilesInTheWindow(t *testing.T) {
	out, err := runAppKit(t, 30*time.Second, []string{"UNI_SWITCH_TEST_PLUGIN_PAGE=1"})
	if err != nil || !strings.Contains(string(out), "plugin page: styled script") {
		t.Fatalf("plugin page: %v\n%s", err, out)
	}
}

func init() {
	if os.Getenv("UNI_SWITCH_TEST_PLUGIN_PAGE") != "1" {
		return
	}
	const tok = "0123456789abcdef0123456789abcdef"
	seen := make(chan string, 1)
	app := application.New(application.Options{
		Name: "uni-switch plugin page regression",
		Mac:  application.MacOptions{ActivationPolicy: application.ActivationPolicyProhibited},
		Assets: application.AssetOptions{Handler: ownWindow(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/seen":
				select {
				case seen <- r.URL.Query().Get("what"):
				default:
				}
			case "/":
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, `<iframe sandbox="allow-scripts" src="`+pluginPagePrefix+tok+`/index.html"></iframe>
<script>addEventListener("message", (e) => fetch("/seen?what=" + encodeURIComponent(e.data)));
setTimeout(() => fetch("/seen?what=nothing"), 8000);</script>`)
			default:
				rel, ok := strings.CutPrefix(r.URL.Path, pluginPagePrefix+tok+"/")
				if !ok {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Security-Policy", pagePolicy(r, tok))
				switch rel {
				case "index.html":
					w.Header().Set("Content-Type", "text/html")
					fmt.Fprint(w, `<!doctype html><link rel="stylesheet" href="style.css"><h1>x</h1><script src="app.js"></script>`)
				case "style.css":
					w.Header().Set("Content-Type", "text/css")
					fmt.Fprint(w, `h1 { color: rgb(1, 2, 3); }`)
				case "app.js":
					w.Header().Set("Content-Type", "text/javascript")
					fmt.Fprint(w, `addEventListener("load", () => parent.postMessage(
  (getComputedStyle(document.querySelector("h1")).color === "rgb(1, 2, 3)" ? "styled" : "unstyled") + " script", "*"));`)
				default:
					http.NotFound(w, r)
				}
			}
		}))},
	})
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		app.Window.NewWithOptions(application.WebviewWindowOptions{Name: "main", Width: 300, Height: 200, URL: "/"})
	})
	go func() {
		fmt.Println("plugin page: " + <-seen)
		app.Quit()
	}()
	if err := app.Run(); err != nil {
		panic(err)
	}
	os.Exit(0)
}
