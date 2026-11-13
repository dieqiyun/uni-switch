//go:build !nogui

package gui

import (
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func (h *host) fontsChanged() {
	application.InvokeSync(func() {
		// Read the latest saved choice here, so simultaneous saves cannot
		// deliver an older notification after a newer one. A released
		// lightweight webview will get its choices from boot.js instead.
		js := fontSettingsJS(settings.Load())
		for _, w := range []*application.WebviewWindow{h.main, h.panel} {
			if w != nil {
				w.ExecJS(js)
			}
		}
	})
}
