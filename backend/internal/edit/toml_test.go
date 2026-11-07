package edit

import (
	"strings"
	"testing"
)

func TestTOMLSpanEditsPreserveUnownedMultilineAndComments(t *testing.T) {
	before := "# 私人配置\nmodel = 'old' # keep this\nprompt = '''\n[not.a.table]\n# not a comment\n'''\n[projects.\"D:/代码\"]\ntrust_level = 'trusted' # project\n[empty]\n"
	after, e := TOMLSet(before, []string{"model"}, "new", false)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(after, "model = \"new\" # keep this") || !strings.Contains(after, "[not.a.table]\n# not a comment") || !strings.Contains(after, "[empty]") {
		t.Fatal(after)
	}
	after, e = TOMLSet(after, []string{"desktop", "enabled-reasoning-efforts"}, []any{"low", "max"}, false)
	if e != nil {
		t.Fatal(e)
	}
	again, e := TOMLSet(after, []string{"desktop", "enabled-reasoning-efforts"}, []any{"low", "max"}, false)
	if e != nil || again != after {
		t.Fatalf("no-op changed bytes: %v", e)
	}
	restored, e := TOMLSet(after, []string{"desktop", "enabled-reasoning-efforts"}, nil, true)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(restored, "desktop") || !strings.Contains(restored, "trust_level = 'trusted' # project") || !strings.Contains(restored, "[empty]") {
		t.Fatal(restored)
	}
}
func TestTOMLMultilineArrayAndOwnedDescendantReplacement(t *testing.T) {
	before := "values = [\n  'one#two', # array\n  'three',\n] # keep\n[model_providers.uni_switch]\nbase_url='old'\n[model_providers.uni_switch.http_headers]\nx='old'\n[model_providers.personal]\nbase_url='personal' # untouched\n"
	s, e := TOMLSet(before, []string{"values"}, []any{"new"}, false)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(s, "values = [\"new\"] # keep") {
		t.Fatal(s)
	}
	m := map[string]any{"base_url": "new", "http_headers": map[string]any{"x-api-key": "synthetic"}}
	s, e = TOMLSet(s, []string{"model_providers", "uni_switch"}, m, false)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(s, "x='old'") || !strings.Contains(s, "base_url='personal' # untouched") {
		t.Fatal(s)
	}
	same, e := TOMLSet(s, []string{"model_providers", "uni_switch"}, m, false)
	if e != nil || same != s {
		t.Fatal("owned table no-op rewrote file", e)
	}
}
