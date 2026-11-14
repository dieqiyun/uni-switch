package agent

import (
	"testing"
)

// The thinking mode follows the Gemini generation whatever the provider
// or plugin prefix, and a model of a later or unnamed generation thinks by
// level, as Gemini 3 does.
func TestOmpGeminiMode(t *testing.T) {
	for id, want := range map[string]string{
		"gem/gemini-3-flash":            "google-level",
		"google-plugin/gemini-3.1-pro":  "google-level",
		"codeassist/Gemini-2.5-Pro":     "budget",
		"gem/gemini-2.0-flash":          "budget",
		"gem/gemini-1.5-pro":            "budget",
		"gemini-4-ultra":                "google-level",
		"relay/vendor/gemini-2.5-flash": "budget",
	} {
		if got := ompGeminiMode(id); got != want {
			t.Errorf("%s: %s, want %s", id, got, want)
		}
	}
}
