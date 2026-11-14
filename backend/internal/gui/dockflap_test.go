package gui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestDockFlapIsTheLogos holds the Dock tile's flap to the header logo's:
// the same keyframes, turn points, length and easing as app.css plays,
// and the bird and clip paths read from index.html.
func TestDockFlapIsTheLogos(t *testing.T) {
	css, err := assets.ReadFile("assets/app.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, part string
		keys       []flapKey
		at         [2]float64
	}{{"wing-flap", "wing", wingKeys, wingAt}, {"tail-flick", "tail", tailKeys, tailAt}, {"bird-bob", "bird", bobKeys, bobAt}} {
		body := regexp.MustCompile(`@keyframes ` + c.name + ` \{\n((?:  .*\n)+)\}`).FindSubmatch(css)
		if body == nil {
			t.Fatalf("no @keyframes %s in app.css", c.name)
		}
		got := []flapKey{{0, 0}}
		for _, m := range regexp.MustCompile(`(?m)^  (\d+)% \{ transform: rotate\((-?\d+)deg\); \}$`).FindAllSubmatch(body[1], -1) {
			at, _ := strconv.ParseFloat(string(m[1]), 64)
			deg, _ := strconv.ParseFloat(string(m[2]), 64)
			got = append(got, flapKey{at / 100, deg})
		}
		got = append(got, flapKey{1, 0})
		if fmt.Sprint(got) != fmt.Sprint(c.keys) {
			t.Errorf("%s: app.css has %v, the Dock plays %v", c.name, got, c.keys)
		}
		origin := fmt.Sprintf(`.logo .%s { transform-box: view-box; transform-origin: %vpx %vpx; }`, c.part, c.at[0], c.at[1])
		if !regexp.MustCompile(regexp.QuoteMeta(origin)).Match(css) {
			t.Errorf("app.css has no %q", origin)
		}
		anim := fmt.Sprintf(`.logo.wag .%s { animation: %s %ss cubic-bezier(.3, 0, .3, 1); }`, c.part, c.name, strings.TrimPrefix(fmt.Sprint(flapSecs), "0"))
		if !regexp.MustCompile(regexp.QuoteMeta(anim)).Match(css) {
			t.Errorf("app.css has no %q", anim)
		}
	}
	for x, prev := 0.0, -1.0; x <= 1; x += .01 {
		if e := flapEase(x); e < prev || e < -1e-9 || e > 1+1e-9 {
			t.Fatalf("the easing at %v is %v, after %v", x, e, prev)
		} else {
			prev = e
		}
	}
	page, err := assets.ReadFile("assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	bird, clips, ok := logoPaths(string(page))
	if !ok {
		t.Fatal("index.html's bird or a clip path didn't parse")
	}
	if len(bird.ops) < 100 || bird.ops[0] != 'M' {
		t.Errorf("the bird read as %d ops", len(bird.ops))
	}
	for i, c := range clips {
		if len(c.ops) < 4 {
			t.Errorf("clip %d read as %v", i, c.ops)
		}
	}
	if _, ok := parseFlapPath("M1 2Q3 4 5 6"); ok {
		t.Error("a path with Q read as good")
	}
}
