package cli

import (
	"testing"
)

func TestParseTokens(t *testing.T) {
	for in, want := range map[string]int{"200000": 200000, "200k": 200000, "200K": 200000, "1m": 1000000, "1.5m": 1500000, "128_000": 128000, " 32k ": 32000} {
		if got, err := parseTokens(in); err != nil || got != want {
			t.Errorf("%q: %d %v", in, got, err)
		}
	}
	for _, in := range []string{"", "k", "lots", "-5", "1g", "nan", "inf", "1e20"} {
		if _, err := parseTokens(in); err == nil {
			t.Errorf("%q read", in)
		}
	}
}
