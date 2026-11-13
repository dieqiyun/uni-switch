package gui

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// The Dock tile's flap is the header logo's (assets/app.css, .logo.wag), as
// the tray's frames are (build/icon/gen.go): the drawing cut into body,
// tail and wing by index.html's clip paths, each piece turned about the
// same point through the same keyframes over the same .9s,
// cubic-bezier(.3, 0, .3, 1) between each two of them, and the whole bird
// bobbing about its feet. TestDockFlapIsTheLogos holds them to app.css.
const (
	flapSecs  = 0.9
	flapEvery = 0.03
)

// A keyframe: at is the fraction of the animation, deg the rotation.
type flapKey struct{ at, deg float64 }

var (
	wingKeys = []flapKey{{0, 0}, {.12, 10}, {.26, -3}, {.40, 8}, {.56, -2}, {.72, 3}, {1, 0}}
	tailKeys = []flapKey{{0, 0}, {.18, 16}, {.36, -7}, {.54, 9}, {.72, -3}, {.86, 2}, {1, 0}}
	bobKeys  = []flapKey{{0, 0}, {.18, -4}, {.40, 2}, {.62, -1}, {1, 0}}
	// transform-origin, in the 44-unit viewBox
	wingAt, tailAt, bobAt = [2]float64{30, 19.8}, [2]float64{14, 22}, [2]float64{26, 38}
)

// flapAt is the bob, tail and wing turns at t, 0..1 of the flap.
func flapAt(t float64) (bob, tail, wing float64) {
	return keyed(bobKeys, t), keyed(tailKeys, t), keyed(wingKeys, t)
}

// keyed is the rotation at t of keyframes played as CSS plays them, each
// step eased by cubic-bezier(.3, 0, .3, 1).
func keyed(ks []flapKey, t float64) float64 {
	for i := 1; i < len(ks); i++ {
		if t <= ks[i].at {
			a, b := ks[i-1], ks[i]
			return a.deg + (b.deg-a.deg)*flapEase((t-a.at)/(b.at-a.at))
		}
	}
	return ks[len(ks)-1].deg
}

// flapEase is cubic-bezier(.3, 0, .3, 1): x found for the curve's parameter
// by bisection, and its y.
func flapEase(x float64) float64 {
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

// A path for the Dock's drawing: ops are 'M', 'L', 'C' and 'Z', pts the
// points they take (one each, three for C), in the viewBox.
type flapPath struct {
	ops []byte
	pts []float64
}

// logoPaths reads the header logo's bird and its three clip paths (body,
// tail, wing) out of index.html, so the Dock cuts the bird where the logo
// does. ok is false when one is missing or has a command it doesn't read.
func logoPaths(page string) (bird flapPath, clips [3]flapPath, ok bool) {
	d := func(re string) (flapPath, bool) {
		m := regexp.MustCompile(re).FindStringSubmatch(page)
		if m == nil {
			return flapPath{}, false
		}
		return parseFlapPath(m[1])
	}
	var good [4]bool
	bird, good[0] = d(`<path id="bird"[^>]* d="([^"]+)"`)
	for i, id := range []string{"bird-body", "bird-tail", "bird-wing"} {
		clips[i], good[i+1] = d(`<clipPath id="` + id + `"><path[^>]* d="([^"]+)"`)
	}
	return bird, clips, good[0] && good[1] && good[2] && good[3]
}

// parseFlapPath reads absolute M, L, H, V, C and Z, H and V written as L.
func parseFlapPath(s string) (flapPath, bool) {
	var p flapPath
	toks := regexp.MustCompile(`[MLHVCZ]|-?(?:\d+\.?\d*|\.\d+)`).FindAllString(s, -1)
	if strings.TrimSpace(regexp.MustCompile(`[MLHVCZ]|-?(?:\d+\.?\d*|\.\d+)|[\s,]`).ReplaceAllString(s, "")) != "" {
		return p, false
	}
	var cmd byte
	var x, y float64
	num := func(i *int) (float64, bool) {
		if *i >= len(toks) {
			return 0, false
		}
		v, err := strconv.ParseFloat(toks[*i], 64)
		*i++
		return v, err == nil && !math.IsNaN(v)
	}
	for i := 0; i < len(toks); {
		if c := toks[i][0]; c >= 'A' && c <= 'Z' {
			cmd = c
			i++
			if cmd == 'Z' {
				p.ops = append(p.ops, 'Z')
				continue
			}
		} else if cmd == 0 || cmd == 'Z' {
			return p, false
		}
		n := map[byte]int{'M': 2, 'L': 2, 'H': 1, 'V': 1, 'C': 6}[cmd]
		v := make([]float64, n)
		for j := range v {
			var good bool
			if v[j], good = num(&i); !good {
				return p, false
			}
		}
		switch cmd {
		case 'M', 'L':
			x, y = v[0], v[1]
			p.ops = append(p.ops, cmd)
			p.pts = append(p.pts, x, y)
			if cmd == 'M' {
				cmd = 'L' // more pairs after M are lines
			}
		case 'H', 'V':
			if cmd == 'H' {
				x = v[0]
			} else {
				y = v[0]
			}
			p.ops = append(p.ops, 'L')
			p.pts = append(p.pts, x, y)
		case 'C':
			x, y = v[4], v[5]
			p.ops = append(p.ops, 'C')
			p.pts = append(p.pts, v...)
		}
	}
	return p, len(p.ops) > 0
}
