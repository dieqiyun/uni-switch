package omarchy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// Hyprland is whether uni-switch runs under Hyprland, Omarchy's compositor or
// anyone else's. A Wayland window can't place itself there, so a tray panel
// that asks to go under its icon opens wherever Hyprland floats it (the
// middle of the screen), framed and dimmed like any other window.
func Hyprland() bool {
	return runtime.GOOS == "linux" && os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != ""
}

// PanelGap is how far a drop-down panel keeps from the bar and the screen's
// edge, as Omarchy's own popups do.
const PanelGap = 6

type hyprMonitor struct {
	Name          string
	X, Y          int
	Width, Height int
	Scale         float64
	Transform     int
	Reserved      [4]int // left, top, right, bottom
}

// size is the monitor's size in layout coordinates, the ones windows and
// the cursor are placed in.
func (m hyprMonitor) size() (w, h int) {
	s := m.Scale
	if s <= 0 {
		s = 1
	}
	w, h = int(float64(m.Width)/s+.5), int(float64(m.Height)/s+.5)
	if m.Transform%2 == 1 { // turned 90° or 270°
		w, h = h, w
	}
	return w, h
}

// PanelAt is where a drop-down panel width wide goes: under the bar, its
// middle at the cursor (which has just clicked the icon it drops from),
// kept PanelGap inside the monitor the cursor is on. mon is that monitor's
// name, and x, y are from its top left corner, as Hyprland's move rule
// takes them: it adds the monitor's own place to them, so a monitor not at
// 0,0 was counted twice and the panel opened off the screen (#1785). maxH
// is the room it has down to the bottom of that monitor's work area.
func PanelAt(width int) (mon string, x, y, maxH int, ok bool) {
	var cur struct{ X, Y int }
	var mons []hyprMonitor
	if hyprJSON("cursorpos", &cur) != nil || hyprJSON("monitors", &mons) != nil {
		return "", 0, 0, 0, false
	}
	return panelAt(width, cur.X, cur.Y, mons)
}

// panelAt takes the cursor in layout coordinates, across every monitor (as
// hyprctl cursorpos gives it), and answers from the corner of the one it is
// on.
func panelAt(width, cx, cy int, mons []hyprMonitor) (mon string, x, y, maxH int, ok bool) {
	for _, m := range mons {
		w, h := m.size()
		if cx < m.X || cx >= m.X+w || cy < m.Y || cy >= m.Y+h {
			continue
		}
		left, right := m.Reserved[0]+PanelGap, w-m.Reserved[2]-PanelGap
		x = max(left, min(right-width, cx-m.X-width/2))
		y = m.Reserved[1] + PanelGap
		return m.Name, x, y, h - m.Reserved[3] - PanelGap - y, true
	}
	return "", 0, 0, 0, false
}

// PlacePanel gives the window titled title a rule of its own (replacing
// the last one, by name): floating, pinned to every workspace, on monitor
// mon at x, y from its corner, with no border and fully opaque — the page
// draws its own frame, in the border's colour. It is asked each time the
// panel opens, as where it goes follows the click, and a config reload
// drops rules made this way.
func PlacePanel(title, mon string, x, y int) error {
	return hyprctl("eval", panelRule(title, mon, x, y))
}

// panelRule is PlacePanel's rule. The monitor is named so that the move
// counts from the corner of the one clicked, not from whichever Hyprland
// would have opened the panel on.
func panelRule(title, mon string, x, y int) string {
	on := ""
	if mon != "" {
		on = "monitor = " + strconv.Quote(mon) + ", "
	}
	return fmt.Sprintf(`hl.window_rule({ name = "uni-switch-panel", match = { title = %s }, tag = "-default-opacity", float = true, pin = true, border_size = 0, no_shadow = true, opacity = "1 1", no_anim = true, %smove = { %d, %d } })`,
		strconv.Quote("^"+regexpQuote(title)+"$"), on, x, y)
}

// Clicks are how the panel closes on a click outside it, as Omarchy's own
// drop-downs do. Losing focus can't say so here: Omarchy has focus follow
// the mouse, so the pointer merely passing over another window takes it,
// and Hyprland tells a program nothing of clicks on others. While the panel
// is open, a left-click bind that lets the click through (non_consuming)
// announces each one on Hyprland's event socket, and WatchClicks hears it.
const clickEvent = "uni-switch-click"

// ReportClicks has Hyprland announce every left click, until StopClicks.
func ReportClicks() error {
	return hyprctl("eval", `if uni_switch_click then uni_switch_click:remove() end uni_switch_click = hl.bind("mouse:272", function() hl.dispatch(hl.dsp.event("`+clickEvent+`")) end, { non_consuming = true })`)
}

// StopClicks takes the bind away again.
func StopClicks() error {
	return hyprctl("eval", `if uni_switch_click then uni_switch_click:remove() uni_switch_click = nil end`)
}

// WatchClicks calls click on each click ReportClicks announces, for as long
// as uni-switch runs, reconnecting when Hyprland restarts.
func WatchClicks(click func()) {
	sock := filepath.Join(appdir.Getenv("XDG_RUNTIME_DIR"), "hypr", os.Getenv("HYPRLAND_INSTANCE_SIGNATURE"), ".socket2.sock")
	for {
		if c, err := net.Dial("unix", sock); err == nil {
			sc := bufio.NewScanner(c)
			for sc.Scan() {
				if sc.Text() == "custom>>"+clickEvent {
					click()
				}
			}
			c.Close()
		}
		time.Sleep(2 * time.Second)
	}
}

// ClickedOutside is whether the pointer, which has just clicked, is off the
// window titled title and below the bar: a click on the bar is left to the
// icon it lands on, so the panel's own icon toggles it rather than closing
// and reopening it.
func ClickedOutside(title string) bool {
	var cur struct{ X, Y int }
	var clients []struct {
		Title string
		At    [2]int
		Size  [2]int
	}
	if hyprJSON("cursorpos", &cur) != nil || hyprJSON("clients", &clients) != nil {
		return false
	}
	for _, w := range clients {
		if w.Title == title {
			return outside(cur.X, cur.Y, w.At, w.Size)
		}
	}
	return false
}

func outside(x, y int, at, size [2]int) bool {
	if y < at[1]-PanelGap { // the bar
		return false
	}
	return x < at[0] || x >= at[0]+size[0] || y < at[1] || y >= at[1]+size[1]
}

func regexpQuote(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != ' ' && c != '-' && c != '_' {
			b = append(b, '\\')
		}
		b = append(b, s[i])
	}
	return string(b)
}

func hyprJSON(what string, v any) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := proc.CommandContext(ctx, "hyprctl", "-j", what).Output()
	if err != nil {
		return err
	}
	return json.Unmarshal(out, v)
}

func hyprctl(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := proc.CommandContext(ctx, "hyprctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("hyprctl %s: %v: %s", args[0], err, out)
	}
	if s := string(out); s != "ok" && s != "ok\n" && s != "" {
		return fmt.Errorf("hyprctl %s: %s", args[0], s)
	}
	return nil
}
