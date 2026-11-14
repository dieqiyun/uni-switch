package provider

import (
	"bytes"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
)

// iconSide is the longest side, in pixels, of a picture the window is sent.
// The window draws an icon at 20 to 64 points, so 256 is sharp at a 250%
// display; a webview keeps every picture on the page decoded at its own
// size, and Plugins' 1254px icons cost 6 MB each (#909, #1956, #2459).
const iconSide = 256

// iconBigPixels is the most pixels a picture is decoded for scaling: past
// it (a small file can hold a huge PNG) the picture is sent as it is.
const iconBigPixels = 64 << 20

var smallIconMu sync.Mutex

// smallIconDir is where the scaled copies are kept: a cache, made again
// from the icons when it is gone.
func smallIconDir() string { return filepath.Join(appdir.Cache(), "icons-small") }

// IconServed is the file to send for a stored picture and its type, ""
// to go by its name; path is "" for a name IconFile refuses. A PNG, JPEG
// or WebP longer than iconSide on a side is sent as a PNG copy scaled
// down to it, made on its first ask and kept; one that can't be read or
// scaled is sent as it is.
func IconServed(name string) (path, ctype string) {
	f := IconFile(name)
	if f == "" {
		return "", ""
	}
	switch filepath.Ext(name) {
	case ".png", ".jpg", ".webp":
	default:
		return f, ""
	}
	small := filepath.Join(smallIconDir(), name+".png")
	if _, err := os.Stat(small); err == nil {
		return small, "image/png"
	}
	smallIconMu.Lock()
	defer smallIconMu.Unlock()
	if _, err := os.Stat(small); err == nil {
		return small, "image/png"
	}
	b, err := scaledIcon(f)
	if err != nil || b == nil {
		return f, ""
	}
	if err := os.MkdirAll(smallIconDir(), 0o755); err != nil {
		return f, ""
	}
	tmp, err := os.CreateTemp(smallIconDir(), ".small-*")
	if err != nil {
		return f, ""
	}
	_, werr := tmp.Write(b)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil || os.Rename(tmp.Name(), small) != nil {
		os.Remove(tmp.Name())
		return f, ""
	}
	return small, "image/png"
}

// scaledIcon is the picture at f scaled to iconSide as a PNG, nil when it
// is that small already or too big to decode.
func scaledIcon(f string) ([]byte, error) {
	data, err := os.ReadFile(f)
	if err != nil {
		return nil, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	w, h := cfg.Width, cfg.Height
	if max(w, h) <= iconSide || w*h > iconBigPixels {
		return nil, nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	sw, sh := iconSide, max(1, h*iconSide/w)
	if h > w {
		sw, sh = max(1, w*iconSide/h), iconSide
	}
	dst := image.NewNRGBA(image.Rect(0, 0, sw, sh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// pruneSmallIcons drops the scaled copies of pictures no longer kept.
func pruneSmallIcons() {
	ents, _ := os.ReadDir(smallIconDir())
	for _, e := range ents {
		name, ok := strings.CutSuffix(e.Name(), ".png")
		if !ok || !iconName.MatchString(name) {
			continue
		}
		if _, err := os.Stat(filepath.Join(IconDir(), name)); os.IsNotExist(err) {
			os.Remove(filepath.Join(smallIconDir(), e.Name()))
		}
	}
}
