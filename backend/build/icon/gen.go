//go:build ignore

// Icon generator. Everything uni-switch shows is drawn from one bird: a round
// little uni-switch in profile, tail cocked, black and white, on a 44-unit grid.
// uni-switch.svg is the full drawing, its belly, shoulder and the slits in its
// tail cut out of the black; the app tile uses it from 64px up.
// uni-switch-small.svg drops the eye, which would only be a speck; the tray and
// the smaller tiles use it, and so does the header in
// internal/gui/assets/index.html.
//
//	go run build/icon/gen.go tray internal/gui/tray.png     # 44px black template icon (macOS menu bar)
//	go run build/icon/gen.go tray-flap internal/gui/trayflap # its frames when clicked (a size after the folder for a bigger look)
//	go run build/icon/gen.go app 64 internal/gui/icon.png   # app icon at a given size
//	go run build/icon/gen.go win 256 build/windows/icon-256.png # the Windows tile, filling its square
//	go run build/icon/gen.go win-ico internal/gui/icon-windows.ico # it at the tray's sizes, one .ico
//	make icons                                              # regenerates all of them plus uni-switch.icns
package main

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

var (
	//go:embed uni-switch.svg
	fullSVG string
	//go:embed uni-switch-small.svg
	smallSVG string
)

func main() {
	var img image.Image
	switch {
	case len(os.Args) == 3 && os.Args[1] == "tray":
		img = tray()
	case len(os.Args) == 3 && os.Args[1] == "tray-flap":
		if err := trayFlap(os.Args[2], 44); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	case len(os.Args) == 4 && os.Args[1] == "tray-flap":
		n, _ := strconv.Atoi(os.Args[3])
		if err := trayFlap(os.Args[2], n); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	case len(os.Args) == 4 && os.Args[1] == "glyph":
		n, _ := strconv.Atoi(os.Args[2])
		img = glyph(n)
	case len(os.Args) == 4 && (os.Args[1] == "app" || os.Args[1] == "app-dark"):
		n, _ := strconv.Atoi(os.Args[2])
		img = app(n, os.Args[1] == "app-dark")
	case len(os.Args) == 4 && os.Args[1] == "win":
		n, _ := strconv.Atoi(os.Args[2])
		img = win(n)
	case len(os.Args) == 3 && os.Args[1] == "win-ico":
		if err := winICO(os.Args[2], trayICOSizes); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	default:
		fmt.Fprintln(os.Stderr, "usage: gen.go tray <out.png> | gen.go app|app-dark|win <size> <out.png> | gen.go win-ico <out.ico>")
		os.Exit(2)
	}
	f, err := os.Create(os.Args[len(os.Args)-1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// trayK is the tray bird's size in its 22pt square: drawn to the edges it
// was a full 22pt wide, bigger than the menu bar's own glyphs (@SherlockYoYo:
// 感觉这只鸟有些大); at .8 it is about 18pt by 14pt, centred.
const trayK = 0.8

// tray draws the silhouette as a macOS template image (black + alpha).
func tray() image.Image {
	const S = 44
	off := S / 2 * (1 - trayK)
	cov := coverage(parse(smallSVG), S, 8, trayK, off, off)
	img := image.NewNRGBA(image.Rect(0, 0, S, S))
	for i, a := range cov {
		img.SetNRGBA(i%S, i/S, color.NRGBA{0, 0, 0, uint8(a*255 + 0.5)})
	}
	return img
}

// The tray bird's flap is the header logo's (internal/gui/assets/app.css,
// .logo.wag): the drawing cut into body, tail and wing by the same clip
// paths, each piece turned about the same point through the same keyframes
// over the same .9s, cubic-bezier(.3, 0, .3, 1) between each two of them,
// and the whole bird bobbing about its feet. Keep the two alike.
const (
	flapSecs  = 0.9
	flapEvery = 0.03 // a frame every 30ms: internal/gui's flap plays them so
	// the clip paths of index.html's #bird-body (even-odd: the wing is cut
	// out of it), #bird-tail and #bird-wing, their H and V written as L for
	// parse
	clipBody = "M16 0L44 0L44 44L0 44L0 26.4L10.6 26.4L13 24.2L16 19Z M29.8 18.3L26 19L22 21L17 23.5L14.6 24.7L10.6 26.4L6 29V32L9 31.5L12.5 32L16.4 31.8L19 31.4L24 28.8L28.5 24L30.6 21.4Z"
	clipTail = "M0 0L17 0L17.2 19.6L14 25L10.6 26.4L0 26.4Z"
	clipWing = "M29.8 18.3L26 19L22 21L17 23.5L14.6 24.7L10.6 26.4L6 29V32L9 31.5L12.5 32L16.4 31.8L19 31.4L24 28.8L28.5 24L30.6 21.4Z"
)

// A keyframe: at is the fraction of the animation, deg the rotation.
type key struct{ at, deg float64 }

var (
	wingKeys = []key{{0, 0}, {.12, 10}, {.26, -3}, {.40, 8}, {.56, -2}, {.72, 3}, {1, 0}}
	tailKeys = []key{{0, 0}, {.18, 16}, {.36, -7}, {.54, 9}, {.72, -3}, {.86, 2}, {1, 0}}
	bobKeys  = []key{{0, 0}, {.18, -4}, {.40, 2}, {.62, -1}, {1, 0}}
	// transform-origin, in the viewBox
	wingAt, tailAt, bobAt = [2]float64{30, 19.8}, [2]float64{14, 22}, [2]float64{26, 38}
)

// trayFlap writes the frames the tray icon plays when it is clicked,
// flap-01.png on, one every flapEvery of the animation, the still bird at
// both ends left out.
func trayFlap(dir string, S int) error {
	bird := parse(smallSVG)
	body, tail, wing := parse(`d="`+clipBody+`"`), parse(`d="`+clipTail+`"`), parse(`d="`+clipWing+`"`)
	n := int(math.Round(flapSecs / flapEvery))
	const ss = 8
	k := float64(S) / 44
	for f := 1; f < n; f++ {
		t := float64(f) / float64(n)
		bob, tl, wg := keyed(bobKeys, t), keyed(tailKeys, t), keyed(wingKeys, t)
		// each piece is the bird and its clip path both turned as CSS turns
		// the piece's group: the bob outside, the piece's own turn inside
		m := func(rings [][][2]float64, at [2]float64, deg float64) []bool {
			moved := make([][][2]float64, len(rings))
			for i, r := range rings {
				moved[i] = make([][2]float64, len(r))
				for j, p := range r {
					q := turn(turn(p, at, deg), bobAt, bob)
					// shrunk about the middle, as tray() draws the still bird
					moved[i][j] = [2]float64{22 + (q[0]-22)*trayK, 22 + (q[1]-22)*trayK}
				}
			}
			return mask(moved, S*ss, k*ss)
		}
		pieces := [][2][]bool{
			{m(bird, tailAt, 0), m(body, tailAt, 0)},
			{m(bird, tailAt, tl), m(tail, tailAt, tl)},
			{m(bird, wingAt, wg), m(wing, wingAt, wg)},
		}
		N := S * ss
		img := image.NewNRGBA(image.Rect(0, 0, S, S))
		for py := 0; py < S; py++ {
			for px := 0; px < S; px++ {
				hit := 0
				for sy := 0; sy < ss; sy++ {
					for sx := 0; sx < ss; sx++ {
						i := (py*ss+sy)*N + px*ss + sx
						for _, pc := range pieces {
							if pc[0][i] && pc[1][i] {
								hit++
								break
							}
						}
					}
				}
				img.SetNRGBA(px, py, color.NRGBA{0, 0, 0, uint8(float64(hit)/(ss*ss)*255 + 0.5)})
			}
		}
		out, err := os.Create(fmt.Sprintf("%s/flap-%02d.png", dir, f))
		if err != nil {
			return err
		}
		err = png.Encode(out, img)
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// keyed is the rotation at t of keyframes played as CSS plays them, each
// step eased by cubic-bezier(.3, 0, .3, 1).
func keyed(ks []key, t float64) float64 {
	for i := 1; i < len(ks); i++ {
		if t <= ks[i].at {
			a, b := ks[i-1], ks[i]
			return a.deg + (b.deg-a.deg)*ease((t-a.at)/(b.at-a.at))
		}
	}
	return ks[len(ks)-1].deg
}

// ease is cubic-bezier(.3, 0, .3, 1): x found for the curve's parameter by
// bisection, and its y.
func ease(x float64) float64 {
	bez := func(u, p1, p2 float64) float64 { return 3*u*(1-u)*(1-u)*p1 + 3*u*u*(1-u)*p2 + u*u*u }
	lo, hi := 0.0, 1.0
	for range 40 {
		m := (lo + hi) / 2
		if bez(m, .3, .3) < x {
			lo = m
		} else {
			hi = m
		}
	}
	return bez((lo+hi)/2, 0, 1)
}

// turn rotates p about o by deg degrees, clockwise on screen as CSS's
// rotate() is.
func turn(p, o [2]float64, deg float64) [2]float64 {
	a := deg * math.Pi / 180
	s, c := math.Sin(a), math.Cos(a)
	dx, dy := p[0]-o[0], p[1]-o[1]
	return [2]float64{o[0] + dx*c - dy*s, o[1] + dx*s + dy*c}
}

// mask is which of an n×n grid of samples, k to a unit, fall in the
// rings, even-odd.
func mask(rings [][][2]float64, n int, k float64) []bool {
	out := make([]bool, n*n)
	var xs []float64
	for y := 0; y < n; y++ {
		v := (float64(y) + 0.5) / k
		xs = xs[:0]
		for _, r := range rings {
			for i, j := 0, len(r)-1; i < len(r); j, i = i, i+1 {
				a, b := r[j], r[i]
				if (a[1] > v) != (b[1] > v) {
					xs = append(xs, (a[0]+(v-a[1])*(b[0]-a[0])/(b[1]-a[1]))*k)
				}
			}
		}
		sort.Float64s(xs)
		for i := 0; i+1 < len(xs); i += 2 {
			lo := int(math.Max(0, math.Ceil(xs[i]-0.5)))
			hi := int(math.Min(float64(n), math.Ceil(xs[i+1]-0.5)))
			for x := lo; x < hi; x++ {
				out[y*n+x] = true
			}
		}
	}
	return out
}

// glyph is the bird alone, black on clear, as big on an n-pixel canvas as
// app draws it on its tile: the layer of build/darwin/AppIcon.icon, which
// macOS 26 lays on a tile of its own, light or dark (#117).
func glyph(n int) image.Image {
	S := float64(n)
	k := S * 0.8 / 44
	o := S/2 - 22*k
	cov := coverage(parse(fullSVG), n, 4, k, o, o)
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	for i, a := range cov {
		img.SetNRGBA(i%n, i/n, color.NRGBA{0, 0, 0, uint8(a*255 + 0.5)})
	}
	return img
}

// app draws a rounded square, white shading to the palest grey, with the
// bird on it in ink, on macOS's icon grid: the tile is ~80% of the
// canvas, the rest left clear as the Dock and Finder expect.
func app(n int, dark bool) image.Image {
	S := float64(n)
	inset := S * 0.1
	return tile(n, inset, (S-2*inset)*0.225, dark)
}

// win is the same tile as Windows draws an app's icon: filling its square,
// as the icons beside it in the taskbar, the tray and Start do. On macOS's
// grid it was a fifth smaller than its neighbours there (dzxx0496). A
// pixel is left round it from 32px up, a 32nd of the side, so the rounded
// corners' soft edge stays inside the square; under 32px that pixel would
// be most of the margin and only blur the edge, so the tile meets it.
func win(n int) image.Image {
	inset := float64(n / 32)
	return tile(n, inset, (float64(n)-2*inset)*0.2, false)
}

// tile draws the rounded square inset from each edge of an n-pixel canvas,
// its corners of the given radius, and the bird on it at .8 of its side.
func tile(n int, inset, radius float64, dark bool) image.Image {
	S := float64(n)
	side := S - 2*inset
	k := side * 0.8 / 44
	o := inset + side/2 - 22*k
	ss := 4
	if n <= 64 {
		ss = 8
	}
	// under 64px the eye is a grey speck; leave it out
	shape := fullSVG
	if n < 64 {
		shape = smallSVG
	}
	bird := coverage(parse(shape), n, ss, k, o, o)
	bg1, bg2 := 0xff, 0xec // top, bottom
	ink := 0x16
	if dark {
		// the Dock's dark appearance (macOS 26): the bird in white on a
		// dark tile, as macOS would otherwise darken the tile under a black
		// bird and lose it (#117)
		bg1, bg2, ink = 0x3a, 0x22, 0xf4
	}
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			var tile float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					fx := float64(x) + (float64(sx)+0.5)/float64(ss)
					fy := float64(y) + (float64(sy)+0.5)/float64(ss)
					if roundRect(fx, fy, inset, inset, side, side, radius) {
						tile++
					}
				}
			}
			if tile == 0 {
				continue
			}
			t := float64(y) / S
			bg := float64(bg1)*(1-t) + float64(bg2)*t
			a := bird[y*n+x]
			g := uint8(bg*(1-a) + float64(ink)*a + 0.5)
			img.SetNRGBA(x, y, color.NRGBA{g, g, g, uint8(tile / float64(ss*ss) * 255)})
		}
	}
	return img
}

// trayICOSizes are the sizes Windows asks a tray icon for (SM_CXSMICON):
// 16px at 100% scaling, 20, 24, 32 and 40 at 125% to 250%, and 48 and 64
// beyond. Wails picks the one it needs from the .ico, where a single PNG
// was scaled down by the system and blurred.
var trayICOSizes = []int{16, 20, 24, 32, 40, 48, 64}

// winICO writes win's tile at each size into one .ico, each image a PNG
// (Windows Vista on reads them).
func winICO(out string, sizes []int) error {
	var pngs [][]byte
	for _, n := range sizes {
		var b bytes.Buffer
		if err := png.Encode(&b, win(n)); err != nil {
			return err
		}
		pngs = append(pngs, b.Bytes())
	}
	var ico bytes.Buffer
	le := binary.LittleEndian
	ico.Write(le.AppendUint16(le.AppendUint16(le.AppendUint16(nil, 0), 1), uint16(len(sizes))))
	at := 6 + 16*len(sizes)
	for i, n := range sizes {
		d := byte(n)
		if n >= 256 {
			d = 0
		}
		e := []byte{d, d, 0, 0}
		e = le.AppendUint16(e, 1)  // planes
		e = le.AppendUint16(e, 32) // bits per pixel
		e = le.AppendUint32(e, uint32(len(pngs[i])))
		e = le.AppendUint32(e, uint32(at))
		ico.Write(e)
		at += len(pngs[i])
	}
	for _, p := range pngs {
		ico.Write(p)
	}
	return os.WriteFile(out, ico.Bytes(), 0o644)
}

// coverage rasterises a shape, scaled by k and moved to (ox, oy), onto an n×n
// grid of pixels with ss×ss samples each, and gives each pixel's coverage in
// 0..1. It fills even-odd, a scanline at a time.
func coverage(rings [][][2]float64, n, ss int, k, ox, oy float64) []float64 {
	cov := make([]float64, n*n)
	per := 1 / float64(ss*ss)
	var xs []float64
	for y := 0; y < n*ss; y++ {
		v := ((float64(y)+0.5)/float64(ss) - oy) / k
		xs = xs[:0]
		for _, r := range rings {
			for i, j := 0, len(r)-1; i < len(r); j, i = i, i+1 {
				a, b := r[j], r[i]
				if (a[1] > v) != (b[1] > v) {
					u := a[0] + (v-a[1])*(b[0]-a[0])/(b[1]-a[1])
					xs = append(xs, (u*k+ox)*float64(ss))
				}
			}
		}
		sort.Float64s(xs)
		row := cov[(y/ss)*n:][:n]
		for i := 0; i+1 < len(xs); i += 2 {
			// sample centres x+0.5 that fall between the two crossings
			lo := int(math.Max(0, math.Ceil(xs[i]-0.5)))
			hi := int(math.Min(float64(n*ss), math.Ceil(xs[i+1]-0.5)))
			for x := lo; x < hi; x++ {
				row[x/ss] += per
			}
		}
	}
	return cov
}

// parse reads the path out of one of the SVGs: absolute M, L, C and Z
// commands, each curve cut into short segments, one polygon per subpath.
func parse(svg string) [][][2]float64 {
	d := svg[strings.Index(svg, ` d="`)+4:]
	d = d[:strings.IndexByte(d, '"')]
	f := strings.Fields(strings.NewReplacer("M", " M ", "L", " L ", "C", " C ", "Z", " Z ").Replace(d))
	num := func(i int) float64 { x, _ := strconv.ParseFloat(f[i], 64); return x }
	var rings [][][2]float64
	var pts [][2]float64
	cmd := ""
	for i := 0; i < len(f); {
		if c := f[i]; c == "M" || c == "L" || c == "C" || c == "Z" {
			cmd = c
			i++
		}
		switch cmd {
		case "M", "L":
			if cmd == "M" && len(pts) > 0 {
				rings, pts = append(rings, pts), nil
			}
			pts = append(pts, [2]float64{num(i), num(i + 1)})
			i += 2
			cmd = "L" // further pairs after M are lines
		case "C":
			p := pts[len(pts)-1]
			c := [8]float64{p[0], p[1], num(i), num(i + 1), num(i + 2), num(i + 3), num(i + 4), num(i + 5)}
			for s := 1; s <= 8; s++ {
				t := float64(s) / 8
				m := 1 - t
				pts = append(pts, [2]float64{
					m*m*m*c[0] + 3*m*m*t*c[2] + 3*m*t*t*c[4] + t*t*t*c[6],
					m*m*m*c[1] + 3*m*m*t*c[3] + 3*m*t*t*c[5] + t*t*t*c[7],
				})
			}
			i += 6
		case "Z":
			if len(pts) > 0 {
				rings, pts = append(rings, pts), nil
			}
		}
	}
	if len(pts) > 0 {
		rings = append(rings, pts)
	}
	return rings
}

// roundRect is a rounded square: true inside it.
func roundRect(px, py, x, y, w, h, r float64) bool {
	if px < x || py < y || px > x+w || py > y+h {
		return false
	}
	cx := math.Max(x+r, math.Min(px, x+w-r))
	cy := math.Max(y+r, math.Min(py, y+h-r))
	return math.Hypot(px-cx, py-cy) <= r
}
