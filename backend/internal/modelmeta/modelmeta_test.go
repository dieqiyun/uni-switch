package modelmeta

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func row(t *testing.T, s string) map[string]any {
	t.Helper()
	var v map[string]any
	d := json.NewDecoder(strings.NewReader(s))
	d.UseNumber()
	if e := d.Decode(&v); e != nil {
		t.Fatal(e)
	}
	return v
}
func catalog(t *testing.T, s string) *Catalog {
	t.Helper()
	c, e := ReadCatalog([]byte(s), nil)
	if e != nil {
		t.Fatal(e)
	}
	return c
}

func TestVendorUsesReferenceFieldsOnly(t *testing.T) {
	f := ParseVendor("m", row(t, `{"context_length":4095,"max_output_tokens":8192,"modalities":{"input":["text","image/png","application/pdf","audio/mpeg","image/jpeg"],"output":["text","audio"]},"supported_reasoning_levels":["light",{"effort":"extra_high"},false,{},"light"],"supported_endpoints":["/v1/chat/completions","/responses"],"native_endpoints":["/v1/messages/"]}`))
	if f.Context != 4095 || f.Output != 8192 || !reflect.DeepEqual(f.Efforts, []string{"light", "extra_high"}) || !reflect.DeepEqual(f.APIs, []string{"anthropic"}) || f.Images == nil || !*f.Images {
		t.Fatalf("vendor facts: %+v", f)
	}
	if !reflect.DeepEqual(f.Inputs, []string{"text", "image", "pdf", "audio"}) {
		t.Fatal(f.Inputs)
	}
	r := Resolve(f, nil, Preferences{}, "")
	if r.PublishedOutput != 4095 || r.Profile["maxOutputTokens"] != int64(8192) {
		t.Fatal(r)
	}
	for _, shape := range []string{`{}`, `{"context_window":123456,"max_tokens":9999,"supports_image_input":true,"supports_tool_calls":true,"reasoning_efforts":["high"],"capabilities":{"vision":true}}`} {
		r := Resolve(ParseVendor("m", row(t, shape)), nil, Preferences{}, "")
		if len(r.Profile) != 0 || len(r.Capabilities) != 0 {
			t.Fatal("historical guesses leaked", r)
		}
	}
	for _, input := range []struct {
		raw    string
		image  *bool
		inputs []string
	}{
		{`{}`, nil, nil}, {`{"modalities":{"input":[]}}`, boolPtr(false), []string{}}, {`{"modalities":{"input":["text"]}}`, boolPtr(false), []string{"text"}},
	} {
		f := ParseVendor("m", row(t, input.raw))
		if !reflect.DeepEqual(f.Images, input.image) || !reflect.DeepEqual(f.Inputs, input.inputs) {
			t.Fatalf("nil/empty/false lost: %+v", f)
		}
	}
}
func TestCodexEntryAndKimiFacts(t *testing.T) {
	f := ParseVendor("m", row(t, `{"slug":"m","supported_reasoning_levels":[{"effort":"low"},{"effort":"high"}],"shell_type":"shell_command","base_instructions":"Hi","context_window":272000,"max_context_window":1000000,"input_modalities":["text","image"]}`))
	if !f.Codex || f.Context != 272000 || f.MaxContext != 1000000 || len(f.CodexEntry) == 0 || f.Images == nil || !*f.Images {
		t.Fatalf("codex: %+v", f)
	}
	r := Resolve(f, &Facts{Provider: "openai", Context: 922000, Efforts: []string{"max"}}, Preferences{}, "catalog")
	if r.Profile["contextWindow"] != int64(272000) || !reflect.DeepEqual(r.Profile["reasoningEfforts"], []string{"low", "high"}) {
		t.Fatal(r)
	}
	for _, raw := range []string{`{"supports_thinking_type":"only","supports_dynamic_tools":true,"think_efforts":{"default_effort":"high"}}`, `{"reasoning":{"type":"only","effort":{"default":"high"}},"tools":{"dynamic_load":true}}`} {
		f := ParseVendor("kimi", row(t, raw))
		if !f.AlwaysThinks || !f.DynamicTools || f.DefaultEffort != "high" {
			t.Fatal(f)
		}
	}
	if f := ParseVendor("m", row(t, `{"supports_thinking_type":"optional","reasoning":{"type":"only"},"supports_dynamic_tools":false,"tools":{"dynamic_load":true}}`)); f.AlwaysThinks || f.DynamicTools {
		t.Fatal("flat facts should stand", f)
	}
}
func TestWindowVotingAndPublishedOutput(t *testing.T) {
	for _, tc := range []struct{ n, w int64 }{{0, 0}, {1, 0}, {2047, 0}, {2048, 2048}, {4095, 4095}, {1000000, 1000000}} {
		if w := Window(tc.n); w != tc.w {
			t.Fatalf("Window(%d)=%d", tc.n, w)
		}
	}
	for _, tc := range []struct{ input, context, w int64 }{{272000, 400000, 272000}, {272000, 128000, 128000}, {100, 4095, 4095}, {4095, 100, 4095}, {100, 100, 0}} {
		r := catalogRow{}
		r.Limit.Input = tc.input
		r.Limit.Context = tc.context
		if r.window() != tc.w {
			t.Fatal(tc, r.window())
		}
	}
	if winningNumber(map[int64]int{128000: 2, 1000000: 2}) != 128000 || winningLevels(map[string]int{"low,high,max": 1, "low,high": 1}) != "low,high" || winningName(map[string]int{"Long Name": 1, "Name": 1}) != "Name" {
		t.Fatal("reference voting tie changed")
	}
}
func TestSourceAndUserPrecedence(t *testing.T) {
	cat := &Facts{Context: 200000, Output: 128000, InferredImages: true, Efforts: []string{"low", "high"}}
	vendor := Facts{ID: "m", Context: 128000, Output: 8192, Inputs: []string{"text", "audio", "video", "pdf"}, Images: boolPtr(false), Efforts: []string{"medium"}}
	r := Resolve(vendor, cat, Preferences{}, "catalog")
	if r.Profile["contextWindow"] != int64(128000) || r.Capabilities["imageInput"] != false || !reflect.DeepEqual(r.Profile["reasoningEfforts"], []string{"medium"}) {
		t.Fatal(r)
	}
	r = Resolve(vendor, cat, Preferences{Context: 1000000, Output: 16000, Vision: boolPtr(true)}, "catalog")
	if r.Profile["contextWindow"] != int64(1000000) || r.Profile["maxOutputTokens"] != int64(16000) || !reflect.DeepEqual(r.Profile["inputModalities"], []string{"text", "image", "audio", "video", "pdf"}) {
		t.Fatal(r)
	}
	r = Resolve(vendor, cat, Preferences{ProviderContext: 1000000}, "catalog")
	if r.Profile["contextWindow"] != int64(128000) {
		t.Fatal("provider cap lost", r)
	}
	r = Resolve(Facts{ID: "relay"}, cat, Preferences{ProviderContext: 1000000}, "catalog")
	if r.Profile["contextWindow"] != int64(1000000) {
		t.Fatal("shared guess capped user", r)
	}
	own := *cat
	own.Provider = "own"
	r = Resolve(Facts{ID: "own"}, &own, Preferences{ProviderContext: 1000000}, "catalog")
	if r.Profile["contextWindow"] != int64(200000) {
		t.Fatal("own catalog did not cap provider preference", r)
	}
	r = Resolve(Facts{ID: "m"}, cat, Preferences{Vision: boolPtr(false)}, "catalog")
	if r.Capabilities["imageInput"] != false || !reflect.DeepEqual(r.Profile["inputModalities"], []string{"text"}) {
		t.Fatal(r)
	}
	r = Resolve(Facts{ID: "m"}, &Facts{}, Preferences{}, "catalog")
	if _, ok := r.Profile["inputModalities"]; ok {
		t.Fatal("unknown image became explicitly false", r)
	}
	r = Resolve(Facts{ID: "m", AlwaysThinks: true, DefaultEffort: "high", Efforts: []string{"low", "high"}}, nil, Preferences{Reasonless: true}, "")
	if r.Profile["reasoning"] != false || len(r.Profile["reasoningEfforts"].([]string)) != 0 || r.Profile["defaultEffort"] != nil {
		t.Fatal(r)
	}
}
func TestEffortsKeptAndNearest(t *testing.T) {
	for _, tc := range []struct{ all, kept, want []string }{
		{[]string{"low", "high"}, nil, []string{"low", "high"}}, {[]string{"high", "low"}, []string{"low"}, []string{"low"}}, {[]string{"high"}, []string{"xhigh"}, []string{"high"}}, {nil, []string{"high", "low"}, []string{"low", "high"}}, {nil, []string{"unrecognized"}, []string{}},
	} {
		if got := keptEfforts(tc.all, tc.kept); !reflect.DeepEqual(got, tc.want) {
			t.Fatal(tc, got)
		}
	}
	for _, tc := range []struct {
		want   string
		levels []string
		out    string
	}{
		{"medium", []string{"low", "high"}, "high"}, {"low", []string{"none", "high"}, "high"}, {"low", []string{"light", "extra_high"}, "light"}, {"xhigh", []string{"light", "extra_high"}, "extra_high"}, {"unknown", []string{"low"}, "unknown"}, {"ultra", nil, "ultra"},
	} {
		if got := NearestEffort(tc.want, tc.levels); got != tc.out {
			t.Fatal(tc, got)
		}
	}
}

