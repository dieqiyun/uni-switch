//go:build !nogui

package gui

import (
	"bytes"
	"image"
	"image/png"
	"io/fs"
	"testing"
)

// The menu bar's own glyphs are about 18pt in its 22pt: a bird drawn to the
// canvas's edges looked too big beside them (@SherlockYoYo), and its flap
// frames were cut off at the sides.
func TestTrayIconSize(t *testing.T) {
	files := map[string][]byte{"tray.png": trayIcon}
	names, _ := fs.Glob(trayFlap, "trayflap/*.png")
	for _, n := range names {
		b, _ := trayFlap.ReadFile(n)
		files[n] = b
	}
	for n, b := range files {
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		box := image.Rectangle{Min: img.Bounds().Max, Max: img.Bounds().Min}
		for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
			for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
				if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
					box = box.Union(image.Rect(x, y, x+1, y+1))
				}
			}
		}
		if img.Bounds().Dx() != 44 || box.Dx() > 36 || box.Dy() > 36 || box.Min.X < 2 || box.Max.X > 42 {
			t.Errorf("%s: glyph %v in %v, want at most 36px (18pt) and clear of the sides", n, box, img.Bounds())
		}
	}
}
