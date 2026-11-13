//go:build !nogui

package gui

import (
	"bytes"
	"image/color"
	"image/png"
	"testing"

	"github.com/godbus/dbus/v5"
)

// ylorn on Discord: a Linux bar's tray icon was the app's icon, a bird on
// an opaque white tile, which a bar tinting its icons by their alpha
// turned into a solid block. It is the bird's silhouette alone, in one
// colour on transparency, light unless the desktop prefers light; the Mac
// keeps its template and Windows the app's icon.
func TestTrayIconPerOS(t *testing.T) {
	for _, c := range []struct {
		light bool
		want  color.NRGBA
	}{{false, trayGlyphLight}, {true, trayGlyphDark}} {
		icon, template := trayIconFor("linux", c.light)
		if template {
			t.Errorf("linux: a template icon, which no Linux bar tints")
		}
		img, err := png.Decode(bytes.NewReader(icon))
		if err != nil {
			t.Fatal(err)
		}
		glyph, _ := png.Decode(bytes.NewReader(trayIcon))
		if img.Bounds() != glyph.Bounds() {
			t.Fatalf("linux: %v, want the menu bar bird's %v", img.Bounds(), glyph.Bounds())
		}
		b := img.Bounds()
		shown := 0
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				p := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				g := color.NRGBAModel.Convert(glyph.At(x, y)).(color.NRGBA)
				if p.A != g.A {
					t.Fatalf("linux light=%v: alpha %d at %d,%d, want the bird's %d (a tile or another shape)", c.light, p.A, x, y, g.A)
				}
				if p.A > 0 {
					shown++
					if p.R != c.want.R || p.G != c.want.G || p.B != c.want.B {
						t.Fatalf("linux light=%v: %v at %d,%d, want every pixel %v", c.light, p, x, y, c.want)
					}
				}
			}
		}
		if shown == 0 || shown == b.Dx()*b.Dy() {
			t.Fatalf("linux: %d of %d pixels shown, want the silhouette on transparency", shown, b.Dx()*b.Dy())
		}
	}
	if icon, template := trayIconFor("darwin", false); !template || !bytes.Equal(icon, trayIcon) {
		t.Errorf("darwin: want the template bird")
	}
	if icon, template := trayIconFor("windows", false); template || !bytes.Equal(icon, winTrayIcon) {
		t.Errorf("windows: want the Windows tile")
	}
	if img, _ := png.Decode(bytes.NewReader(trayGlyph(trayGlyphLight))); img == nil || img.ColorModel() != color.NRGBAModel {
		t.Errorf("trayGlyph: want an NRGBA png, got %T", img)
	}
}

// The portal's color-scheme: 1 prefers dark, 2 light, 0 neither; Read
// wraps it in a second variant, ReadOne and SettingChanged don't.
func TestPortalLight(t *testing.T) {
	for _, c := range []struct {
		v    dbus.Variant
		want bool
	}{
		{dbus.MakeVariant(uint32(2)), true},
		{dbus.MakeVariant(dbus.MakeVariant(uint32(2))), true},
		{dbus.MakeVariant(uint32(1)), false},
		{dbus.MakeVariant(dbus.MakeVariant(uint32(0))), false},
		{dbus.MakeVariant("light"), false},
	} {
		if got := portalLight(c.v); got != c.want {
			t.Errorf("portalLight(%v) = %v, want %v", c.v, got, c.want)
		}
	}
}
