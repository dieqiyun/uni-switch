//go:build !nogui

package gui

import (
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// revealFolder shows the folder's parent, the folder in it to drag from
// there: file managers differ in how one is asked to select an item.
func revealFolder(app *application.App, path string) error {
	return openFolder(app, filepath.Dir(path))
}
