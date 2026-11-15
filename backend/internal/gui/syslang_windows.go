package gui

import "golang.org/x/sys/windows"

// systemLang is the user's first display language, the one WebView2 hands
// the page as navigator.language.
func systemLang() string {
	if l, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME); err == nil && len(l) > 0 {
		return l[0]
	}
	return ""
}
