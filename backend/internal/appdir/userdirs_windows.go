package appdir

import "golang.org/x/sys/windows"

var knownFolderIDs = map[string]*windows.KNOWNFOLDERID{
	DownloadsFolder: windows.FOLDERID_Downloads,
	DesktopFolder:   windows.FOLDERID_Desktop,
	DocumentsFolder: windows.FOLDERID_Documents,
}

// osKnownFolder is SHGetKnownFolderPath of kind and of the profile folder,
// made or not (KF_FLAG_DONT_VERIFY: without it a missing folder is an
// error; a caller that needs it there checks). Windows expands the
// %USERPROFILE% in it from the environment of a process's first call.
func osKnownFolder(kind string) (path, profile string) {
	id := knownFolderIDs[kind]
	if id == nil {
		return "", ""
	}
	p, err := windows.KnownFolderPath(id, windows.KF_FLAG_DONT_VERIFY)
	if err != nil {
		return "", ""
	}
	pr, err := windows.KnownFolderPath(windows.FOLDERID_Profile, windows.KF_FLAG_DONT_VERIFY)
	if err != nil {
		return "", ""
	}
	return p, pr
}
