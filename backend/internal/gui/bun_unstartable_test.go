package gui

import (
	"encoding/json"

	"os"
	"strings"
	"testing"
)

// What the page says for it is a key i18n.js has in every language: a
// t() of a word it lacks shows the English.
func TestBunSaidIsTranslated(t *testing.T) {
	words, err := os.ReadFile("assets/i18n.js")
	if err != nil {
		t.Fatal(err)
	}
	for why, said := range bunSaid {
		key, _ := json.Marshal(said)
		if n := strings.Count(string(words), string(key)+":"); n != 4 {
			t.Errorf("%s: i18n.js has %q in %d languages, want zh, zh-TW, ja and de", why, said, n)
		}
	}
}
