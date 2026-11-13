package proc

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Node version managers' folders read the same on every system: the
// version folders a manager keeps, highest first, and fnm's Windows layout
// (fnmWin*), which uni-switch's own PATH and FindTool use on Windows and the
// tests take on any system.

// nodeVersions are the names of the version folders under root that have
// a folder bin (root/<version>/<bin>), highest version first (nodeVersionLess); a name that is
// no version (mise's "lts", "latest") after every one that is.
func nodeVersions(root, bin string) []string {
	es, _ := os.ReadDir(root)
	var names []string
	for _, e := range es {
		if st, err := os.Stat(filepath.Join(root, e.Name(), bin)); err == nil && st.IsDir() {
			names = append(names, e.Name())
		}
	}
	slices.SortStableFunc(names, func(a, b string) int {
		switch {
		case nodeVersionLess(b, a):
			return -1
		case nodeVersionLess(a, b):
			return 1
		}
		return strings.Compare(a, b)
	})
	return names
}

// parseNodeVersion reads v22.12.0 (or 22.12.0, mise's, or 22, a partial
// one) as its numbers; false for a name that isn't one.
func parseNodeVersion(s string) ([3]int, int, bool) {
	var v [3]int
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) > 3 || parts[0] == "" {
		return v, 0, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, 0, false
		}
		v[i] = n
	}
	return v, len(parts), true
}

// nodeVersionLess says a is a lower version than b, by number: v9 before
// v22. A name that is no version is lower than one that is.
func nodeVersionLess(a, b string) bool {
	va, _, oka := parseNodeVersion(a)
	vb, _, okb := parseNodeVersion(b)
	if oka != okb {
		return okb
	}
	return oka && slices.Compare(va[:], vb[:]) < 0
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// fnm on Windows (#2685). A version's programs are in its installation
// folder itself — node-versions\<v>\installation\node.exe, npx.cmd — with
// no bin under it as on macOS and Linux, and aliases\default is a junction
// to the default version's installation folder (fnm's symlink_dir is
// junction::create there). A terminal set up with `fnm env` has its own
// link to aliases\default first on PATH, so its node, npx and the npm
// shims beside them are found; uni-switch, started from the Start menu, has
// the registry's PATH only and had none of them. An npx.cmd run by its
// full path started too, but the package it fetched runs `node` from
// PATH, and the server died with cmd's "'node' is not recognized".

// fnmWinRoots are the folders fnm keeps its Node versions in on Windows
// that exist here, the one fnm uses first: FNM_DIR, else its default,
// the data folder's fnm (etcetera's Windows data_dir, home\AppData\Roaming,
// which APPDATA names), else the legacy ~\.fnm.
func fnmWinRoots(home, appdata, fnmDir string) []string {
	var roots []string
	if filepath.IsAbs(fnmDir) {
		roots = append(roots, fnmDir)
	}
	if filepath.IsAbs(appdata) {
		roots = append(roots, filepath.Join(appdata, "fnm"))
	}
	if filepath.IsAbs(home) {
		roots = append(roots, filepath.Join(home, "AppData", "Roaming", "fnm"), filepath.Join(home, ".fnm"))
	}
	var have []string
	for _, r := range roots {
		if isDir(r) && !slices.ContainsFunc(have, func(h string) bool { return strings.EqualFold(h, r) }) {
			have = append(have, r)
		}
	}
	return have
}

// fnmWinBins are the folders of the Node versions fnm keeps in dir on
// Windows: the default alias's (aliases\default, as it stands: a junction
// Stat follows) first, then each installation folder, newest first.
func fnmWinBins(dir string) []string {
	var bins []string
	if def := filepath.Join(dir, "aliases", "default"); isDir(def) {
		bins = append(bins, def)
	}
	root := filepath.Join(dir, "node-versions")
	for _, n := range nodeVersions(root, "installation") {
		bins = append(bins, filepath.Join(root, n, "installation"))
	}
	return bins
}

// fnmWinUse is the folder of fnm's Node in dir a terminal gets on Windows:
// the default alias's; with no default alias, the highest installed. ""
// when the default leads nowhere: fnm's "system" alias points at a folder
// that can't exist (Z:\_fnm_\...), and such a terminal runs the system's
// Node, which PATH has already.
func fnmWinUse(dir string) string {
	def := filepath.Join(dir, "aliases", "default")
	if isDir(def) {
		return def
	}
	if _, err := os.Lstat(def); err == nil {
		return ""
	}
	root := filepath.Join(dir, "node-versions")
	if ns := nodeVersions(root, "installation"); len(ns) > 0 {
		return filepath.Join(root, ns[0], "installation")
	}
	return ""
}

// fnmWinPath is the folder of fnm's Node a terminal gets, in the root fnm
// uses (fnmWinRoots, fnmWinUse); none when fnm isn't here.
func fnmWinPath(home, appdata, fnmDir string) []string {
	if roots := fnmWinRoots(home, appdata, fnmDir); len(roots) > 0 {
		if d := fnmWinUse(roots[0]); d != "" {
			return []string{d}
		}
	}
	return nil
}

// addPath adds the folders PATH lacks, after the ones it has.
func addPath(dirs []string) {
	cur := filepath.SplitList(os.Getenv("PATH"))
	seen := map[string]bool{}
	for _, d := range cur {
		seen[d] = true
	}
	for _, d := range dirs {
		if d != "" && filepath.IsAbs(d) && !seen[d] {
			seen[d] = true
			cur = append(cur, d)
		}
	}
	os.Setenv("PATH", strings.Join(cur, string(os.PathListSeparator)))
}
