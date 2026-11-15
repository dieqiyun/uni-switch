// Package webview2env keeps WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS as uni-switch
// was started with. Wails' WebView2 loader blanks the variable in its own
// init (webviewloader.preventEnvAndRegistryOverrides, so neither it nor the
// registry's policy can change the browser's arguments), before uni-switch's
// code runs, and a user's --disable-features=RendererCodeIntegrity never
// reached the renderer (#2753). This package reads it first. Go
// initializes, at each step, the first package by import path whose
// imports are all done. This one imports only syscall, so it is ready as
// soon as syscall is, and it sorts before "os"; the loader needs os, so
// os, and the loader after it, can't start while this one waits.
package webview2env

import "syscall"

// Name is the variable WebView2 reads extra browser arguments from.
const Name = "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS"

// Args is the variable's value when uni-switch started, "" when it wasn't set.
var Args, _ = syscall.Getenv(Name)
