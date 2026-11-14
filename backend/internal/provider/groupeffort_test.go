package provider

import (
	"testing"
)

// effortHome is a machine of its own with providers whose model ids have
// colons of their own: OpenRouter's :free, Ollama's :7b, Bedrock's :0,
// and one whose id ends in a level's name.
func effortHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, p := range []Provider{
		{ID: "a", Name: "A", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m", "n"}},
		{ID: "or", Name: "OR", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"deepseek/deepseek-r1:free", "odd:high"}},
		{ID: "ol", Name: "Ollama", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"qwen:7b"}},
		{ID: "bd", Name: "Bedrock", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"anthropic.claude-opus-4-v1:0"}},
	} {
		if err := Save(p); err != nil {
			t.Fatal(err)
		}
	}
}

// A trailing :<level> is an effort; any other colon is the model's own, as
// is a level's name the model's own id ends in.
func TestMemberEffortParsing(t *testing.T) {
	effortHome(t)
	for in, want := range map[string][2]string{
		"a/m":                                  {"a/m", ""},
		"a/m:low":                              {"a/m", "low"},
		"a/m:XHigh":                            {"a/m", "xhigh"},
		"a/m:none":                             {"a/m", "none"},
		"a/m:max":                              {"a/m", "max"},
		"a/m:turbo":                            {"a/m:turbo", ""},
		"or/deepseek/deepseek-r1:free":         {"or/deepseek/deepseek-r1:free", ""},
		"or/deepseek/deepseek-r1:free:medium":  {"or/deepseek/deepseek-r1:free", "medium"},
		"deepseek/deepseek-r1:free":            {"deepseek/deepseek-r1:free", ""},
		"ol/qwen:7b":                           {"ol/qwen:7b", ""},
		"qwen:7b":                              {"qwen:7b", ""},
		"ol/qwen:7b:high":                      {"ol/qwen:7b", "high"},
		"bd/anthropic.claude-opus-4-v1:0":      {"bd/anthropic.claude-opus-4-v1:0", ""},
		"bd/anthropic.claude-opus-4-v1:0:high": {"bd/anthropic.claude-opus-4-v1:0", "high"},
		"or/odd:high":                          {"or/odd:high", ""}, // the model's own id
		"or/odd:high:low":                      {"or/odd:high", "low"},
		"group/fast:low":                       {"group/fast", "low"}, // for SaveGroup to refuse
		":low":                                 {":low", ""},
	} {
		m, e := MemberEffort(in)
		if m != want[0] || e != want[1] {
			t.Errorf("MemberEffort(%q) = %q %q, want %q %q", in, m, e, want[0], want[1])
		}
	}
	if WithMemberEffort("a/m", "") != "a/m" || WithMemberEffort("a/m", "low") != "a/m:low" {
		t.Error("WithMemberEffort")
	}
}

// While a request holds the providers, whether a member at an effort is a
// model's own id is looked up among them once, not at each of the
// group's expansions (XcodeFish, #2502: a third of /v1/models' CPU was
// Find and Available again for every such member); a provider saved
// meanwhile is looked at anew, and with nothing held every look is fresh.
func TestMemberEffortLooksOncePerHold(t *testing.T) {
	effortHome(t)
	looks := func(id string, n int) int64 {
		was := providersLooked.Load()
		for range n {
			if m, e := MemberEffort(id); m+":"+e != id {
				t.Fatalf("MemberEffort(%q) = %q %q", id, m, e)
			}
		}
		return providersLooked.Load() - was
	}
	release := Hold()
	if got := looks("a/m:low", 5); got != 1 {
		t.Fatalf("held: looked %d times, want 1", got)
	}
	if err := Save(Provider{ID: "a", Name: "A", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m", "m:low"}}); err != nil {
		t.Fatal(err)
	}
	if m, e := MemberEffort("a/m:low"); m != "a/m:low" || e != "" {
		t.Fatalf("saved while held: %q %q, want the model's own id", m, e)
	}
	release()
	if got := looks("a/n:high", 3); got != 3 {
		t.Fatalf("not held: looked %d times, want 3", got)
	}
}
