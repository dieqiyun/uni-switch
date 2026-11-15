// PLUGIN-SERVED (see AGENTS.md): a moved Factory's models are the plugin's
// (the retired opencode-factory-auth, packages/factory/models.test.mjs
// checks the same fixture there); this is the built-in's table.

package provider

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// JL on Discord (droid 0.240.0): droid offers Haiku 5.5, a Factory account
// through uni-switch didn't. The fixture is droid 0.240.0's own registry
// entries (@factory/cli-darwin-arm64, bin/droid), byte for byte.
func TestFactoryDroid0240Models(t *testing.T) {
	raw, err := os.ReadFile("testdata/factory_droid_0.240.0_models.txt")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	entry := func(id string) string {
		for _, l := range strings.Split(src, "\n") {
			if strings.HasPrefix(l, `"`+id+`":{id:"`+id+`",`) {
				return l
			}
		}
		t.Fatalf("%s is not in the fixture", id)
		return ""
	}
	limits := func(text string) (int, int) {
		re := regexp.MustCompile(`contextLimits:R\(\{maxInputTokens:(\d+),maxOutputTokens:(\d+)\}\)`)
		m := re.FindStringSubmatch(text)
		if m == nil {
			name := regexp.MustCompile(`contextLimits:(\w+),`).FindStringSubmatch(text)[1]
			m = regexp.MustCompile(`(?m)^` + name + `=te\(\{[^}]*maxInputTokens:(\d+),maxOutputTokens:(\d+)\}\)`).FindStringSubmatch(src)
		}
		c, _ := strconv.Atoi(m[1])
		o, _ := strconv.Atoi(m[2])
		return c, o
	}
	for _, c := range []struct {
		id  string
		api Protocol
	}{{"claude-haiku-5-5", Anthropic}, {"mistral-large-4", Chat}} {
		text := entry(c.id)
		m, ok := factoryModelOf(c.id)
		if !ok {
			t.Fatalf("%s isn't offered", c.id)
		}
		if name := regexp.MustCompile(`,name:"([^"]+)"`).FindStringSubmatch(text)[1]; m.name != name {
			t.Errorf("%s name %q, droid %q", c.id, m.name, name)
		}
		if ctx, out := limits(text); m.context != ctx || m.output != out {
			t.Errorf("%s limits %d/%d, droid %d/%d", c.id, m.context, m.output, ctx, out)
		}
		var efforts []string
		json.Unmarshal([]byte(regexp.MustCompile(`reasoningEffort:\{supported:(\[[^\]]*\])`).FindStringSubmatch(text)[1]), &efforts)
		efforts = slices.DeleteFunc(efforts, func(e string) bool { return e == "off" })
		if !slices.Equal(m.efforts, efforts) {
			t.Errorf("%s efforts %v, droid %v", c.id, m.efforts, efforts)
		}
		if m.api != c.api || !m.images {
			t.Errorf("%s api %s images %v", c.id, m.api, m.images)
		}
	}
	if !strings.Contains(entry("mistral-large-4"), `billingPool:"core"`) || !factoryCore("mistral-large-4") || factoryCore("claude-haiku-5-5") {
		t.Error("Mistral Large 4 is Droid Core's, Haiku 5.5 Standard's")
	}
	// offered only after the user accepts Factory's high-cost terms
	if _, ok := factoryModelOf("gpt-6-astra-ultrafast"); ok || !strings.Contains(entry("gpt-6-astra-ultrafast"), `explicitOptInRequirement:{kind:"high_cost"`) {
		t.Error("GPT-6 Astra Ultra Fast is opt-in in droid and stays out")
	}
}

// #2080: droid 0.241.1 offers Gemini 4 Argon. The fixture is droid
// 0.241.1's picker and Gemini entries, byte for byte; I(total, output)
// states the total context, as k does for Gemini 3.
func TestFactoryDroid0241Gemini(t *testing.T) {
	raw, err := os.ReadFile("testdata/factory_droid_0.241.1_gemini.txt")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	var picker []string
	json.Unmarshal([]byte(regexp.MustCompile(`(?m)^var y=(\[[^\]]*\]);`).FindStringSubmatch(src)[1]), &picker)
	n := 0
	for _, id := range picker {
		if !strings.HasPrefix(id, "gemini-") {
			continue
		}
		n++
		var text string
		for _, l := range strings.Split(src, "\n") {
			if strings.HasPrefix(l, `"`+id+`":{id:"`+id+`",`) {
				text = l
			}
		}
		m, ok := factoryModelOf(id)
		if !ok || text == "" {
			t.Fatalf("%s offered %v, in the fixture %v", id, ok, text != "")
		}
		if name := regexp.MustCompile(`,name:"([^"]+)"`).FindStringSubmatch(text)[1]; m.name != name {
			t.Errorf("%s name %q, droid %q", id, m.name, name)
		}
		lim := regexp.MustCompile(`contextLimits:[Ik]\((\d+e\d+|\d+),(\d+)\)`).FindStringSubmatch(text)
		ctx, _ := strconv.ParseFloat(lim[1], 64)
		out, _ := strconv.Atoi(lim[2])
		if m.context != int(ctx) || m.output != out {
			t.Errorf("%s limits %d/%d, droid %v/%d", id, m.context, m.output, ctx, out)
		}
		var efforts []string
		json.Unmarshal([]byte(regexp.MustCompile(`reasoningEffort:\{supported:(\[[^\]]*\])`).FindStringSubmatch(text)[1]), &efforts)
		if !slices.Equal(m.efforts, efforts) {
			t.Errorf("%s efforts %v, droid %v", id, m.efforts, efforts)
		}
		// droid takes images unless an entry says images:!1
		if m.api != Gemini || m.upstream != "google" || !m.images || strings.Contains(text, "images:!1") || factoryCore(id) {
			t.Errorf("%s: %+v", id, m)
		}
	}
	if n != 7 {
		t.Errorf("droid 0.241.1 offers %d Gemini models", n)
	}
}
