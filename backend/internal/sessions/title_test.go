package sessions

import (
	"math/rand"
	"regexp"
	"strings"
	"testing"
)

// oldTitle is title as it was, through a regexp and runes of the whole.
func oldTitle(s string) string {
	s = strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(s, " "))
	if r := []rune(s); len(r) > 160 {
		s = strings.TrimSpace(string(r[:160])) + "…"
	}
	return s
}

func TestTitleAsBefore(t *testing.T) {
	cases := []string{
		"", " ", "\t\n", "hi", "  a  b\t\tc \n", "\v x \v", " x ", " \v x",
		strings.Repeat("a", 159), strings.Repeat("a", 160), strings.Repeat("a", 161),
		strings.Repeat("a", 160) + " ", strings.Repeat("a", 160) + "\v  \n",
		strings.Repeat("a", 159) + "  b", strings.Repeat("a", 159) + "\vb",
		strings.Repeat("字", 200), strings.Repeat("a ", 100), "\xff\xfe" + strings.Repeat("b", 170),
		"ok\xff", strings.Repeat("\xff", 161), strings.Repeat("x ", 90), "\u0085a\u0085",
	}
	alphabet := []string{"a", "字", " ", "\t", "\n", "\r", "\f", "\v", " ", "\u0085", " ", "\xff", "é", "  "}
	rng := rand.New(rand.NewSource(1))
	for range 20000 {
		var b strings.Builder
		for n := rng.Intn(400); n > 0; n-- {
			b.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		cases = append(cases, b.String())
	}
	for _, c := range cases {
		if got, want := title(c), oldTitle(c); got != want {
			t.Fatalf("title(%q) = %q, was %q", c, got, want)
		}
	}
}

// A pasted file as the prompt is read only as far as its title.
func TestTitleOfALongPromptAllocatesItsTitle(t *testing.T) {
	long := strings.Repeat("word  \n", 1<<17) // ~900 KB
	if allocs := testing.AllocsPerRun(20, func() { title(long) }); allocs > 8 {
		t.Fatalf("%v allocations", allocs)
	}
	var sink string
	n := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink = title(long)
		}
	})
	_ = sink
	if per := n.AllocedBytesPerOp(); per > 4<<10 {
		t.Fatalf("a %d KB prompt's title allocated %d KB", len(long)>>10, per>>10)
	}
}
