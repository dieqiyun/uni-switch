package library

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Two skills from different places can go by one name: anthropics/skills'
// skill-creator and openai/skills' are not the same skill (mora1n on
// Discord). The library keeps each under a name of its own, since that
// name is its folder here and in every agent's skills folder: the first
// keeps the name its SKILL.md gives, and one installed after it goes in
// beside it under that name with its source's owner added
// (skill-creator-openai). Called keeps the name its SKILL.md gave.

// hadSkill is the library's skill that is the one at from already: one
// installed from that very place, whatever its name here, or one by its
// name with no source to tell it by (taken in from an agent's folder) and
// the very same files.
func (l *Library) hadSkill(src Source, c Candidate, from string) *Skill {
	for _, s := range l.Skills {
		if fromSameSource(s, src, c.Path, from) {
			return s
		}
	}
	if s := l.skill(c.Name); s != nil && s.Source == nil && sameTree(skillDir(s.Name), from) {
		return s
	}
	return nil
}

// skillNameTaken is whether the library has a skill called name, or its
// folder has something by that name. One that can't be looked at is
// taken: what is there is unknown.
func (l *Library) skillNameTaken(name string) bool {
	if l.skill(name) != nil {
		return true
	}
	_, err := os.Lstat(skillDir(name))
	return !errors.Is(err, fs.ErrNotExist)
}

// freeSkillName is the name a skill called name from src goes by in the
// library: name itself while nothing has it, else name with the source's
// owner, then its repository's (or folder's) name, then a number added.
func (l *Library) freeSkillName(name string, src Source) string {
	if !l.skillNameTaken(name) {
		return name
	}
	owner, repo := sourceParts(src)
	var tries []string
	if owner != "" {
		tries = append(tries, owner, owner+"-"+repo)
	}
	if repo != "" {
		tries = append(tries, repo)
	}
	for _, s := range tries {
		if n := withSuffix(name, s); n != "" && n != name && !l.skillNameTaken(n) {
			return n
		}
	}
	base := owner
	if base == "" {
		base = repo
	}
	for i := 2; ; i++ {
		suffix := fmt.Sprint(i)
		if base != "" {
			suffix = base + "-" + suffix
		}
		if n := withSuffix(name, suffix); n != "" && !l.skillNameTaken(n) {
			return n
		}
	}
}

// sourceParts is a source's owner and repository: openai and skills for
// openai/skills or https://gitlab.com/openai/skills.git, and for a folder
// the folder it is in and its own name.
func sourceParts(src Source) (owner, repo string) {
	var parts []string
	if src.Kind == "folder" {
		d := filepath.Clean(src.Dir)
		parts = []string{filepath.Base(filepath.Dir(d)), filepath.Base(d)}
	} else {
		r := strings.TrimSuffix(strings.TrimRight(src.Repo, "/"), ".git")
		parts = strings.FieldsFunc(r, func(c rune) bool { return c == '/' || c == ':' })
	}
	switch len(parts) {
	case 0:
		return "", ""
	case 1:
		return "", fitName(parts[0])
	}
	return fitName(parts[len(parts)-2]), fitName(parts[len(parts)-1])
}

// withSuffix is name-suffix, name cut short so that the whole is a name
// the library takes (nameRe): at most 64 bytes.
func withSuffix(name, suffix string) string {
	suffix = fitName(suffix)
	if suffix == "" {
		return ""
	}
	if max := 64 - len(suffix) - 1; len(name) > max {
		if max < 1 {
			return ""
		}
		name = strings.TrimRight(name[:max], "-_")
	}
	n := name + "-" + suffix
	if !nameRe.MatchString(n) {
		return ""
	}
	return n
}

// nameSkill sets the name a skill's SKILL.md gives in its front matter to
// name, the folder's: an agent knows a skill by that name, and two by one
// name in an agent are one shadowing the other. Nothing else in it
// changes. A SKILL.md that gives no name already goes by its folder's,
// and one that is a link isn't written through.
func nameSkill(dir, name string) error {
	p := filepath.Join(dir, "SKILL.md")
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if out, ok := renamedMeta(string(b), name); ok {
		return os.WriteFile(p, []byte(out), fi.Mode().Perm())
	}
	return nil
}

// renamedMeta is a SKILL.md with its front matter's top-level name set to
// name, its line endings kept; false when it gives no name to set.
func renamedMeta(s, name string) (string, bool) {
	lines := strings.SplitAfter(s, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r\n") != "---" {
		return s, false
	}
	for i := 1; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r\n")
		if line == "---" {
			break
		}
		if strings.HasPrefix(line, "name:") {
			if strings.TrimSpace(strings.Trim(strings.TrimSpace(line[len("name:"):]), `"'`)) == name {
				return s, false
			}
			lines[i] = "name: " + name + lines[i][len(line):]
			return strings.Join(lines, ""), true
		}
	}
	return s, false
}

// fetchedHash is the hash a skill's files fetched at from have once in
// the library: for one kept under another name than its SKILL.md gives
// (Called), as named there. One that can't be told is "".
func (s *Skill) fetchedHash(from string) string {
	if s.Called == "" {
		return hashDir(from)
	}
	tmp, err := os.MkdirTemp("", "uni-switch-skill-named-")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(tmp)
	dir := filepath.Join(tmp, s.Name)
	if err := copyDir(from, dir); err != nil {
		return ""
	}
	if err := nameSkill(dir, s.Name); err != nil {
		return ""
	}
	return hashDir(dir)
}