func TestDecoratedAndSharedFactsKeepTheirDifferentRoles(t *testing.T) {
	c := catalog(t, `{"openai":{"models":{"m":{"id":"m","limit":{"context":128000}},"small":{"id":"small","limit":{"context":128000}}}},"relay-a":{"models":{"m":{"limit":{"context":256000,"output":8192},"reasoning":true,"modalities":{"input":["text","image"]}},"small":{"limit":{"context":256000}}}},"relay-b":{"models":{"m":{"limit":{"context":256000,"output":8192},"reasoning":true,"modalities":{"input":["text","image"]}},"small":{"limit":{"context":256000}}}}}`)
	f, source := c.Known("m", "https://api.openai.com/v1")
	for _, tc := range []struct{ live, preference, want int64 }{{0, 0, 128000}, {1, 0, 256000}, {2048, 0, 2048}, {0, 1000000, 128000}, {1, 1000000, 1000000}} {
		r := Resolve(Facts{ID: "m", Context: tc.live}, f, Preferences{ProviderContext: tc.preference}, source)
		if r.Profile["contextWindow"] != tc.want || r.PublishedOutput != OutputWithin(tc.want, 8192) {
			t.Fatal(tc, r)
		}
		if r.Capabilities["imageInput"] != true || r.Profile["reasoning"] != true {
			t.Fatal("shared thinking/images missing", r)
		}
	}
	// A complete Codex entry retains vendor levels and never receives an
	// invented Ultra mode. Other API entries add Ultra only after user filtering.
	for _, tc := range []struct {
		id                 string
		codex              bool
		levels, kept, want []string
	}{
		{"gpt-6-sol", false, []string{"low", "max"}, nil, []string{"low", "max", "ultra"}},
		{"openai/gpt-6.1-sol-2026-09-14", false, []string{"max"}, nil, []string{"max", "ultra"}},
		{"gpt-6-luna", false, []string{"max"}, nil, []string{"max"}},
		{"gpt-6-sol", true, []string{"max"}, nil, []string{"max"}},
		{"gpt-6-sol", false, []string{"low", "max"}, []string{"low"}, []string{"low"}},
	} {
		r := Resolve(Facts{ID: tc.id, Codex: tc.codex, Efforts: tc.levels}, nil, Preferences{Efforts: tc.kept}, "")
		if !reflect.DeepEqual(r.Profile["reasoningEfforts"], tc.want) {
			t.Fatal(tc, r)
		}
	}
}

