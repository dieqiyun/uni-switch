//go:build !dev && !nogui

package gui

import "net/http"

func devShell(*host) http.Handler { return nil }
