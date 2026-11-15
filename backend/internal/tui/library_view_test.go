package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/library"
)

// The library page keeps the row picked in view: its window is counted in
// the lines it draws — a row a line, a section's head and the blank before
// it two more — not in the rows alone, so a list of several sections (the
// servers, the skills, those found in the agents) can't scroll the row
// picked off the bottom of a short terminal.
func TestLibraryViewKeepsTheRowPicked(t *testing.T) {
	var lib []libRow
	lib = append(lib, libRow{kind: "instructions", name: "shared instructions"})
	for i := 0; i < 10; i++ {
		lib = append(lib, libRow{kind: "mcp", name: fmt.Sprintf("srv-%02d", i)})
	}
	for i := 0; i < 20; i++ {
		lib = append(lib, libRow{kind: "skill", name: fmt.Sprintf("skill-%02d", i)})
	}
	for i := 0; i < 3; i++ {
		lib = append(lib, libRow{kind: "found-mcp", name: fmt.Sprintf("own-mcp-%d", i)})
	}
	for i := 0; i < 5; i++ {
		lib = append(lib, libRow{kind: "found-skill", name: fmt.Sprintf("own-skill-%d", i)})
	}
	lib = append(lib, libRow{kind: "rtk", name: "RTK"})
	// a terminal some thirty lines tall: the skills alone are more than the
	// page shows at once
	m := model{w: 120, h: 30, lib: lib, libView: &library.View{Dir: "/x"}}
	for _, pick := range []string{"skill-10", "own-mcp-0", "own-skill-4", "RTK"} {
		m.lrow = libIndexOf(lib, pick)
		v := m.viewLibrary()
		if !strings.Contains(v, "▸ "+pick) {
			t.Fatalf("row %d (%s) picked, but the view stops short of it:\n%s", m.lrow, pick, v)
		}
	}
	// the page starts at its top while it all fits
	m.lrow = 0
	if v := m.viewLibrary(); !strings.Contains(v, "shared instructions") {
		t.Fatalf("the top of the list isn't shown:\n%s", v)
	}
}

func libIndexOf(lib []libRow, name string) int {
	for i, r := range lib {
		if r.name == name {
			return i
		}
	}
	return -1
}
