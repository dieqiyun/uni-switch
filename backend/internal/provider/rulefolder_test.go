package provider

import (
	"testing"
)

func folderEnv(t *testing.T, fold bool, home string) {
	t.Helper()
	oldFold, oldHome := foldCase, ruleHome
	foldCase, ruleHome = fold, func() string { return home }
	t.Cleanup(func() { foldCase, ruleHome = oldFold, oldHome })
}

// A folder is under a rule's folder at a path's boundary only, ~ is the
// home folder, a Windows drive is one with its WSL mount and a WSL
// distro's share with the distro's own path, and case counts only where
// the system's folders have it.
func TestFolderUnder(t *testing.T) {
	folderEnv(t, false, "/home/me")
	for _, tc := range []struct {
		folder, root string
		want         bool
	}{
		{"/a/b", "/a/b", true},
		{"/a/b/c", "/a/b", true},
		{"/a/b/", "/a/b", true},
		{"/a/b", "/a/b/", true},
		{"/a/bc", "/a/b", false},
		{"/a", "/a/b", false},
		{"/a/b/../x", "/a/b", false},
		{"/A/B", "/a/b", false}, // Linux: case counts
		{"/home/me/work/acme", "~/work", true},
		{"/home/me/work", "~", true},
		{"/home/mex", "~", false},
		{"/home/me/x", "~/", true},
		{`C:\Users\me\work\acme`, `C:\Users\me\work`, true},
		{`c:\users\ME\work\acme`, `C:\Users\me\work`, true}, // a drive: no case, from either side
		{"/mnt/c/Users/me/work/acme", `C:\Users\me\work`, true},
		{`C:\Users\me\work`, "/mnt/c/users/me/work", true},
		{"/mnt/c/Users/me/workx", `C:\Users\me\work`, false},
		{"/home/me/proj", `\\wsl$\Ubuntu\home\me`, true},
		{"/home/me/proj", `\\wsl.localhost\Ubuntu-24.04\home\me\proj`, true},
		{"/x", "/", true},
		{"", "/", false}, // the agent didn't say: no rule matches it
		{"", "~", false},
		{"relative/x", "/", false},
	} {
		if got := FolderUnder(tc.folder, tc.root); got != tc.want {
			t.Errorf("FolderUnder(%q, %q) = %v, want %v", tc.folder, tc.root, got, tc.want)
		}
	}
	folderEnv(t, true, "/Users/Me")
	for _, tc := range []struct {
		folder, root string
		want         bool
	}{
		{"/users/me/Work/Acme", "~/work", true}, // macOS, Windows: no case
		{"/Users/Me/Workshop", "~/work", false},
	} {
		if got := FolderUnder(tc.folder, tc.root); got != tc.want {
			t.Errorf("fold: FolderUnder(%q, %q) = %v, want %v", tc.folder, tc.root, got, tc.want)
		}
	}
	folderEnv(t, true, "")
	if FolderUnder("/x/y", "~") {
		t.Error("~ with no home folder known matched")
	}
}
