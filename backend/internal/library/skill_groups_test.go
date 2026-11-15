package library

import (
	"path/filepath"
	"reflect"
	"testing"
)

// Groups the user makes of some skills (#791, mintonight: 能不能在选择后
// 自己创建分组呢): a skill is in one at most, a rename keeps its skills,
// one ungrouped is gone, and a skill taken out of the library leaves its
// group, which goes once it's empty.
func TestSkillGroups(t *testing.T) {
	h := sandbox(t)
	src := filepath.Join(h, "src/skills")
	for _, n := range []string{"pdf", "xlsx", "docx"} {
		skill(t, filepath.Join(src, n), n, n)
	}
	ok(t)(InstallSkills(src, []string{"pdf", "xlsx", "docx"}, []string{"claude"}))
	groups := func() []SkillGroup {
		v, err := Read(nil)
		if err != nil {
			t.Fatal(err)
		}
		return v.SkillGroups
	}

	if _, err := GroupSkills("", " ", []string{"pdf"}); err == nil {
		t.Error("a group with no name was made")
	}
	if _, err := GroupSkills("", "office", []string{"nope"}); err == nil {
		t.Error("a skill the library hasn't was grouped")
	}
	ok(t)(GroupSkills("", " office ", []string{"pdf", "xlsx"}))
	ok(t)(GroupSkills("", "docs", []string{"docx", "pdf"}))
	want := []SkillGroup{{"office", []string{"xlsx"}}, {"docs", []string{"docx", "pdf"}}}
	if g := groups(); !reflect.DeepEqual(g, want) {
		t.Fatalf("groups %+v, want %+v", g, want)
	}
	if _, err := GroupSkills("office", "docs", nil); err == nil {
		t.Error("a rename onto another group was taken")
	}
	ok(t)(GroupSkills("office", "sheets", nil))
	ok(t)(UngroupSkills("docs", []string{"pdf"}))
	want = []SkillGroup{{"sheets", []string{"xlsx"}}, {"docs", []string{"docx"}}}
	if g := groups(); !reflect.DeepEqual(g, want) {
		t.Fatalf("after a rename and one out %+v, want %+v", g, want)
	}
	ok(t)(UngroupSkills("sheets", nil))
	ok(t)(RemoveSkill("docx"))
	if g := groups(); len(g) != 0 {
		t.Errorf("left %+v", g)
	}
	if v, _ := Read(nil); len(v.Skills) != 2 || len(v.Skills[0].Agents) != 1 {
		t.Errorf("grouping changed the skills: %+v", v.Skills)
	}
}
