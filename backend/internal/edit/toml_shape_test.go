package edit

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2/unstable"
)

// tomlShaper finds every key and comment where the parser's own Shape does.
func TestTOMLShaperMatchesParser(t *testing.T) {
	doc := "# top\na = 1 # one\n\n[t]\n  b.c = \"x#y\"  # two\n\"q\" = [\n  1,\n]\n[[arr]]\nz = { k = 1 }\n"
	lines := splitLines(doc)
	shapeOf := tomlShaper(lines)
	p := unstable.Parser{KeepComments: true}
	p.Reset([]byte(strings.Join(lines, "\n")))
	n := 0
	check := func(it *unstable.Node) {
		if it.Raw.Length == 0 {
			return
		}
		if got, want := shapeOf(it.Raw), p.Shape(it.Raw); got != want {
			t.Errorf("%v %q: %+v, the parser says %+v", it.Kind, p.Raw(it.Raw), got, want)
		}
		n++
	}
	for p.NextExpression() {
		e := p.Expression()
		check(e)
		if e.Kind != unstable.Comment {
			for k := e.Key(); k.Next(); {
				check(k.Node())
			}
		}
		if c := e.Next(); c != nil {
			check(c)
		}
	}
	if err := p.Error(); err != nil || n < 8 {
		t.Fatalf("parsed %d nodes: %v", n, err)
	}
}

// tomlBigFile is a config.toml of about size bytes: the table shape a Codex
// one reached, 1.5 MB of keys, each on its own header.
func tomlBigFile(size int) []string {
	var b strings.Builder
	for i := 0; b.Len() < size; i++ {
		fmt.Fprintf(&b, "[projects.\"/Users/me/p%d\"]\ntrust_level = \"trusted\" # note\n\n", i)
	}
	b.WriteString("model_provider = \"uni-switch\"\n")
	return splitLines("model_provider = \"x\"\n" + b.String())
}

// parseTOMLTimed parses lines five times and keeps the fastest, which a
// loaded runner can still reach, and the file it read.
func parseTOMLTimed(t *testing.T, lines []string) (tomlFile, time.Duration) {
	t.Helper()
	var f tomlFile
	var best time.Duration
	for rep := 0; rep < 5; rep++ {
		start := time.Now()
		got, err := parseTOMLFile(lines)
		if err != nil {
			t.Fatal(err)
		}
		if d := time.Since(start); rep == 0 || d < best {
			best = d
		}
		f = got
	}
	return f, best
}

// A big config.toml is parsed in a time that grows with its size, not its
// square: Codex's own reached 1.5 MB, and each key's line was counted from
// the top of the file, a third of a second at every uni-switch start.
//
// The check is the ratio between parsing a 1 MB and an 8 MB file, not a bound
// in seconds: a runner at load ~190 took the still-linear 3 MB parse past the
// old 1s bound and went red without anything quadratic (#2219). Load slows
// both parses, if not always alike. A parse that grows with size squared
// needs 64× the time for the 8× bigger file, where the linear one needs 8×,
// so the bound of 24 leaves about 3× on both sides: at 2 MB and 8 MB with a
// bound of 8 (2× each side), mote at load 250 went red 1 run in 13.
func TestTOMLBigFileParsesInLinearTime(t *testing.T) {
	small, large := tomlBigFile(1<<20), tomlBigFile(8<<20)
	f, dSmall := parseTOMLTimed(t, small)
	if len(f.tables) < 10000 || f.root.keys[0].name != "model_provider" {
		t.Fatalf("parsed %d tables, root %+v", len(f.tables), f.root.keys)
	}
	last := f.tables[len(f.tables)-1]
	if k := last.keys[len(last.keys)-1]; k.name != "model_provider" || k.from != len(small)-2 && k.from != len(small)-1 {
		t.Errorf("last key %+v of %d lines", k, len(small))
	}
	_, dLarge := parseTOMLTimed(t, large)
	if ratio := float64(dLarge) / float64(dSmall); ratio > 24 {
		t.Errorf("an 8 MB file took %v to parse, %.1f× the 1 MB one's %v: near the 64× of size squared, not the 8× of size", dLarge, ratio, dSmall)
	}
}
