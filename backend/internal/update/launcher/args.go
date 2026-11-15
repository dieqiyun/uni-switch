package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// magic ends the file; the app's name is just before it, after a NUL.
const magic = "\x00uni-switch-com-target"

// tail is how much of the file's end is read for the name: a file name
// of 255 UTF-16 units is at most 765 bytes of UTF-8.
const tail = 1024

// targetName is the app this runs: the name in its trailer, else the
// file's own name with .exe.
func targetName(self string) string {
	if f, err := os.Open(self); err == nil {
		defer f.Close()
		if st, err := f.Stat(); err == nil && st.Size() > tail {
			b := make([]byte, tail)
			if _, err := f.ReadAt(b, st.Size()-tail); err == nil && bytes.HasSuffix(b, []byte(magic)) {
				b = b[:len(b)-len(magic)]
				if name := string(b[bytes.LastIndexByte(b, 0)+1:]); name != "" && filepath.Base(name) == name {
					return name
				}
			}
		}
	}
	return strings.TrimSuffix(filepath.Base(self), filepath.Ext(self)) + ".exe"
}

// argsOf is the command line after the program's name, as typed: the
// app gets the arguments byte for byte, parsed by its own rules. The name
// ends at the closing quote, or unquoted at a space or tab.
func argsOf(line string) string {
	i := 0
	if strings.HasPrefix(line, `"`) {
		if j := strings.IndexByte(line[1:], '"'); j >= 0 {
			i = j + 2
		} else {
			i = len(line)
		}
	} else {
		for i < len(line) && line[i] != ' ' && line[i] != '\t' {
			i++
		}
	}
	return strings.TrimLeft(line[i:], " \t")
}

// opensWindow is whether args are a command the app answers with its
// window (cli.run): none, app, gui, tray, panel, -Embedding or a uni-switch://
// link.
func opensWindow(args []string) bool {
	if len(args) == 0 {
		return true
	}
	switch args[0] {
	case "app", "gui", "tray", "panel", "-Embedding":
		return true
	}
	return strings.HasPrefix(strings.ToLower(args[0]), "uni-switch:")
}
