package gui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestDatesFollowGUILanguage: every date, time and number the GUI formats
// is given intlLang(), the GUI's language. With no locale, [] or undefined
// it follows the system instead, and a Chinese Mac set to English read
// Routing's days as "10月11日 周日" (#1843). The browser test
// dates-gui-lang.test.cjs checks what Routing shows; this keeps every other
// call from slipping back, since CI doesn't run the browser tests.
func TestDatesFollowGUILanguage(t *testing.T) {
	bare := regexp.MustCompile(`\.toLocale(Date|Time)?String\(\s*(\)|\[\]|undefined\b|document\.documentElement\.lang)|new Intl\.\w+\(\s*(\)|\[\]|undefined\b)|intlLang\(\) \|\|`)
	files, err := filepath.Glob(filepath.Join("assets", "*.js"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no assets: %v", err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if bare.MatchString(line) {
				t.Errorf("%s:%d formats in the system's locale, not the GUI's: give it intlLang()\n\t%s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}
