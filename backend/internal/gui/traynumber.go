//go:build !nogui

package gui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"
	"sync"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Windows' notification area shows icons alone, never text beside them,
// so the Mac's menu bar cells can't be shown there (Benjaminliang_1 on X:
// "windows 能不能支持一下显示"). Instead the tray icon itself becomes the
// first card's figure, its digits alone ("42" for 42% used, or left as
// Settings has it), in the taskbar's own text colour on transparency, as
// a battery or a clock in that row is; the tooltip spells out every card.
// A card with no percentage (a balance, uni-switch's totals) can't be read in
// so small a square: the next card's is drawn, and with none the icon
// stays the bird. Settings › Tray icon number (settings.TrayNoNumber)
// keeps the bird.

// trayIconFigure is the figure goos's tray icon is drawn as for the
// cells: Windows' alone, unless Settings keeps the bird (noNumber).
func trayIconFigure(goos string, noNumber bool, cells []trayCell) string {
	if goos != "windows" || noNumber {
		return ""
	}
	return trayFigure(cells)
}

// trayFigure is the figure Windows' tray icon draws for the cells: the
// first row of the first cell whose first row is a percentage, its digits
// alone ("42% +3%" is "42"); "" when no cell has one.
func trayFigure(cells []trayCell) string {
	for _, c := range cells {
		if len(c.Rows) == 0 {
			continue
		}
		r, _, _ := strings.Cut(strings.TrimSpace(c.Rows[0]), " ")
		n, ok := strings.CutSuffix(r, "%")
		if ok && n != "" && len(n) <= 3 && strings.Trim(n, "0123456789") == "" {
			return n
		}
	}
	return ""
}

// Taskbar text colours: white on a dark taskbar, near black on a light
// one, as linuxTrayIcon's glyph is.
func trayNumberColour(dark bool) color.NRGBA {
	if dark {
		return trayGlyphLight
	}
	return trayGlyphDark
}

var (
	trayFontOnce sync.Once
	trayFontFace *opentype.Font
)

// trayNumberFont is the system's UI font (trayNumberFontFile: Segoe UI's
// semibold on Windows), or Go's bold where it can't be read.
func trayNumberFont() *opentype.Font {
	trayFontOnce.Do(func() {
		if b := trayNumberFontFile(); b != nil {
			if f, err := opentype.Parse(b); err == nil {
				trayFontFace = f
				return
			}
		}
		trayFontFace, _ = opentype.Parse(gobold.TTF)
	})
	return trayFontFace
}

// trayNumberIcon is fig drawn as a size×size tray icon (PNG) for a dark
// or light taskbar: the digits as tall as fits with a little room above
// and below, centred; three of them ("100") are narrowed to fit the
// square rather than drawn smaller. nil when it can't be drawn.
func trayNumberIcon(fig string, size int, dark bool) []byte {
	f := trayNumberFont()
	if f == nil || fig == "" || size < 8 {
		return nil
	}
	face := func(em float64) font.Face {
		fc, err := opentype.NewFace(f, &opentype.FaceOptions{Size: em, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			return nil
		}
		return fc
	}
	// the digits' height at 100px, to size them to the square
	probe := face(100)
	if probe == nil {
		return nil
	}
	b, _ := font.BoundString(probe, "0123456789")
	probe.Close()
	ratio := float64(b.Max.Y-b.Min.Y) / 64 / 100
	if ratio <= 0 {
		return nil
	}
	em := math.Round(float64(size)*0.72) / ratio
	fc := face(em)
	if fc == nil {
		return nil
	}
	w := float64(font.MeasureString(fc, fig)) / 64
	// narrowed no more than to 2/3 of its width; smaller beyond that
	if most := float64(size) * 1.5; w > most {
		fc.Close()
		em *= most / w
		if fc = face(em); fc == nil {
			return nil
		}
		w = float64(font.MeasureString(fc, fig)) / 64
	}
	defer fc.Close()
	db, _ := font.BoundString(fc, "0123456789")
	top, bottom := float64(db.Min.Y)/64, float64(db.Max.Y)/64
	baseline := math.Round((float64(size)-(bottom-top))/2 - top)
	text := image.NewNRGBA(image.Rect(0, 0, int(math.Ceil(w))+4, size))
	d := font.Drawer{Dst: text, Src: image.NewUniform(trayNumberColour(dark)), Face: fc,
		Dot: fixed.Point26_6{X: fixed.I(2), Y: fixed.I(int(baseline))}}
	d.DrawString(fig)
	// centred by its ink, not its advance, whose side bearings differ
	ink := inkColumns(text)
	if ink.Empty() {
		return nil
	}
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	if iw := ink.Dx(); iw <= size {
		xdraw.Copy(out, image.Pt((size-iw)/2, 0), text, ink, xdraw.Over, nil)
	} else {
		xdraw.CatmullRom.Scale(out, out.Bounds(), text, ink, xdraw.Over, nil)
	}
	var buf bytes.Buffer
	if png.Encode(&buf, out) != nil {
		return nil
	}
	return buf.Bytes()
}

// inkColumns is img's full height between its first and last columns
// with any ink.
func inkColumns(img *image.NRGBA) image.Rectangle {
	b := img.Bounds()
	x0, x1 := b.Max.X, b.Min.X
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.NRGBAAt(x, y).A > 0 {
				x0, x1 = min(x0, x), max(x1, x+1)
			}
		}
	}
	if x0 >= x1 {
		return image.Rectangle{}
	}
	return image.Rect(x0, b.Min.Y, x1, b.Max.Y)
}
