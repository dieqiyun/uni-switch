package netproxy

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// system reads Internet Options' manual proxy (what most proxy clients set).
// A key or value that isn't there is no proxy; any other error is a
// failed read.
func system() (Proxy, bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return Proxy{}, errors.Is(err, registry.ErrNotExist)
	}
	defer k.Close()
	if on, _, err := k.GetIntegerValue("ProxyEnable"); err != nil || on == 0 {
		return Proxy{}, err == nil || errors.Is(err, registry.ErrNotExist)
	}
	server, _, err := k.GetStringValue("ProxyServer")
	if err != nil {
		return Proxy{}, errors.Is(err, registry.ErrNotExist)
	}
	override, _, _ := k.GetStringValue("ProxyOverride")
	return parseWindows(server, override), true
}
