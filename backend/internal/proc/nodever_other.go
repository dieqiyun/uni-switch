//go:build !windows

package proc

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// A Node version manager keeps one folder per version, and a terminal runs
// the one it was told to use, not the one whose name sorts last. uni-switch
// picked nvm's "newest-looking" folder by name, and in plain string order
// v9.11.2 comes after v22.12.0: a codex, claude or gemini installed under a
// Node the terminal doesn't use was found, run and updated in place of the
// terminal's (found while answering 悠悠哥 on Discord, 9f407cafa). The
// folders are taken in the order a terminal would reach them instead: the
// manager's default first, then the highest version.

// nvmDir is nvm's folder: $NVM_DIR as the profile sets it, else ~/.nvm.
func nvmDir(home string) string {
	if d := os.Getenv("NVM_DIR"); filepath.IsAbs(d) {
		return d
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".nvm")
}

// nvmBins are the bin folders of the Node versions nvm keeps in dir, the
// one a terminal gets first (nvmDefault), then the rest newest first.
// system is true when nvm's default is the system's Node, which a
// terminal then runs, and none of these.
func nvmBins(dir string) (bins []string, def string, system bool) {
	root := filepath.Join(dir, "versions", "node")
	names := nodeVersions(root, "bin")
	def, system = nvmDefault(dir, names)
	for _, n := range names {
		bins = append(bins, filepath.Join(root, n, "bin"))
	}
	if def != "" {
		def = filepath.Join(root, def, "bin")
		bins = slices.DeleteFunc(bins, func(b string) bool { return b == def })
		bins = append([]string{def}, bins...)
	}
	return bins, def, system
}

// nvmDefault is the installed version (one of names, highest first) nvm's
// default alias resolves to, as nvm.sh does when a shell loads it
// (nvm_auto use → nvm_resolve_local_alias default): alias/default, then
// each alias it names in turn (lts/* → lts/jod → v22.20.0, node, stable),
// a cycle being none; "node" and "stable" the highest installed; a partial
// version (22, v22.1) the highest installed that it starts; a full one
// itself when it is installed (its bin/node there). "" when the default
// isn't set or names nothing installed (nvm then puts no Node on PATH), or
// is the system's Node, which system says.
func nvmDefault(dir string, names []string) (string, bool) {
	alias := filepath.Join(dir, "alias")
	cur, seen := "default", map[string]bool{"default": true}
	for {
		next := nvmAlias(alias, cur)
		if next == "" {
			break
		}
		if seen[next] {
			return "", false // nvm's ∞
		}
		seen[next] = true
		cur = next
	}
	if cur == "default" {
		return "", false
	}
	installed := func(n string) bool {
		st, err := os.Stat(filepath.Join(dir, "versions", "node", n, "bin", "node"))
		return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
	}
	switch cur {
	case "system":
		return "", true
	case "node", "stable":
		for _, n := range names {
			if _, _, ok := parseNodeVersion(n); ok && strings.HasPrefix(n, "v") {
				return n, false
			}
		}
		return "", false
	}
	// nvm_ensure_version_prefix: 22 → v22
	if cur[0] >= '0' && cur[0] <= '9' {
		cur = "v" + cur
	}
	if _, parts, ok := parseNodeVersion(cur); !ok || !strings.HasPrefix(cur, "v") {
		return "", false // iojs, unstable, N/A, ∞: no Node of versions/node
	} else if parts == 3 {
		if installed(cur) {
			return cur, false
		}
		return "", false
	}
	// nvm_ls v22 matches v22.*, v22.1 matches v22.1.*, newest last
	for _, n := range names {
		if strings.HasPrefix(n, strings.TrimSuffix(cur, ".")+".") {
			return n, false
		}
	}
	return "", false
}

// nvmAlias is what nvm's alias name holds, nvm_alias: the first line of
// alias/<name> with something on it, after nvm_normalize_lts turns
// lts/-1 into the LTS line before the newest. "" when there is no such
// alias.
func nvmAlias(alias, name string) string {
	if n, ok := strings.CutPrefix(name, "lts/-"); ok {
		k, err := strconv.Atoi(n)
		if err != nil || k < 1 {
			return ""
		}
		es, _ := os.ReadDir(filepath.Join(alias, "lts"))
		var ls []string
		for _, e := range es {
			ls = append(ls, e.Name())
		}
		slices.Sort(ls) // `ls`, whose order nvm takes
		i := len(ls) - (k + 1)
		if i < 0 {
			i = 0
		}
		if len(ls) == 0 || ls[i] == "*" {
			return ""
		}
		name = "lts/" + ls[i]
	}
	if name == "" || strings.Contains(name, "..") {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(alias, filepath.FromSlash(name)))
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

// fnmBins are the bin folders of the Node versions fnm keeps in dir, the
// default alias's (aliases/default, a link to the version's folder) first,
// then the rest newest first.
func fnmBins(dir string) []string {
	root := filepath.Join(dir, "node-versions")
	var bins []string
	for _, n := range nodeVersions(root, filepath.Join("installation", "bin")) {
		bins = append(bins, filepath.Join(root, n, "installation", "bin"))
	}
	if to, err := filepath.EvalSymlinks(filepath.Join(dir, "aliases", "default")); err == nil {
		if real, err := filepath.EvalSymlinks(root); err == nil {
			if rel, err := filepath.Rel(real, to); err == nil && !strings.HasPrefix(rel, "..") {
				def := filepath.Join(root, rel, "bin")
				if isDir(def) {
					bins = slices.DeleteFunc(bins, func(b string) bool { return b == def })
					bins = append([]string{def}, bins...)
				}
			}
		}
	}
	return bins
}

// nodeBins are the bin folders of a manager that keeps each version as
// root/<version>/bin (mise's installs), newest first.
func nodeBins(root string) []string {
	var bins []string
	for _, n := range nodeVersions(root, "bin") {
		bins = append(bins, filepath.Join(root, n, "bin"))
	}
	return bins
}
