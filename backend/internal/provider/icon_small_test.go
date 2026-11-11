package provider

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func iconHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
}

func pngOf(t *testing.T, w, h int) []byte {
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			m.Set(x, y, color.NRGBA{uint8(x), uint8(y), 200, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, m); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func storedName(t *testing.T, data []byte) string {
	ref, err := StoreIcon(data)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimPrefix(ref, "file:")
}

// a webview keeps a picture decoded at its own size: a 1254px icon drawn
// at 20 points held 6 MB in the Plugins page's renderer (#909)
func TestBigIconServedScaled(t *testing.T) {
	iconHome(t)
	name := storedName(t, pngOf(t, 1254, 600))
	f, ctype := IconServed(name)
	if ctype != "image/png" || f == IconFile(name) {
		t.Fatalf("IconServed = %q, %q: want a scaled copy", f, ctype)
	}
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != iconSide || cfg.Height != 600*iconSide/1254 {
		t.Errorf("scaled to %dx%d, want %dx%d", cfg.Width, cfg.Height, iconSide, 600*iconSide/1254)
	}
	if again, _ := IconServed(name); again != f {
		t.Errorf("second ask = %q, want the kept copy %q", again, f)
	}
	// the original is gone: its copy goes with it
	os.Remove(IconFile(name))
	pruneSmallIcons()
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Errorf("copy of a dropped icon kept: %v", err)
	}
}

func TestSmallIconServedAsItIs(t *testing.T) {
	iconHome(t)
	small := storedName(t, pngOf(t, 64, 64))
	if f, ctype := IconServed(small); f != IconFile(small) || ctype != "" {
		t.Errorf("64px icon = %q, %q: want the file itself", f, ctype)
	}
	svg := storedName(t, []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" width="1024" height="1024"/>`))
	if f, ctype := IconServed(svg); f != IconFile(svg) || ctype != "" {
		t.Errorf("svg = %q, %q: want the file itself", f, ctype)
	}
	// a file that says PNG and doesn't decode is still sent
	bad := storedName(t, []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"))
	if f, _ := IconServed(bad); f != IconFile(bad) {
		t.Errorf("undecodable png = %q, want the file itself", f)
	}
	if f, _ := IconServed("../providers.json"); f != "" {
		t.Errorf("a name outside the folder = %q", f)
	}
}
