package library

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// rtkWingetLocations is the InstallLocation of each entry winget wrote for
// rtk-ai.rtk among the installed programs (HKCU's for a user install,
// HKLM's for a machine one): its key is rtk-ai.rtk_<source>, with
// WinGetPackageIdentifier rtk-ai.rtk. ok is false when none of them could
// be opened.
func rtkWingetLocations() (locs []string, ok bool) {
	const uninstall = `Software\Microsoft\Windows\CurrentVersion\Uninstall`
	for _, root := range []struct {
		key  registry.Key
		view uint32
	}{
		{registry.CURRENT_USER, 0},
		{registry.LOCAL_MACHINE, registry.WOW64_64KEY},
		{registry.LOCAL_MACHINE, registry.WOW64_32KEY},
	} {
		k, err := registry.OpenKey(root.key, uninstall, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE|root.view)
		if err != nil {
			continue
		}
		ok = true
		names, _ := k.ReadSubKeyNames(-1)
		for _, n := range names {
			sk, err := registry.OpenKey(k, n, registry.QUERY_VALUE|root.view)
			if err != nil {
				continue
			}
			id, _, _ := sk.GetStringValue("WinGetPackageIdentifier")
			if strings.EqualFold(id, "rtk-ai.rtk") {
				if loc, _, _ := sk.GetStringValue("InstallLocation"); loc != "" {
					locs = append(locs, loc)
				}
			}
			sk.Close()
		}
		k.Close()
	}
	return locs, ok
}
