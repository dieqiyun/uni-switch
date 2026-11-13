package update

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// shellProfiles are PowerShell 7's and Windows PowerShell's profiles, each
// only where its execution policy runs a profile: Windows PowerShell's is
// Restricted until the user changes it, and a profile it may not run
// would put an error in each terminal it opens. cmd has no profile.
func shellProfiles() []ShellProfile {
	docs := documents()
	if docs == "" {
		return nil
	}
	gpo := policy(registry.LOCAL_MACHINE, `SOFTWARE\Policies\Microsoft\Windows\PowerShell`)
	var out []ShellProfile
	seven := filepath.Join(docs, "PowerShell")
	if _, err := os.Stat(seven); err == nil || pwshInstalled() {
		// RemoteSigned unless a policy says otherwise
		if gpo == "" || runsProfile(gpo) {
			out = append(out, ShellProfile{Kind: "powershell", Path: filepath.Join(seven, "Microsoft.PowerShell_profile.ps1"), Create: true})
		}
	}
	p := gpo
	if p == "" {
		p = policy(registry.CURRENT_USER, `SOFTWARE\Microsoft\PowerShell\1\ShellIds\Microsoft.PowerShell`)
	}
	if p == "" {
		p = policy(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\PowerShell\1\ShellIds\Microsoft.PowerShell`)
	}
	if runsProfile(p) {
		out = append(out, ShellProfile{Kind: "powershell", Path: filepath.Join(docs, "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1"), Create: true})
	}
	return out
}

func pwshInstalled() bool {
	_, err := proc.Look("pwsh")
	return err == nil
}

// runsProfile says a policy lets a profile written here run.
func runsProfile(p string) bool {
	switch strings.ToLower(p) {
	case "remotesigned", "unrestricted", "bypass":
		return true
	}
	return false
}

// policy is the ExecutionPolicy a registry key sets, "" none.
func policy(root registry.Key, path string) string {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	if path == `SOFTWARE\Policies\Microsoft\Windows\PowerShell` {
		if on, _, err := k.GetIntegerValue("EnableScripts"); err == nil && on == 0 {
			return "Restricted"
		}
	}
	v, _, _ := k.GetStringValue("ExecutionPolicy")
	return v
}

// documents is the user's Documents folder, where PowerShell's profiles
// are; under a home that isn't the user's own (a test's), Documents in it.
func documents() string {
	home := os.Getenv("USERPROFILE")
	if home == "" {
		return ""
	}
	own, _ := windows.KnownFolderPath(windows.FOLDERID_Profile, 0)
	if own == "" || !strings.EqualFold(filepath.Clean(own), filepath.Clean(home)) {
		return filepath.Join(home, "Documents")
	}
	if d, err := windows.KnownFolderPath(windows.FOLDERID_Documents, 0); err == nil && d != "" {
		return d
	}
	return filepath.Join(home, "Documents")
}
