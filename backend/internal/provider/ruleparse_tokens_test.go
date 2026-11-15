package provider

import "testing"

func TestParseTokensRejectsNonFinite(t *testing.T) {
	for _, in := range []string{
		"nan", "NaN", "inf", "+inf", "1e20", "1e308", "1e20k",
		"9223372036854775808", "9223372036854775k",
		"9223372036854775807", // float64 rounds this to 2^63 on amd64
	} {
		if n, err := ParseTokens(in); err == nil {
			t.Errorf("%q accepted as %d", in, n)
		}
	}
}
