package library

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// tnt1223 on Discord: asdfgh1445/ctf-super-hub has 35 skills, one folder
// each at the top, and the page listed 16. Each SKILL.md names its skill in
// Chinese after a "CTF•" or "Strix•", and made fit for a file name the
// names fell to "CTF" (11 of them), "Strix" (10) and "" (头脑风暴), which
// were listed once each. The folders and names are the repository's own,
// at 7715719, quotes as written.
var ctfSuperHub = []struct{ dir, name string }{
	{"brainstorming", `"头脑风暴"`},
	{"ctf-ai-ml", `"CTF•AI/ML 攻防"`},
	{"ctf-beginner-hub", `"CTF•新手入口"`},
	{"ctf-crypto", `"CTF•密码学"`},
	{"ctf-forensics", `"CTF•数字取证"`},
	{"ctf-malware", `"CTF•恶意软件分析"`},
	{"ctf-misc", `"CTF•杂项"`},
	{"ctf-osint", `"CTF•开源情报"`},
	{"ctf-pwn", `"CTF•二进制利用"`},
	{"ctf-reverse", `"CTF•逆向工程"`},
	{"ctf-super-hub", `"CTF•超级总控"`},
	{"ctf-web", `"CTF•Web 漏洞"`},
	{"ctf-writeup", `"CTF•题解写作"`},
	{"solve-challenge", `"CTF•挑战调度器"`},
	{"strix-authentication-jwt", `Strix•JWT 身份认证测试`},
	{"strix-beginner-hub", `Strix•新手入口`},
	{"strix-broken-function-level-authorization", `Strix•功能级授权缺陷`},
	{"strix-business-logic", `Strix•业务逻辑漏洞`},
	{"strix-csrf", `Strix•CSRF 测试`},
	{"strix-ffuf", `Strix•ffuf 用法`},
	{"strix-httpx", `Strix•httpx 用法`},
	{"strix-idor", `Strix•IDOR 测试`},
	{"strix-information-disclosure", `Strix•信息泄露测试`},
	{"strix-insecure-file-uploads", `Strix•不安全文件上传`},
	{"strix-katana", `Strix•Katana 用法`},
	{"strix-nuclei", `Strix•Nuclei 用法`},
	{"strix-open-redirect", `Strix•开放重定向`},
	{"strix-path-traversal-lfi-rfi", `Strix•路径穿越与文件包含`},
	{"strix-quick", `Strix•快速扫描`},
	{"strix-rce", `Strix•远程代码执行`},
	{"strix-sql-injection", `Strix•SQL 注入`},
	{"strix-sqlmap", `Strix•sqlmap 用法`},
	{"strix-ssrf", `Strix•SSRF 测试`},
	{"strix-standard", `Strix•标准扫描`},
	{"strix-xss", `Strix•XSS 测试`},
}

func TestSkillsNamedInAnotherScriptAllListed(t *testing.T) {
	sandbox(t)
	files := map[string]string{
		"README.md":                      "# CTF Super Hub\n",
		"SKILL-INDEX.md":                 "| skill |\n",
		"docs/guide.md":                  "guide\n",
		"evals/README.md":                "evals\n",
		"scripts/install.sh":             "#!/bin/sh\n",
		".github/workflows/validate.yml": "on: push\n",
	}
	var want []string
	for _, s := range ctfSuperHub {
		files[s.dir+"/SKILL.md"] = "---\nname: " + s.name + "\ndescription: \"用于 " + s.dir + ";触发名:" + s.dir + "\"\n---\n\n# " + s.dir + "\n"
		files[s.dir+"/references/notes.md"] = "notes\n"
		want = append(want, s.dir)
	}
	fakeSkillRepo(t, files)

	p, err := ProbeSkills("https://github.com/asdfgh1445/ctf-super-hub")
	if err != nil {
		t.Fatal(err)
	}
	var paths, names []string
	for _, c := range p.Candidates {
		paths, names = append(paths, c.Path), append(names, c.Name)
		if !nameRe.MatchString(c.Name) {
			t.Errorf("%s goes by %q, which the library can't hold", c.Path, c.Name)
		}
	}
	slices.Sort(paths)
	if !slices.Equal(paths, want) {
		t.Errorf("listed %d skills %v, want all %d", len(paths), paths, len(want))
	}
	if !slices.Equal(names, want) { // sorted by name, each its folder's
		t.Errorf("names %v, want %v", names, want)
	}

	r, err := InstallSkills("https://github.com/asdfgh1445/ctf-super-hub", []string{"ctf-crypto", "strix-rce", "brainstorming"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := slices.Sorted(slices.Values(r.Installed)); !slices.Equal(got, []string{"brainstorming", "ctf-crypto", "strix-rce"}) {
		t.Errorf("installed %v", r.Installed)
	}
	if _, err := os.Stat(filepath.Join(skillDir("ctf-crypto"), "references", "notes.md")); err != nil {
		t.Errorf("ctf-crypto's files: %v", err)
	}
}

// A name in ASCII goes as it did, its spaces and marks made dashes, so a
// skill installed from it before is still the one the library has. A name
// in another script with no folder name to fall back on keeps what is left
// of it. A skill at the top of a repository goes by the repository's name,
// not the temporary folder it was downloaded to.
func TestSkillNameFallsBackToFolderOnlyWhenLettersAreLost(t *testing.T) {
	for _, c := range []struct{ folder, name, want string }{
		{"my-skill", "My Skill", "My-Skill"},
		{"pdf", "pdf", "pdf"},
		{"x", "", "x"},
		{"ctf-crypto", "CTF•密码学", "ctf-crypto"},
		{"brainstorming", "头脑风暴", "brainstorming"},
		{"cafe-notes", "Café notes", "cafe-notes"},
		{"技能", "头脑风暴", ""},
		{"技能", "CTF•密码学", "CTF"},
	} {
		if got := nameFor(c.folder, meta{Name: c.name}); got != c.want {
			t.Errorf("nameFor(%s, %q) = %q, want %q", c.folder, c.name, got, c.want)
		}
	}
	for _, c := range []struct {
		src  Source
		want string
	}{
		{Source{Kind: "github", Repo: "asdfgh1445/ctf-super-hub"}, "ctf-super-hub"},
		{Source{Kind: "git", Repo: "https://gitlab.example.com/group/skills.git"}, "skills"},
		{Source{Kind: "git", Repo: "git@gitlab.example.com:group/skills.git"}, "skills"},
		{Source{Kind: "git", Repo: "git@host:skills"}, "skills"},
		{Source{Kind: "folder", Dir: filepath.FromSlash("/home/me/my-skill")}, "my-skill"},
	} {
		if got := srcFolder(c.src); got != c.want {
			t.Errorf("srcFolder(%+v) = %q, want %q", c.src, got, c.want)
		}
	}
}

// A repository that is one skill at its top, named in another script, is
// listed under the repository's name.
func TestSkillAtTheTopNamedInAnotherScript(t *testing.T) {
	sandbox(t)
	fakeSkillRepo(t, map[string]string{"SKILL.md": "---\nname: \"头脑风暴\"\ndescription: d\n---\n"})
	p, err := ProbeSkills("asdfgh1445/brainstorming")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Candidates) != 1 || p.Candidates[0].Name != "brainstorming" || p.Candidates[0].Path != "" {
		t.Errorf("candidates %+v, want brainstorming at the top", p.Candidates)
	}
}
