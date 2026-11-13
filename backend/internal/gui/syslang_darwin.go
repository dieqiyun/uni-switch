package gui

import (
	"os"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// systemLang is the first of the Mac's preferred languages, the one the
// page's navigator.language is; an app opened from Finder has no LANG.
func systemLang() string {
	if out, err := proc.Command("defaults", "read", "-g", "AppleLanguages").Output(); err == nil {
		if l := firstAppleLang(string(out)); l != "" {
			return l
		}
	}
	return envLang(os.Getenv)
}
