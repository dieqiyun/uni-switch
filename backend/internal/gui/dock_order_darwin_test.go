//go:build darwin && cgo && !nogui

package gui

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// uni-switch out of the Dock, activated by its quick panel, opens its window
// and joins the Dock (Settings › Show in Dock › while the window is open): it leads
// Command-Tab's order, so one Command-Tab away comes back to it (#1381). It
// joined while active and stayed last, behind every app, though in front.
// The order is the Mac's front-to-back list, which `lsappinfo` prints.
func TestDockJoinLeadsCommandTab(t *testing.T) {
	out, err := runAppKit(t, 30*time.Second, []string{"UNI_SWITCH_TEST_DOCK_ORDER=1"})
	if strings.Contains(string(out), "dock order: skip") {
		t.Skipf("this session can't bring an app to the front:\n%s", out)
	}
	if err != nil || !strings.Contains(string(out), "dock order: ok") {
		t.Fatalf("dock order: %v\n%s", err, out)
	}
}

// lsFirst says whether pid's app leads the front-to-back order, and whether
// it is the front app at all.
func lsFirst(pid int) (first, front bool, out string) {
	me, err := exec.Command("lsappinfo", "find", "pid="+strconv.Itoa(pid)).Output()
	asn := strings.TrimSpace(string(me))
	if i := strings.Index(asn, "-\""); i > 0 {
		asn = asn[:i]
	}
	if err != nil || asn == "" {
		return false, false, fmt.Sprintf("no ASN for pid %d: %v", pid, err)
	}
	f, _ := exec.Command("lsappinfo", "front").Output()
	list, _ := exec.Command("lsappinfo", "visibleProcessList").Output()
	front = strings.HasPrefix(strings.TrimSpace(string(f)), asn)
	first = strings.HasPrefix(strings.TrimSpace(string(list)), asn+"-")
	return first, front, fmt.Sprintf("me %s, front %s, order %.300s", asn, strings.TrimSpace(string(f)), list)
}

func init() {
	if os.Getenv("UNI_SWITCH_TEST_DOCK_ORDER") != "1" {
		return
	}
	app := application.New(application.Options{
		Name: "uni-switch dock order regression",
		Mac:  application.MacOptions{ActivationPolicy: application.ActivationPolicyAccessory},
		Assets: application.AssetOptions{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<p>uni-switch</p>`)
		})},
	})
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			defer app.Quit()
			// The quick panel's Focus activates uni-switch out of the Dock, and
			// its Open (openMain) joins the Dock, then shows the window. The
			// Mac makes uni-switch the front app at once, but NSApp says it is
			// active only once the main thread has taken the activation in,
			// which on a busy Mac can be after the join (#1651). Both are
			// done in one turn of the main thread here, so the join always
			// comes before NSApp knows.
			skip := application.InvokeSyncWithResult(func() bool {
				activateApp()
				deadline := time.Now().Add(5 * time.Second)
				for _, front, _ := lsFirst(os.Getpid()); !front; _, front, _ = lsFirst(os.Getpid()) {
					if time.Now().After(deadline) {
						return true
					}
					time.Sleep(100 * time.Millisecond)
				}
				setDock(true, true)
				w := app.Window.NewWithOptions(application.WebviewWindowOptions{Name: "main", Width: 300, Height: 200})
				w.Show()
				w.Focus()
				return false
			})
			if skip {
				fmt.Println("dock order: skip")
				return
			}
			var first bool
			var got string
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
				if first, _, got = lsFirst(os.Getpid()); first {
					break
				}
			}
			if !first {
				fmt.Printf("dock order: uni-switch isn't first in Command-Tab's order: %s\n", got)
				return
			}
			fmt.Println("dock order: ok")
		}()
	})
	if err := app.Run(); err != nil {
		panic(err)
	}
	os.Exit(0)
}
