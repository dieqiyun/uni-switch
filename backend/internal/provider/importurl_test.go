package provider

import (
	"net/url"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseImportPreset(t *testing.T) {
	p, err := ParseImport("uni-switch://import?preset=dieqiyun&key=sk-abc123&models=deepseek-v4-pro,%20deepseek-v4-flash")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "dieqiyun" || p.Preset != "dieqiyun" || p.Key != "sk-abc123" || p.Chat == "" {
		t.Fatalf("got %+v", p)
	}
	if !slices.Equal(p.Models, []string{"deepseek-v4-pro", "deepseek-v4-flash"}) {
		t.Fatalf("models %q", p.Models)
	}
}

// SiliconFlow's international site is another account: its endpoint, key
// page and console, while the preset's default stays the China site.
func TestSiliconFlowInternational(t *testing.T) {
	p, err := ParseImport("uni-switch://import?preset=siliconflow&region=intl")
	if err != nil {
		t.Fatal(err)
	}
	if p.Chat != "https://api.siliconflow.com/v1" || p.KeysURL != "https://cloud.siliconflow.com/account/ak" || p.Website != "https://cloud.siliconflow.com" {
		t.Fatalf("intl: got %q %q %q", p.Chat, p.KeysURL, p.Website)
	}
	if src, ok := balanceSourceOf(p); !ok || !strings.Contains(src.url, "api.siliconflow.com") {
		t.Fatalf("intl balance: %+v %v", src, ok)
	}
	d, err := FromPreset("siliconflow")
	if err != nil {
		t.Fatal(err)
	}
	if d.Chat != "https://api.siliconflow.cn/v1" || d.KeysURL != "https://cloud.siliconflow.cn/account/ak" {
		t.Fatalf("default: got %q %q", d.Chat, d.KeysURL)
	}
	pr := Preset("siliconflow")
	if r := d.regionOf(pr); r == nil || r.ID != "cn" {
		t.Fatalf("a China provider saved before the regions sits at %+v", r)
	}
	if r := p.regionOf(pr); r == nil || r.ID != "intl" {
		t.Fatalf("the international one sits at %+v", r)
	}
}

func TestParseImportCustom(t *testing.T) {
	q := url.Values{
		"name":      {"My Relay"},
		"chat":      {"https://relay.example/v1/"},
		"anthropic": {"https://relay.example"},
		"key":       {"sk-relay"},
		"website":   {"https://relay.example"},
		"keys":      {"javascript:alert(1)"},
		"catalog":   {"openai"},
	}
	p, err := ParseImport("uni-switch://import?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "my-relay" || p.Name != "My Relay" || p.Chat != "https://relay.example/v1" || p.Anthropic != "https://relay.example" || p.Preset != "" {
		t.Fatalf("got %+v", p)
	}
	if p.Website != "https://relay.example" || p.KeysURL != "" {
		t.Fatalf("pages: %q %q", p.Website, p.KeysURL)
	}
}

func TestParseImportRegion(t *testing.T) {
	var pr *PresetDef
	for i := range presets {
		if len(presets[i].Regions) > 1 {
			pr = &presets[i]
			break
		}
	}
	if pr == nil {
		t.Skip("no preset with regions")
	}
	r := pr.Regions[1]
	p, err := ParseImport("uni-switch://import?preset=" + pr.ID + "&region=" + r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Chat != r.Chat || p.Anthropic != r.Anthropic || p.Preset != pr.ID {
		t.Fatalf("got %+v, want region %+v", p, r)
	}
	if _, err := ParseImport("uni-switch://import?preset=" + pr.ID + "&region=nowhere"); err == nil {
		t.Fatal("unknown region accepted")
	}
}

// A Gemini-compatible relay is added in the app, the TUI and
// `uni-switch provider set` with a Gemini URL alone (#1346, #2201); a link
// naming one was refused as giving no base URL, and a gemini= beside
// chat= was dropped without a word.
func TestParseImportGemini(t *testing.T) {
	p, err := ParseImport("uni-switch://import?name=Gemini%20Relay&gemini=https://relay.example/v1beta&key=sk-g")
	if err != nil {
		t.Fatal(err)
	}
	if p.Gemini != "https://relay.example/v1beta" || p.Chat != "" || p.Responses != "" || p.Anthropic != "" {
		t.Fatalf("gemini only: got %+v", p)
	}
	p, err = ParseImport("uni-switch://import?name=X&chat=https://relay.example/v1/&gemini=https://relay.example/v1beta")
	if err != nil {
		t.Fatal(err)
	}
	if p.Chat != "https://relay.example/v1" || p.Gemini != "https://relay.example/v1beta" {
		t.Fatalf("chat and gemini: got %+v", p)
	}
	if _, err := ParseImport("uni-switch://import?name=X&gemini=not-a-url"); err == nil {
		t.Fatal("a bad gemini= was accepted")
	}
}

func TestParseImportRejects(t *testing.T) {
	for _, link := range []string{
		"https://github.com/dieqiyun/uni-switch/import?preset=dieqiyun",
		"uni-switch://delete?id=deepseek",
		"uni-switch://import",
		"uni-switch://import?preset=nope",
		"uni-switch://import?name=X",
		"uni-switch://import?name=X&chat=http://relay.example/v1",
		"uni-switch://import?name=X&chat=https://user:pw@relay.example/v1",
		"uni-switch://import?name=X&chat=file:///etc/passwd",
		"uni-switch://import?name=uni-switch&chat=https://x.example/v1",
		"uni-switch://import?name=%E2%80%94&chat=https://x.example/v1",
		"uni-switch://import?preset=dieqiyun&key=sk%0Aevil",
	} {
		if p, err := ParseImport(link); err == nil {
			t.Errorf("%s: accepted as %+v", link, p)
		}
	}
}

func TestParseImportIcon(t *testing.T) {
	q := url.Values{
		"name": {"My Relay"},
		"chat": {"https://relay.example/v1"},
		"icon": {"https://relay.example/logo.svg?v=2"},
	}
	p, err := ParseImport("uni-switch://import?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if p.IconURL != "https://relay.example/logo.svg?v=2" {
		t.Fatalf("iconUrl = %q", p.IconURL)
	}
	// an explicit icon wins over the catalog's own logo
	q.Set("catalog", "openai")
	if p, err = ParseImport("uni-switch://import?" + q.Encode()); err != nil || p.IconURL == "" {
		t.Fatalf("with a catalog: %+v, %v", p, err)
	}
	for _, bad := range []string{
		"http://relay.example/logo.svg", // plain http
		"file:///etc/passwd",
		"https://user:pw@relay.example/logo.svg",
		"https://relay.example/logo.svg#x",
		"https://localhost/logo.svg",
		"https://127.0.0.1/logo.svg",
		"not a url",
	} {
		q.Set("icon", bad)
		if _, err := ParseImport("uni-switch://import?" + q.Encode()); err == nil {
			t.Errorf("icon %q accepted", bad)
		}
	}
}

func TestParseImportLocalHTTP(t *testing.T) {
	for _, host := range []string{"localhost:11434", "127.0.0.1:8080", "192.168.1.20:8000", "box.local"} {
		if _, err := ParseImport("uni-switch://import?name=Local&chat=http://" + host + "/v1"); err != nil {
			t.Errorf("%s: %v", host, err)
		}
	}
}

func TestParseImportForms(t *testing.T) {
	for _, link := range []string{"uni-switch:import?preset=dieqiyun", "uni-switch:///import?preset=dieqiyun", "UNI-SWITCH://import/?preset=dieqiyun"} {
		if _, err := ParseImport(link); err != nil {
			t.Errorf("%s: %v", link, err)
		}
	}
}

// A long name in Chinese is cut to 80 bytes where a character begins, not
// through one: half a character is invalid UTF-8 the GUI shows as � (#2416).
func TestParseImportNameCutKeepsCharactersWhole(t *testing.T) {
	q := url.Values{
		"name": {strings.Repeat("鱼", 30) + "-relay"},
		"chat": {"https://relay.example/v1"},
		"key":  {"sk-relay"},
	}
	p, err := ParseImport("uni-switch://import?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(p.Name) || len(p.Name) > 80 || p.Name != strings.Repeat("鱼", 26) {
		t.Fatalf("name %q (%d bytes)", p.Name, len(p.Name))
	}
}
