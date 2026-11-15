package gui

// copyHeld says whether a copy is made, from a read back of the clipboard
// (#1881): read is 1 when the clipboard was read and held is what it
// holds, 0 when it holds no text, -1 when
// it couldn't be told. Only the text itself, read back, is a copy made; an
// empty read is GTK closing the pipe on a paste, which is the bug itself.
func copyHeld(read int, held, text string) bool {
	return read == 1 && held == text
}
