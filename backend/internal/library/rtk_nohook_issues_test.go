package library

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// sharedRTKIssues are the agents that cite the same rtk-ai/rtk issues on
// purpose, each with why. Any other two agents citing the same list is a
// reason copied from one agent onto another.
var sharedRTKIssues = map[[2]string]string{
	// Grok App runs Grok Build, and rtk-ai/rtk#2136 asks for Grok Build.
	{"grok", "grok-app"}: "Grok App runs Grok Build",
	// Qoder CN is Qoder's China build, and rtk-ai/rtk#1327 asks for Qoder.
	{"qoder", "qoder-cn"}: "Qoder CN is Qoder's China build",
}

var rtkIssueRe = regexp.MustCompile(`github\.com/([\w.-]+/[\w.-]+)/issues/(\d+)`)

func rtkIssues(s string) []string {
	var out []string
	for _, m := range rtkIssueRe.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1]+"#"+m[2])
	}
	sort.Strings(out)
	return slices.Compact(out)
}

// The RTK tab's no-hook tag says each agent's own reason and cites its
// own issues (新v学员 on Discord: Grok Build's tag said DeepSeek Harness's
// hooks can't rewrite a command and linked dsh's rtk-ai/rtk#3847, since
// every agent got dsh's text). The page's reasons (rtkNoHookWhy in
// plugins/library/client.js) cover every agent in rtkNoHook and cite the
// same issues as rtk.go's, two agents share an issue list only when
// sharedRTKIssues says why, and the fallback for an agent with no entry
// cites nothing.
func TestRTKNoHookIssuesAreEachAgents(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "gui", "assets", "plugins", "library", "client.js"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	start := strings.Index(s, "function rtkNoHookWhy(")
	if start < 0 {
		t.Fatal("client.js has no rtkNoHookWhy: the no-hook tag isn't per agent")
	}
	end := strings.Index(s[start:], "\n  }\n")
	if end < 0 {
		t.Fatal("rtkNoHookWhy has no end")
	}
	body := s[start : start+end]
	page := map[string]string{}
	fallback, hasFallback := "", false
	for _, line := range strings.Split(body, "\n") {
		if m := regexp.MustCompile(`^\s*case "([\w-]+)": return (.*)$`).FindStringSubmatch(line); m != nil {
			page[m[1]] = m[2]
		} else if m := regexp.MustCompile(`^\s*default: return (.*)$`).FindStringSubmatch(line); m != nil {
			fallback, hasFallback = m[1], true
		}
	}
	if !hasFallback {
		t.Error("rtkNoHookWhy has no default reason")
	}
	if got := rtkIssues(fallback); len(got) > 0 {
		t.Errorf("the reason for an agent with no entry cites %v", got)
	}

	ids := make([]string, 0, len(rtkNoHook))
	for id := range rtkNoHook {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		why, ok := page[id]
		if !ok {
			t.Errorf("%s is in rtkNoHook but the page has no reason for it", id)
			continue
		}
		if g, p := rtkIssues(rtkNoHook[id]), rtkIssues(why); !slices.Equal(g, p) {
			t.Errorf("%s cites %v in rtk.go and %v on the page", id, g, p)
		}
	}
	for id := range page {
		if _, ok := rtkNoHook[id]; !ok {
			t.Errorf("the page has a reason for %s, which rtkNoHook doesn't list", id)
		}
	}

	for _, src := range []map[string]string{rtkNoHook, page} {
		for i, a := range ids {
			for _, b := range ids[i+1:] {
				ia, ib := rtkIssues(src[a]), rtkIssues(src[b])
				if len(ia) == 0 || !slices.Equal(ia, ib) {
					continue
				}
				if _, ok := sharedRTKIssues[[2]string{a, b}]; !ok {
					t.Errorf("%s and %s both cite %v: one's reason was copied onto the other", a, b, ia)
				}
			}
		}
	}
	// a deliberate share that no longer holds is dropped from the list
	for pair := range sharedRTKIssues {
		if ia := rtkIssues(rtkNoHook[pair[0]]); len(ia) == 0 || !slices.Equal(ia, rtkIssues(rtkNoHook[pair[1]])) {
			t.Errorf("sharedRTKIssues lists %v, which don't share an issue list", pair)
		}
	}
}
