package catalog

import (
	"strings"
	"testing"
)

// pinmmis on X (10-29): the same model from several relays, each renamed
// to be told apart, "ds.4.1(WB)" for deepseek-v4.1-flash. No rule can read
// that, so the user says which model it is (the editor's Same as,
// settings' ModelSameAs), and every spec models.dev gives follows it: the
// window, the reply limit, the levels, images, thinking.
const userSameCatalog = `{
  "deepseek": {"models": {
    "deepseek-v4.1-flash": {"id":"deepseek-v4.1-flash","name":"DeepSeek V4.1 Flash","limit":{"context":1048576,"output":393216},
      "reasoning":true,"reasoning_options":[{"type":"effort","values":["high","max"]}],"modalities":{"input":["text"]},
      "cost":{"input":0.14,"output":0.28}},
    "deepseek-v4.1-pro": {"id":"deepseek-v4.1-pro","limit":{"context":1048576,"output":393216},"modalities":{"input":["text"]}}
  }},
  "openai": {"models": {
    "gpt-5.5": {"id":"gpt-5.5","limit":{"context":400000,"output":128000},"modalities":{"input":["text","image"]},
      "reasoning_options":[{"type":"effort","values":["low","medium","high","xhigh"]}]}
  }}
}`

func withUserSame(t *testing.T, same map[string]string) {
	t.Helper()
	was := UserSame
	UserSame = func(b string) (string, bool) { v, ok := same[b]; return v, ok }
	t.Cleanup(func() { UserSame = was })
}

func TestUserSameGivesTheSpecs(t *testing.T) {
	writeCatalog(t, userSameCatalog)
	if ContextOf("ds.4.1(WB)") != 0 || Knows("ds.4.1(wb)") {
		t.Fatal("ds.4.1(WB) known before the user said what it is")
	}
	withUserSame(t, map[string]string{
		"ds.4.1(wb)": "deepseek-v4.1-flash",
		"gpt-5.5":    "deepseek-v4.1-pro", // listed by its own id: stays its own
		"loop-a":     "loop-b",
		"loop-b":     "loop-a",
		"g55":        "openai/GPT-5.5",
	})
	for _, id := range []string{"ds.4.1(WB)", "relay/ds.4.1(WB)"} {
		if got := ContextOf(id); got != 1048576 {
			t.Errorf("ContextOf(%q) = %d", id, got)
		}
		if got := OutputOf(id); got != 393216 {
			t.Errorf("OutputOf(%q) = %d", id, got)
		}
		if got := strings.Join(EffortsOf(id), ","); got != "high,max" {
			t.Errorf("EffortsOf(%q) = %q", id, got)
		}
		if !Thinks(id) || !Knows(id) {
			t.Errorf("%q: Thinks %v Knows %v", id, Thinks(id), Knows(id))
		}
	}
	if got := ContextOf("g55"); got != 400000 || !SeesImages("g55") {
		t.Errorf("g55 (said with a vendor's prefix, any case): %d %v", got, SeesImages("g55"))
	}
	if got := ContextOf("gpt-5.5"); got != 400000 {
		t.Errorf("a model listed by its own id took the user's word: %d", got)
	}
	if got := ContextOf("loop-a"); got != 0 {
		t.Errorf("a loop of words found %d", got)
	}
	// the usage report sends only an id models.dev knows as written
	if KnowsAsWritten("ds.4.1(WB)") {
		t.Error("KnowsAsWritten took the user's word")
	}
	// a price is the user's word through the provider (SamePriceIn), never
	// borrowed here
	if pr, ok := PricedBy([]string{"deepseek"}, "ds.4.1(WB)"); ok {
		t.Errorf("PricedBy = %+v", pr)
	}
}

// A tag in brackets before or after an id models.dev knows is a channel's,
// not the model's: its specs are the model's without it, with no word
// from the user. Anything else stays unknown.
func TestTaggedIDHasTheModelsSpecs(t *testing.T) {
	writeCatalog(t, userSameCatalog)
	for _, c := range []struct {
		id     string
		window int
	}{
		{"deepseek-v4.1-flash (WB)", 1048576},
		{"DeepSeek-V4.1-Flash（WB）", 1048576},
		{"[WB] deepseek-v4.1-flash", 1048576},
		{"【WB】DeepSeek V4.1 Flash", 1048576},
		{"deepseek-v4-1-flash [relay] (2)", 1048576},
		{"gpt-5.5 - (team)", 400000},
		{"deepseek-v4.1-flash(wb)", 1048576}, // found's "(level)" already
		// not a tag, or nothing known left
		{"ds.4.1(WB)", 0},
		{"deepseek-v4.1-flashy (WB)", 0},
		{"deepseek-v4.1 (WB) flash", 0},
		{"(WB)", 0},
		{"[1m]", 0},
		{"deepseek-v4.1 [flash]", 0},
		{"deepseek-v4.1-flash[wb]", 0}, // written onto it: maybe a variant's
	} {
		if got := ContextOf(c.id); got != c.window {
			t.Errorf("ContextOf(%q) = %d, want %d", c.id, got, c.window)
		}
	}
	if got := strings.Join(EffortsOf("[WB] deepseek-v4.1-flash"), ","); got != "high,max" {
		t.Errorf("EffortsOf = %q", got)
	}
	if !SeesImages("gpt-5.5 (team)") {
		t.Error("SeesImages lost the tagged model")
	}
	if KnowsAsWritten("deepseek-v4.1-flash (WB)") {
		t.Error("KnowsAsWritten took a tagged id")
	}
	if pr, ok := PricedBy([]string{"deepseek"}, "deepseek-v4.1-flash (WB)"); ok {
		t.Errorf("a tagged id is priced: %+v", pr)
	}
}
