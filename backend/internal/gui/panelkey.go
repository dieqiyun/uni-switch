package gui

import (
	"errors"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
)

// The quick panel's global shortcut (Settings › General): a key
// combination the user records, which opens the tray's panel from any app
// (ben hanks on Discord). There is none by default, so nothing uni-switch
// takes clashes with another app's until the user picks it.

// onPanelShortcut binds the shortcut s ("" for none) in the running app in
// place of the one before, and says why it couldn't; the app sets it
// (app.go). Off the app (uni-switch web, tests) it is nil and the choice is
// only saved.
var onPanelShortcut func(s string) error

// panelKeyTaken is the shortcut the app couldn't bind as it started,
// another app holding it; "" once one is bound or none is set.
var panelKeyTaken atomic.Value // string

// errPanelKeyTaken is what the page is told when the system refuses the
// combination, as Windows and X11 do for one another app holds (the Mac's
// Carbon hot keys take it twice, so there it isn't said).
var errPanelKeyTaken = errors.New("Another app already uses this shortcut")

// panelKeysJSON is what Settings' row needs beside the saved shortcut.
type panelKeysJSON struct {
	// Can is whether this system lets uni-switch take a global shortcut at all
	Can bool `json:"can"`
	// Command opens the panel from a shortcut the desktop binds itself,
	// where uni-switch can't (Wayland)
	Command string `json:"command,omitempty"`
	// Taken is the saved shortcut the system refused as uni-switch started
	Taken string `json:"taken,omitempty"`
}

func panelKeysState() panelKeysJSON {
	st := panelKeysJSON{Can: !waylandSession(runtime.GOOS, os.Getenv)}
	if !st.Can {
		exe, err := os.Executable()
		if err != nil || exe == "" {
			exe = "uni-switch"
		}
		st.Command = shellWord(exe) + " panel"
	}
	st.Taken, _ = panelKeyTaken.Load().(string)
	return st
}

// waylandSession says the desktop is a Wayland session, where no app may
// grab a key for itself: only the desktop's own settings (or its
// GlobalShortcuts portal, which leaves the keys to the compositor and on
// Hyprland binds none until the user writes one) can. It reads the session
// as Wails does in choosing its backend.
func waylandSession(goos string, getenv func(string) string) bool {
	if goos != "linux" {
		return false
	}
	switch strings.ToLower(getenv("XDG_SESSION_TYPE")) {
	case "wayland":
		return true
	case "x11":
		return false
	}
	return getenv("WAYLAND_DISPLAY") != ""
}

// shellWord quotes a path with a space or a quote for a POSIX shell.
func shellWord(s string) string {
	if !strings.ContainsAny(s, " '\"\\$`!&;()|<>*?[]#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// panelShortcut checks a shortcut the page recorded and writes it as
// Wails' accelerator, its modifiers in the Mac's order (Ctrl, Option,
// Shift, Cmd) and the key last: "Ctrl+Option+K", "Cmd+Shift+Space". The
// Command key is "Cmd" on the Mac and the Windows or Super key "Super" off
// it, as Wails reads them (its "Cmd" off the Mac is Ctrl). A key is a
// letter, a digit, Space or F1–F20, the keys every system's hot key table
// has; one with no modifier but Shift is refused, since it would take a
// key from typing everywhere, but an F key may stand alone. "" is none.
func panelShortcut(s, goos string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	var ctrl, alt, shift, meta bool
	parts := strings.Split(s, "+")
	key := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
	for _, part := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "ctrl", "control":
			ctrl = true
		case "alt", "option":
			alt = true
		case "shift":
			shift = true
		case "cmd", "command", "super", "win", "meta":
			meta = true
		default:
			return "", errNotShortcut
		}
	}
	fkey := len(key) >= 2 && key[0] == 'f' && slices.Contains(fKeys, key[1:])
	switch {
	case len(key) == 1 && (key[0] >= 'a' && key[0] <= 'z' || key[0] >= '0' && key[0] <= '9'):
		key = strings.ToUpper(key)
	case key == "space":
		key = "Space"
	case fkey:
		key = strings.ToUpper(key)
	default:
		return "", errNotShortcut
	}
	if !ctrl && !alt && !meta && !fkey {
		return "", errNotShortcut
	}
	var out []string
	if ctrl {
		out = append(out, "Ctrl")
	}
	if alt {
		out = append(out, map[bool]string{true: "Option", false: "Alt"}[goos == "darwin"])
	}
	if shift {
		out = append(out, "Shift")
	}
	if meta {
		out = append(out, map[bool]string{true: "Cmd", false: "Super"}[goos == "darwin"])
	}
	return strings.Join(append(out, key), "+"), nil
}

// errNotShortcut is what the page is told of a combination it shouldn't
// have sent: the page checks each press itself first.
var errNotShortcut = errors.New("Not a shortcut uni-switch can use")

var fKeys = []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15", "16", "17", "18", "19", "20"}
