//go:build darwin && cgo

package gui

import "C"

// The export lives separately because its cgo preamble must not define functions.
// Copy the native string before returning; a refresh can replace the drawn cells.
//
//export mpCellClicked
func mpCellClicked(id *C.char) {
	if onTrayCellClick != nil {
		onTrayCellClick(C.GoString(id)) // the handler schedules work after startup
	}
}
