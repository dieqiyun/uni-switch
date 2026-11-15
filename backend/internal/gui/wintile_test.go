//go:build !nogui

package gui

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// Windows draws an app's icon to the edges of its square. Made on the
// Mac's grid, uni-switch's tile was 80% of it and looked a size smaller than
// its neighbours in the taskbar, the tray and Start (dzxx0496). Each
// picture the exe's icon group lists (the taskbar, the window, Start's
// shortcut) and each size of the tray's .ico is a tile over at least
// 15/16 of its side, centred, and opaque in the middle.
func TestWindowsIconsFillTheirSquare(t *testing.T) {
	var res struct {
		Group map[string]map[string][]string `json:"RT_GROUP_ICON"`
	}
	b, err := os.ReadFile(filepath.Join("..", "..", "build", "windows", "winres.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, f := range res.Group["#3"]["0000"] {
		b, err := os.ReadFile(filepath.Join("..", "..", "build", "windows", f))
		if err != nil {
			t.Fatal(err)
		}
		files["build/windows/"+f] = b
	}
	if len(files) == 0 {
		t.Fatal("winres.json lists no icon for group #3")
	}
	ico := winTrayIcon
	le := binary.LittleEndian
	if len(ico) < 6 || le.Uint16(ico[2:]) != 1 {
		t.Fatal("icon-windows.ico is not an .ico")
	}
	n := int(le.Uint16(ico[4:]))
	sizes := map[int]bool{}
	for i := range n {
		e := ico[6+16*i:]
		size, at := int(le.Uint32(e[8:])), int(le.Uint32(e[12:]))
		files["icon-windows.ico#"+string(rune('0'+i))] = ico[at : at+size]
		sizes[int(e[0])] = true
	}
	for _, s := range []int{16, 24, 32} {
		if !sizes[s] {
			t.Errorf("icon-windows.ico has no %dpx picture", s)
		}
	}
	for name, b := range files {
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		r := img.Bounds()
		box := image.Rectangle{Min: r.Max, Max: r.Min}
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
					box = box.Union(image.Rect(x, y, x+1, y+1))
				}
			}
		}
		if box.Dx()*16 < r.Dx()*15 || box.Dy()*16 < r.Dy()*15 || box.Min.X-r.Min.X != r.Max.X-box.Max.X {
			t.Errorf("%s: tile %v in %v, want at least 15/16 of the side, centred", name, box, r)
		}
		if _, _, _, a := img.At(r.Dx()/2, r.Dy()/8).RGBA(); a != 0xffff {
			t.Errorf("%s: the tile is not opaque inside", name)
		}
	}
}
