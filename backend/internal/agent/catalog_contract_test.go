package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCode2NativeSchemaAndUserConnectionOptions(t *testing.T) {
	p := fixture("opencode")
	p.Protocol = "chat_completions"
	raw := `{"provider":{"uni-switch":{"options":{"headers":{"X-Personal":"keep"},"setCacheKey":false}}},"providers":{"uni-switch":{"settings":{"headers":{"X-Native":"keep"}},"compatibility":{"supportsPromptCacheKey":false}}}}`
	p.Files = []File{{Path: filepath.Join(t.TempDir(), "opencode.jsonc"), Format: "jsonc", Content: &raw}}
	plan, e := Build(p)
	if e != nil {
		t.Fatal(e)
	}
	d, e := parseConfig(File{Format: "jsonc", Content: plan.Changes[0].After})
	if e != nil {
		t.Fatal(e)
	}
	if Get(d, "providers.uni-switch.package") != "aisdk:@ai-sdk/openai-compatible" || Get(d, "providers.uni-switch.settings.baseURL") != p.BaseURL {
		t.Fatal(*plan.Changes[0].After)
	}
	if Get(d, "providers.uni-switch.options") != nil || Get(d, "provider.uni-switch.options.headers.X-Personal") != "keep" || Get(d, "providers.uni-switch.settings.headers.X-Native") != "keep" || Get(d, "providers.uni-switch.compatibility.supportsPromptCacheKey") != false {
		t.Fatal(*plan.Changes[0].After)
	}
}

func TestProfilesDiscoverRealWorkspaceAndKeepSecretsAtRoot(t *testing.T) {
	dir := t.TempDir()
	workspace := filepath.Join(dir, "custom-agent")
	data := Canonical(Object{"agents": Object{"profiles": Object{"research": Object{"workspace_dir": workspace}, "off": Object{"enabled": false}}}})
	if e := os.WriteFile(filepath.Join(dir, "config.json"), []byte(data), 0600); e != nil {
		t.Fatal(e)
	}
	specs, e := ProfileSpecifications("qwenpaw", dir, "research", false)
	if e != nil {
		t.Fatal(e)
	}
	if specs[0].Path != filepath.Join(workspace, "agent.json") || specs[1].Path != filepath.Join(dir+".secret", "providers", "custom", "uni-switch.json") {
		t.Fatal(specs)
	}
	if _, e := ProfileSpecifications("qwenpaw", dir, "off", false); e == nil {
		t.Fatal("disabled profile accepted")
	}
	if _, e := ProfileSpecifications("qwenpaw", dir, "../escape", false); e == nil {
		t.Fatal("missing profile accepted")
	}
	root := filepath.Join(dir, ".omp")
	current := filepath.Join(root, "profiles", "work", "agent")
	for _, p := range []string{current, filepath.Join(root, "profiles", "personal", "agent")} {
		if e := os.MkdirAll(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	profiles := Profiles("omp", current)
	found := false
	for _, p := range profiles {
		found = found || p.ID == "profile:personal"
	}
	if !found {
		t.Fatal("sibling omp profile missing", profiles)
	}
}

func TestRealTOMLArrayTablesPreserveOtherEntries(t *testing.T) {
	p := fixture("reasonix")
	p.Protocol = "chat_completions"
	raw := "# personal comment\n[[providers]]\nname='personal'\nkind='openai'\nmodels=['keep']\n[[providers]]\nname='uni-switch'\nkind='openai'\nmodels=['stale']\n[desktop]\ntheme='keep'\n"
	p.Files = []File{{Path: filepath.Join(t.TempDir(), "config.toml"), Format: "toml", Content: &raw}, {Path: filepath.Join(t.TempDir(), ".env"), Format: "env"}}
	plan, e := Build(p)
	if e != nil {
		t.Fatal(e)
	}
	d, e := Parse("toml", plan.Changes[0].After)
	if e != nil {
		t.Fatal(e)
	}
	entries := array(d["providers"])
	if len(entries) != 2 || object(entries[0])["name"] != "personal" || object(entries[1])["name"] != "uni-switch" || Get(d, "desktop.theme") != "keep" || !strings.Contains(*plan.Changes[0].After, "# personal comment") {
		t.Fatal(*plan.Changes[0].After)
	}
}

func TestOmpCarriesLegacyProvidersIntoFirstYAML(t *testing.T) {
	dir := t.TempDir()
	raw := `{"providers":{"personal":{"baseUrl":"https://synthetic.invalid","models":[{"id":"keep"}]}}}`
	if e := os.WriteFile(filepath.Join(dir, "models.json"), []byte(raw), 0600); e != nil {
		t.Fatal(e)
	}
	p := fixture("omp")
	p.Protocol = "chat_completions"
	p.Files = nil
	specs, e := Specifications("omp", dir, false, true)
	if e != nil {
		t.Fatal(e)
	}
	if len(specs) != 3 {
		t.Fatal("legacy model migration was omitted", specs)
	}
	for _, s := range specs {
		var content *string
		if filepath.Base(s.Path) == "models.json" {
			content = &raw
		}
		p.Files = append(p.Files, File{Path: s.Path, Format: s.Format, Content: content})
	}
	plan, e := Build(p)
	if e != nil {
		t.Fatal(e)
	}
	models, e := Parse("yaml", plan.Changes[1].After)
	if e != nil {
		t.Fatal(e)
	}
	if Get(models, "providers.personal.baseUrl") != "https://synthetic.invalid" || Get(models, "providers.uni-switch") == nil || *plan.Changes[2].After != raw {
		t.Fatal("legacy providers lost")
	}
}
