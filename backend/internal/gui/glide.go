package gui

import (
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Glide is how the panel moves to a new height: over MS milliseconds on a
// CSS cubic-bezier, so it keeps pace with what the page animates inside it.
// A zero Glide is a jump.
type Glide struct {
	MS    int
	Curve [4]float64
}

// parseFit reads a fit request: h, and ms and ease ("x1,y1,x2,y2") when the
// panel is to glide there.
func parseFit(q url.Values) (height int, g Glide, ok bool) {
	height, err := strconv.Atoi(q.Get("h"))
	if err != nil {
		return 0, Glide{}, false
	}
	g.MS, _ = strconv.Atoi(q.Get("ms"))
	g.MS = max(0, min(2000, g.MS))
	g.Curve = [4]float64{.25, .1, .25, 1} // CSS ease
	if parts := strings.Split(q.Get("ease"), ","); len(parts) == 4 {
		for i, p := range parts {
			v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				return height, Glide{MS: g.MS, Curve: [4]float64{.25, .1, .25, 1}}, true
			}
			g.Curve[i] = v
		}
		// x of the control points stays within the timeline
		g.Curve[0] = max(0, min(1, g.Curve[0]))
		g.Curve[2] = max(0, min(1, g.Curve[2]))
	}
	return height, g, true
}

// at is how far along the curve is at time t (0…1), as CSS works it out:
// solve x(s) = t for s, then y(s).
func (g Glide) at(t float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	x1, y1, x2, y2 := g.Curve[0], g.Curve[1], g.Curve[2], g.Curve[3]
	bez := func(a, b, s float64) float64 { return 3*a*s*(1-s)*(1-s) + 3*b*s*s*(1-s) + s*s*s }
	lo, hi := 0.0, 1.0
	for range 40 {
		mid := (lo + hi) / 2
		if bez(x1, x2, mid) < t {
			lo = mid
		} else {
			hi = mid
		}
	}
	return bez(y1, y2, (lo+hi)/2)
}

// frames are the heights, one per display frame, a glide from `from` to
// `to` passes through; the last is `to`.
func (g Glide) frames(from, to int) []int {
	n := max(1, int(math.Round(float64(g.MS)/(1000.0/60))))
	out := make([]int, n)
	for i := range n {
		out[i] = from + int(math.Round(float64(to-from)*g.at(float64(i+1)/float64(n))))
	}
	out[n-1] = to
	return out
}

// stepGlide moves a window's height frame by frame where the system can't
// animate it itself; set is called with each height, and stops early once a
// newer glide has begun (gen no longer matches).
func stepGlide(g Glide, from, to int, gen func() bool, set func(int)) {
	if g.MS == 0 || from == to {
		set(to)
		return
	}
	tick := time.NewTicker(time.Second / 60)
	defer tick.Stop()
	for _, h := range g.frames(from, to) {
		if !gen() {
			return
		}
		set(h)
		<-tick.C
	}
}

// ease is the curve as a fit request spells it.
func (g Glide) ease() string {
	s := make([]string, len(g.Curve))
	for i, v := range g.Curve {
		s[i] = strconv.FormatFloat(v, 'g', -1, 64)
	}
	return strings.Join(s, ",")
}

// parseTint reads a tint request: c as "r,g,b,a" (0…255 each) and ms, how
// long the change of colour takes.
func parseTint(q url.Values) (c [4]uint8, ms int, ok bool) {
	parts := strings.Split(q.Get("c"), ",")
	if len(parts) != 4 {
		return c, 0, false
	}
	for i, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || v < 0 || v > 255 {
			return c, 0, false
		}
		c[i] = uint8(v)
	}
	ms, _ = strconv.Atoi(q.Get("ms"))
	return c, max(0, min(2000, ms)), true
}
