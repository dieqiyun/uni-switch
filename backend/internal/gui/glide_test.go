package gui

import (
	"math"
	"net/url"
	"testing"
)

func TestParseFit(t *testing.T) {
	h, g, ok := parseFit(url.Values{"h": {"480"}, "ms": {"620"}, "ease": {".22,1,.36,1"}})
	if !ok || h != 480 || g.MS != 620 || g.Curve != [4]float64{.22, 1, .36, 1} {
		t.Fatalf("parsed %d %+v %v", h, g, ok)
	}
	if _, g, _ := parseFit(url.Values{"h": {"300"}}); g.MS != 0 {
		t.Errorf("a bare h glides: %+v", g)
	}
	if _, _, ok := parseFit(url.Values{"h": {"x"}}); ok {
		t.Error("a height that isn't a number read as one")
	}
	if _, g, _ := parseFit(url.Values{"h": {"300"}, "ms": {"99999"}, "ease": {"2,0,-1,1"}}); g.MS != 2000 || g.Curve[0] != 1 || g.Curve[2] != 0 {
		t.Errorf("out of range kept: %+v", g)
	}
	if got := (Glide{Curve: [4]float64{.22, 1, .36, 1}}).ease(); got != "0.22,1,0.36,1" {
		t.Errorf("ease %q", got)
	}
}

// The glide keeps the page's pace: linear is linear, an ease-out is ahead of
// it early on, and the frames end exactly on the height asked for.
func TestGlideCurve(t *testing.T) {
	lin := Glide{Curve: [4]float64{0, 0, 1, 1}}
	for _, x := range []float64{.1, .5, .9} {
		if math.Abs(lin.at(x)-x) > 1e-6 {
			t.Errorf("linear at %v = %v", x, lin.at(x))
		}
	}
	out := Glide{MS: 620, Curve: [4]float64{.22, 1, .36, 1}}
	if out.at(.3) < .6 {
		t.Errorf("ease-out lags at .3: %v", out.at(.3))
	}
	f := out.frames(300, 420)
	if len(f) != 37 || f[len(f)-1] != 420 {
		t.Fatalf("frames %d, last %d", len(f), f[len(f)-1])
	}
	for i := 1; i < len(f); i++ {
		if f[i] < f[i-1] {
			t.Fatalf("frames go back at %d: %v", i, f)
		}
	}
}

func TestParseTint(t *testing.T) {
	c, ms, ok := parseTint(url.Values{"c": {"27, 28, 32, 255"}, "ms": {"450"}})
	if !ok || c != [4]uint8{27, 28, 32, 255} || ms != 450 {
		t.Fatalf("got %v %d %v", c, ms, ok)
	}
	if _, ms, _ := parseTint(url.Values{"c": {"0,0,0,255"}, "ms": {"99999"}}); ms != 2000 {
		t.Errorf("ms not capped: %d", ms)
	}
	for _, bad := range []string{"", "1,2,3", "1,2,3,256", "1,2,x,4", "-1,0,0,0"} {
		if _, _, ok := parseTint(url.Values{"c": {bad}}); ok {
			t.Errorf("%q read as a colour", bad)
		}
	}
}
