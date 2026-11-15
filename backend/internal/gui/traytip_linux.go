//go:build linux && !nogui

package gui

import (
	"log"
	"reflect"
	"unsafe"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// setTrayTip puts the tooltip on the tray's StatusNotifierItem. Wails
// (v3.0.0-beta.24) leaves SetTooltip on Linux a no-op ("TBD"): the item's
// ToolTip stays the "uni-switch" it was made with and no NewToolTip is sent, so
// a bar never had the quotas to show (atie on Discord: Omarchy's bar showed
// none on hover with 菜单栏显示额度 on). This sets the property Wails
// exported, through its own prop.Properties (which keeps the (sa(iiay)ss)
// type), and sends NewToolTip from the same connection: waybar reads ToolTip
// again only on that signal, Quickshell on it or PropertiesChanged.
func setTrayTip(tray *application.SystemTray, tip string) bool {
	props := trayProps(tray)
	if props == nil {
		return false
	}
	v := struct {
		V0 string           // icon name
		V1 []application.PX // icon pixmaps
		V2 string           // title: what Omarchy's bar shows, and waybar
		V3 string           // description: waybar only, under the title
	}{V2: sniTip(tip), V1: []application.PX{}}
	if err := props.Set("org.kde.StatusNotifierItem", "ToolTip", dbus.MakeVariant(v)); err != nil {
		log.Println("tray: tooltip:", err)
		return false
	}
	conn, err := dbus.SessionBus() // the connection Wails' tray is on
	if err != nil {
		return false
	}
	if err := conn.Emit("/StatusNotifierItem", "org.kde.StatusNotifierItem.NewToolTip"); err != nil {
		log.Println("tray: tooltip signal:", err)
	}
	return true
}

// trayProps is the item's exported properties inside Wails' Linux tray
// (SystemTray.impl.props), nil once a Wails version keeps them elsewhere.
func trayProps(tray *application.SystemTray) (props *prop.Properties) {
	defer func() {
		if recover() != nil {
			props = nil
		}
	}()
	if tray == nil {
		return nil
	}
	impl := trayImpl(reflect.ValueOf(tray).Elem())
	if !impl.IsValid() {
		return nil
	}
	f := impl.FieldByName("props")
	if !f.IsValid() || f.Type() != reflect.TypeOf((*prop.Properties)(nil)) {
		return nil
	}
	return *(**prop.Properties)(unsafe.Pointer(f.UnsafeAddr()))
}

// trayClick is where the tray host said the icon was clicked, across its
// bar: Wails' Linux tray keeps Activate(x, y)'s coordinates
// (SystemTray.impl.lastClickX) and passes the click handler none. ok is
// false when they aren't there to read.
func trayClick(tray *application.SystemTray) (x int, ok bool) {
	if tray == nil {
		return 0, false
	}
	return clickIn(reflect.ValueOf(tray).Elem())
}

// setTrayPassive takes the tray's StatusNotifierItem out of the bar's tray
// (Status "Passive") while uni-switch's icon is in Omarchy's bar as its widget,
// and puts it back ("Active") when it isn't. Omarchy's tray and waybar's both
// leave a Passive item out. Wails exports Status read-only and has no Hide
// on Linux, so it is set through Wails' own properties, as the tooltip is,
// and NewStatus is sent for the hosts that read it again only on that.
func setTrayPassive(tray *application.SystemTray, passive bool) (ok bool) {
	props := trayProps(tray)
	if props == nil {
		return false
	}
	status := "Active"
	if passive {
		status = "Passive"
	}
	defer func() {
		if r := recover(); r != nil {
			log.Println("tray: status:", r)
			ok = false
		}
	}()
	props.SetMust("org.kde.StatusNotifierItem", "Status", status)
	conn, err := dbus.SessionBus() // the connection Wails' tray is on
	if err != nil {
		return false
	}
	if err := conn.Emit("/StatusNotifierItem", "org.kde.StatusNotifierItem.NewStatus", status); err != nil {
		log.Println("tray: status signal:", err)
	}
	return true
}

// traySchemeLight says whether the desktop prefers a light colour scheme,
// as xdg-desktop-portal's Settings has it; false for dark, for no
// preference, and when there is no portal to ask.
func traySchemeLight() bool {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	obj := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
	var v dbus.Variant
	if err := obj.Call("org.freedesktop.portal.Settings.Read", 0, "org.freedesktop.appearance", "color-scheme").Store(&v); err != nil {
		return false
	}
	return portalLight(v)
}

// watchTrayScheme calls f with the new preference each time the desktop's
// colour scheme changes (the portal's SettingChanged), on a connection of
// its own so Wails' signals on the shared one aren't held up by it.
func watchTrayScheme(f func(light bool)) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return
	}
	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath("/org/freedesktop/portal/desktop"),
		dbus.WithMatchInterface("org.freedesktop.portal.Settings"),
		dbus.WithMatchMember("SettingChanged"),
	); err != nil {
		conn.Close()
		return
	}
	ch := make(chan *dbus.Signal, 8)
	conn.Signal(ch)
	for s := range ch {
		if len(s.Body) < 3 {
			continue
		}
		ns, _ := s.Body[0].(string)
		key, _ := s.Body[1].(string)
		v, ok := s.Body[2].(dbus.Variant)
		if ns == "org.freedesktop.appearance" && key == "color-scheme" && ok {
			f(portalLight(v))
		}
	}
}
