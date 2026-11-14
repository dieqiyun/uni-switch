//go:build !nogui

package gui

import (
	"fmt"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// revealFolder shows the folder selected in its parent's Finder window,
// to drag from there; Wails' own way opens a folder rather than select it.
func revealFolder(_ *application.App, path string) error {
	if out, err := proc.Command("open", "-R", path).CombinedOutput(); err != nil {
		return fmt.Errorf("open -R: %v %s", err, out)
	}
	return nil
}