func TestLabVotingDoesNotDiscardSameBareIDRows(t *testing.T) {
	c, e := ReadCatalog([]byte(`{"openai":{"models":{}}}`), []byte(`{"a/new":{"limit":{"context":256000}},"b/new":{"limit":{"context":128000}},"c/new":{"limit":{"context":128000}}}`))
	if e != nil {
		t.Fatal(e)
	}
	f, _ := c.Known("new", "")
	if f == nil || f.Context != 128000 {
		t.Fatal("lab voting differs from loadGen", f)
	}
}
func TestMakerCatalogFamilyAndRelayIsolation(t *testing.T) {
	c := catalog(t, `{"xiaomi":{"models":{"mimo-v2.6-flash":{"id":"mimo-v2.6-flash","reasoning":true,"reasoning_options":[{"type":"toggle"}]}}},"openai":{"models":{"gpt-6-sol":{"id":"gpt-6-sol","family":"gpt-sol","release_date":"2026-01-01","reasoning":true,"reasoning_options":[{"type":"effort","values":["low","high"]}]},"gpt-6.1-sol":{"id":"gpt-6.1-sol","family":"gpt-sol","release_date":"2026-02-01","reasoning":true}}},"deepseek":{"models":{"deepseek-r":{"id":"deepseek-r","reasoning":true,"reasoning_options":[{"type":"toggle"},{"type":"effort","values":["high","max"]}]}}},"relay":{"models":{"mimo-v2.6-flash":{"id":"mimo-v2.6-flash","reasoning_options":[{"type":"effort","values":["low","max"]}]}}}}`)
	f, _ := c.Known("mimo-v2.6-flash", "https://relay.invalid/v1")
	if f == nil || !f.LevelsKnown || len(f.Efforts) > 0 {
		t.Fatal("reseller overruled maker", f)
	}
	for _, id := range []string{"gpt-6.1-sol", "gpt-6.2-sol", "gpt-6-sol-high"} {
		f, _ := c.Known(id, "https://relay.invalid/v1")
		if f == nil || !reflect.DeepEqual(f.Efforts, []string{"low", "high"}) {
			t.Fatal(id, f)
		}
	}
	f, _ = c.Known("deepseek-r", "https://api.deepseek.com/v1")
	if !reflect.DeepEqual(f.Efforts, []string{"none", "high", "max"}) {
		t.Fatal("verified off not on own provider", f)
	}
	f, _ = c.Known("deepseek-r", "https://relay.invalid/v1")
	if !reflect.DeepEqual(f.Efforts, []string{"high", "max"}) {
		t.Fatal("provider toggle leaked into relay", f)
	}
	// Budget-only maker makes no claim about selectable effort words.
	c = catalog(t, `{"anthropic":{"models":{"m":{"id":"m","reasoning":true,"reasoning_options":[{"type":"budget_tokens"}]}}},"reseller":{"models":{"m":{"id":"m","reasoning_options":[{"type":"effort","values":["low","high"]}]}}}}`)
	f, _ = c.Known("m", "https://relay.invalid")
	if !reflect.DeepEqual(f.Efforts, []string{"low", "high"}) {
		t.Fatal(f)
	}
}
func TestCatalogAliasesSpellingAndProviderIsolation(t *testing.T) {
	c := catalog(t, `{"openai":{"models":{"gpt-5.5":{"id":"gpt-5.5","limit":{"context":1000000},"modalities":{"input":["text","image"]}},"gpt-mini":{"id":"gpt-mini","limit":{"context":128000}}}},"relay":{"models":{"custom-alias":{"id":"custom-alias","canonical_model_id":"openai/gpt-6-sol","limit":{"context":512000}},"claude-gpt-mini":{"id":"claude-gpt-mini","limit":{"context":64000}}}}}`)
	for _, tc := range []struct {
		id   string
		want int64
	}{{"gpt-5.5", 1000000}, {"GPT_5p5", 1000000}, {"nyro-gpt-5.5", 1000000}, {"gpt-5.5[200k]", 200000}, {"gpt-5.5(high)", 1000000}, {"gpt-5.5:batch", 1000000}, {"[WB] gpt-5.5", 1000000}, {"gpt-6-sol", 512000}, {"claude-gpt-mini", 64000}} {
		f, _ := c.Known(tc.id, "https://relay.invalid")
		if f == nil || f.Context != tc.want {
			t.Fatal(tc, f)
		}
	}
	if f, _ := c.Known("gpt-5.5-mini", "https://relay.invalid"); f != nil && f.Context > 0 {
		t.Fatal("different variant merged", f)
	}
	for _, tc := range []struct{ in, out string }{{" accounts/fireworks/models/DeepSeek_V4p1_Flash-260910 ", "deepseek-v4-1-flash"}, {"claude-opus-5-5@20260801", "claude-opus-5-5"}, {"gpt-4o-2024-08-06", "gpt-4o-2024-08-06"}, {"qwen3-2507", "qwen3-2507"}, {"kimi-k2-0905", "kimi-k2-0905"}, {"model-261332", "model-261332"}} {
		if got := SameModel(tc.in); got != tc.out {
			t.Fatal(tc, got)
		}
	}
	// A plan on the same hostname must not inherit the pay-as-you-go catalog.
	ids := c.providerIDs("https://api.openai.com/some-proxy/v1")
	if len(ids) > 0 {
		t.Fatal("host-only catalog match", ids)
	}
}
func TestLabsOnlySupplementAndCacheRefresh(t *testing.T) {
	main := `{"openai":{"models":{"m":{"id":"m","limit":{"context":128000}}}}}`
	labs := `{"openai/m":{"id":"m","limit":{"context":1000000}},"anthropic/new":{"id":"new","limit":{"context":512000}}}`
	c, e := ReadCatalog([]byte(main), []byte(labs))
	if e != nil {
		t.Fatal(e)
	}
	a, _ := c.Known("m", "")
	b, _ := c.Known("new", "")
	if a.Context != 128000 || b.Context != 512000 {
		t.Fatal(a, b)
	}
	dir := t.TempDir()
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "" || r.Header.Get("x-api-key") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credentials reached public catalog")
		}
		if r.URL.Path == "/api" {
			w.Write([]byte(main))
		} else {
			w.Write([]byte(labs))
		}
	}))
	defer server.Close()
	cache := Cache{Directory: dir, Client: server.Client(), CatalogURL: server.URL + "/api", LabsURL: server.URL + "/labs"}
	before := cache.Status()
	if before.Models != 0 || !before.Stale {
		t.Fatal(before)
	}
	status, e := cache.Update(context.Background(), false)
	if e != nil || !status.Updated || status.Models != 2 || status.SHA256 == "" {
		t.Fatal(status, e)
	}
	_, e = cache.Update(context.Background(), false)
	if e != nil || calls != 2 {
		t.Fatal("fresh cache should skip network", calls, e)
	}
	// Bad or failed updates cannot destroy the last valid cache.
	old, _ := os.ReadFile(filepath.Join(dir, "models-dev.json"))
	cache.CatalogURL = server.URL + "/bad" // serves labs JSON, not a provider catalog
	if _, e = cache.Update(context.Background(), true); e == nil {
		t.Fatal("bad schema accepted")
	}
	saved, _ := os.ReadFile(filepath.Join(dir, "models-dev.json"))
	if string(saved) != string(old) {
		t.Fatal("cache corrupted")
	}
	past := time.Now().Add(-25 * time.Hour)
	if e = os.Chtimes(filepath.Join(dir, "models-dev.json"), past, past); e != nil {
		t.Fatal(e)
	}
	if !cache.Status().Stale {
		t.Fatal("stale cache not recognized")
	}
	// Each resolver reads current cache generation, including writes by another
	// worker; no in-process once-only catalog survives a file refresh.
	if e = atomicSave(filepath.Join(dir, "models-dev.json"), []byte(strings.ReplaceAll(main, "128000", "256000"))); e != nil {
		t.Fatal(e)
	}
	next, _, _ := cache.Load()
	f, _ := next.Known("m", "")
	if f.Context != 256000 {
		t.Fatal("new cache invisible", f)
	}
}
func TestCacheFallbackAndFirstOffline(t *testing.T) {
	dir := t.TempDir()
	fallback := filepath.Join(t.TempDir(), "models.json")
	if e := os.WriteFile(fallback, []byte(`{"openai":{"models":{"m":{"id":"m","limit":{"context":4095}}}}}`), 0600); e != nil {
		t.Fatal(e)
	}
	cache := Cache{Directory: dir, Fallback: fallback}
	c, path, _ := cache.Load()
	f, _ := c.Known("m", "")
	if path != fallback || f.Context != 4095 {
		t.Fatal(path, f)
	}
	if e := atomicSave(filepath.Join(dir, "models-dev.json"), []byte(`not-json`)); e != nil {
		t.Fatal(e)
	}
	c, _, _ = cache.Load()
	if len(c.providers) != 1 {
		t.Fatal("invalid own cache blocks fallback")
	}
	cache.Fallback = ""
	c, _, _ = cache.Load()
	result := Resolve(Facts{ID: "unknown"}, nil, Preferences{}, "")
	if len(c.providers) != 0 || len(result.Profile) != 0 || len(result.Capabilities) != 0 {
		t.Fatal("offline invented facts")
	}
}
