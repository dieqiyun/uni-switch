package gui

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/godbus/dbus/v5"
)

// sessionBus reports whether a D-Bus session bus is reachable; Wails'
// single-instance lock needs one and gives up on the whole app without it.
func sessionBus() bool {
	_, err := dbus.SessionBus()
	return err == nil
}

// dropTrayName gives up the org.kde.StatusNotifierItem-<pid>-1 name Wails'
// tray takes. The tray registers with the watcher by its path, which the
// watcher files under the connection's own name (:1.x); anything that also
// registers the well-known name (songlairui on X: 为什么我的 omarchy 上会有
// 两只喜鹊) makes the same icon a second item, as Quickshell's watcher tells
// items apart by how they were named, not by who owns them. Without the name
// such a registration is refused, and one made already is dropped with it.
func dropTrayName() {
	conn, err := dbus.SessionBus()
	if err != nil {
		return
	}
	name := fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid())
	// the tray takes it when the app has started, a moment after this
	for range 120 {
		var owner string
		if conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, name).Store(&owner) == nil {
			if _, err := conn.ReleaseName(name); err != nil {
				log.Println("tray: release", name+":", err)
				return
			}
			// A watcher that filed the item under the name it was given up
			// by drops it with the name (noctalia, #2421): register again by
			// the connection's own name, which one that filed it so already
			// takes as the item it has.
			if own := conn.Names(); len(own) > 0 {
				w := conn.Object("org.kde.StatusNotifierWatcher", "/StatusNotifierWatcher")
				if c := w.Call("org.kde.StatusNotifierWatcher.RegisterStatusNotifierItem", 0, own[0]); c.Err != nil {
					log.Println("tray: register", own[0]+":", c.Err)
				}
			}
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}
