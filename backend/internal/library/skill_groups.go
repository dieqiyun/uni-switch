package library

import (
	"fmt"
	"slices"
	"strings"
)

// SkillGroup is a group the user made of some skills (#791, mintonight:
// 能不能在选择后自己创建分组呢): the page shows it as a group of its own,
// above the ones by source, with its skills taken out of theirs. It is
// only how the page shows them; the agents get each skill as before.
type SkillGroup struct {
	Name   string   `json:"name"`
	Skills []string `json:"skills"`
}

// skillGroups are l's groups with the skills still in the library, and
// none left empty.
func (l *Library) skillGroups() []SkillGroup {
	out := []SkillGroup{}
	for _, g := range l.SkillGroups {
		names := slices.DeleteFunc(slices.Clone(g.Skills), func(n string) bool { return l.skill(n) == nil })
		if len(names) > 0 {
			out = append(out, SkillGroup{Name: g.Name, Skills: names})
		}
	}
	return out
}

// GroupSkills puts the skills named into the group called name, made
// when there is none, and takes them out of any other: a skill is in one
// group at most. old, when given, is a group renamed to name, and its
// skills are then those named when any are. No names and no old is
// nothing; a rename to a group there is already is refused.
func GroupSkills(old, name string, names []string) (*Result, error) {
	name, old = strings.TrimSpace(name), strings.TrimSpace(old)
	if name == "" {
		return nil, fmt.Errorf("a group needs a name")
	}
	if old == "" && len(names) == 0 {
		return nil, fmt.Errorf("no skills to group")
	}
	return change(func(l *Library) error {
		for _, n := range names {
			if l.skill(n) == nil {
				return fmt.Errorf("no skill called %s", n)
			}
		}
		g := l.skillGroup(name)
		if old != "" && old != name {
			if g != nil {
				return fmt.Errorf("there is a group called %s already", name)
			}
			if g = l.skillGroup(old); g == nil {
				return fmt.Errorf("no group called %s", old)
			}
			g.Name = name
		}
		if g == nil {
			g = &SkillGroup{Name: name}
			l.SkillGroups = append(l.SkillGroups, g)
		}
		for _, o := range l.SkillGroups {
			if o != g {
				o.Skills = slices.DeleteFunc(o.Skills, func(n string) bool { return slices.Contains(names, n) })
			}
		}
		for _, n := range names {
			if !slices.Contains(g.Skills, n) {
				g.Skills = append(g.Skills, n)
			}
		}
		l.pruneSkillGroups()
		return nil
	})
}

// UngroupSkills takes the group called name away; its skills go back to
// the groups by source. With names, only those leave it.
func UngroupSkills(name string, names []string) (*Result, error) {
	return change(func(l *Library) error {
		g := l.skillGroup(strings.TrimSpace(name))
		if g == nil {
			return fmt.Errorf("no group called %s", name)
		}
		if len(names) == 0 {
			g.Skills = nil
		} else {
			g.Skills = slices.DeleteFunc(g.Skills, func(n string) bool { return slices.Contains(names, n) })
		}
		l.pruneSkillGroups()
		return nil
	})
}

func (l *Library) skillGroup(name string) *SkillGroup {
	for _, g := range l.SkillGroups {
		if g.Name == name {
			return g
		}
	}
	return nil
}

// pruneSkillGroups drops skills no longer in the library, and groups
// left with none.
func (l *Library) pruneSkillGroups() {
	for _, g := range l.SkillGroups {
		g.Skills = slices.DeleteFunc(g.Skills, func(n string) bool { return l.skill(n) == nil })
	}
	l.SkillGroups = slices.DeleteFunc(l.SkillGroups, func(g *SkillGroup) bool { return len(g.Skills) == 0 })
}
