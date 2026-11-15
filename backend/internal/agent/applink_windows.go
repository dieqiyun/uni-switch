//go:build windows

package agent

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// systemLinkHandler reads the program Windows runs for scheme's links,
// HKEY_CLASSES_ROOT\<scheme>\shell\open\command, which is what Electron's
// setAsDefaultProtocolClient writes (Cindy, AionUi) for the user, and
// installers for the user or the machine. Only a scheme with no key of its
// own and no default app the user chose (UserChoice, as Settings' Default
// apps keeps one) opens nothing; a key with no command of its own is
// handed to another (a packaged app's), so it opens something unknown.
func systemLinkHandler(scheme string) (string, bool, error) {
	k, err := registry.OpenKey(registry.CLASSES_ROOT, scheme, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return userChoice(scheme)
	}
	if err != nil {
		return "", false, err
	}
	k.Close()
	k, err = registry.OpenKey(registry.CLASSES_ROOT, scheme+`\shell\open\command`, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return "", true, nil
	}
	if err != nil {
		return "", false, err
	}
	defer k.Close()
	v, typ, err := k.GetStringValue("")
	if errors.Is(err, registry.ErrNotExist) {
		return "", true, nil
	}
	if err != nil {
		return "", false, err
	}
	if typ == registry.EXPAND_SZ {
		if x, err := registry.ExpandString(v); err == nil {
			v = x
		}
	}
	return commandProgram(v), true, nil
}

// userChoice: the app the user picked for scheme in Default apps, which
// needs no key of the scheme's own.
func userChoice(scheme string) (string, bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\Shell\Associations\UrlAssociations\`+scheme+`\UserChoice`, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer k.Close()
	id, _, err := k.GetStringValue("ProgId")
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return "", id != "", nil
}
