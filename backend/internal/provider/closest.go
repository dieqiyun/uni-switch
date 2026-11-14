package provider

// Closest is what a request for a model uni-switch doesn't know is told to ask
// instead (MOMO on Discord: "Zen-Space-Bunny", the model's name as a
// picker shows it, got a 404 that named no id; the one meant was
// opencode-zen-free/space-bunny-free, "Space Bunny Free").

import (
	"slices"
	"strings"
	"unicode"
)

// nameWords are the lowercase runs of letters and of digits in s:
// "Zen-Space-Bunny" is zen, space, bunny; "glm-5.3" is glm, 5, 3.
func nameWords(s string) []string {
	var out []string
	var b strings.Builder
	kind := 0 // 1 letter, 2 digit
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range strings.ToLower(s) {
		k := 0
		switch {
		case unicode.IsLetter(r):
			k = 1
		case unicode.IsDigit(r):
			k = 2
		}
		if k != kind {
			flush()
		}
		kind = k
		if k != 0 {
			b.WriteRune(r)
		}
	}
	flush()
	return out
}

// squashed is s lowercase with only its letters and digits.
func squashed(s string) string { return strings.Join(nameWords(s), "") }

// Closest lists up to n catalog ids nearest to asked, nearest first: a
// model whose display name holds asked's words before one whose id does,
// and the provider's id or name counting only alongside. Nothing when no
// model's name or id shares a word with it.
func Closest(asked string, n int) []string {
	words := nameWords(asked)
	if len(words) == 0 || n <= 0 {
		return nil
	}
	whole := squashed(asked)
	type scored struct {
		id    string
		score int
	}
	var hits []scored
	for _, e := range Catalog() {
		names := slices.Concat(nameWords(e.Name), nameWords(e.Default))
		model := nameWords(e.Model)
		var from []string
		{
			from = slices.Concat(nameWords(e.Provider.ID), nameWords(e.Provider.Name))
		}
		score, own := 0, 0
		for _, w := range words {
			switch {
			case slices.Contains(names, w):
				score += 4
				own++
			case slices.Contains(model, w):
				score += 3
				own++
			case slices.Contains(from, w):
				score++
			}
		}
		if own == 0 {
			continue // a provider's name alone is no match
		}
		if whole != "" && (strings.Contains(squashed(e.Name), whole) || strings.Contains(squashed(e.ID), whole)) {
			score += 4
		}
		hits = append(hits, scored{e.ID, score})
	}
	slices.SortStableFunc(hits, func(a, b scored) int { return b.score - a.score })
	var out []string
	for _, h := range hits {
		if !slices.Contains(out, h.id) {
			out = append(out, h.id)
		}
		if len(out) == n {
			break
		}
	}
	return out
}
