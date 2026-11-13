package gui

import (
	"context"
	"net/http"
	"runtime"
)

// windowSchemeKey carries the native transport scheme, which Wails removes
// from r.URL before handing the request to us. Network headers cannot set it.
type windowSchemeKey struct{}

// ownWindow serves the app's own windows. Wails hands their requests over
// with no RemoteAddr and fills in 192.0.2.1:1234 (TEST-NET-1), so the
// allow list took the desktop window for a computer at 192.0.2.1 and
// asked for that address to be added (Mathresearcher on Discord). Only
// the webview reaches this handler, never the network, so its requests
// come from this computer: loopback.
func ownWindow(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.RemoteAddr = "127.0.0.1:0"
		scheme := "wails"
		if runtime.GOOS == "windows" {
			scheme = "http"
		}
		r = r.WithContext(context.WithValue(r.Context(), windowSchemeKey{}, scheme))
		next.ServeHTTP(w, r)
	})
}
