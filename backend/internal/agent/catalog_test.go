package agent

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisteredClientContractsPreservePersonalSettingsAndReapply(t *testing.T) {
	for _, client := range ExtraClients {
		t.Run(client.ID, func(t *testing.T) {
			if client.ID == "zcode" || client.ID == "dsh" || client.ID == "workbuddy" || client.ID == "dsbot" {
				return
			}
			p := fixture(client.ID)
			p.Protocol = client.Protocols[0]
			p.Routed = true
			p.Harness = "opencode"
			if client.Mode == "import" {
				p.BaseURL = "http://127.0.0.1:12345"
				link, e := Build(p)
				if e != nil || link.ImportLink == "" || len(link.Changes) != 0 {
					t.Fatalf("invalid import plan: %+v %v", link, e)
				}
				return
			}
			p.Files = nil
			specs, e := Specifications(client.ID, t.TempDir(), false, true)
			if e != nil {
				t.Fatal(e)
			}
			for _, s := range specs {
				raw := ""
				switch s.Format {
				case "json", "jsonc", "newmax", "alma", "hanako_provider":
					raw = `{"personal":{"key":"keep"},"version":1}`
				case "jsonc_array":
					raw = `[{"name":"personal","vendor":"customendpoint","models":[]}]`
				case "yaml":
					raw = "# 保留注释\npersonal:\n  key: keep\n"
				case "toml":
					raw = "# 保留注释\n[personal]\nkey = 'keep'\n"
				case "env":
					raw = "# 保留注释\nPERSONAL_KEY=keep\n"
				case "hanako_chat":
					raw = `{"provider":"personal","id":"keep"}`
				}
				p.Files = append(p.Files, File{Path: s.Path, Format: s.Format, Content: ptr(raw)})
			}
			plan, e := Build(p)
			if e != nil {
				t.Fatal(e)
			}
			if len(plan.Changes) != len(specs) {
				t.Fatal("incomplete transaction")
			}
			for i, c := range plan.Changes {
				if c.Path != specs[i].Path || p.Files[i].Format != "script" && p.Files[i].Format != "hanako_chat" && !strings.Contains(*c.After, "keep") && p.Client != "vscode" && p.Client != "vscodeinsiders" && p.Client != "vscodium" {
					t.Fatalf("lost personal setting: %s", *c.After)
				}
				if p.Files[i].Format == "jsonc_array" {
					if !strings.Contains(*c.After, "personal") {
						t.Fatal("lost personal group")
					}
				} else if e := Validate(mapFormat(p.Files[i].Format), *c.After); e != nil {
					t.Fatal(e)
				}
				p.Files[i].Content = c.After
			}
			p.Previous = &plan.Changes[0]
			again, e := Build(p)
			if e != nil {
				t.Fatal(e)
			}
			for i, c := range again.Changes {
				if !Same(c.After, plan.Changes[i].After) {
					t.Fatalf("non-idempotent %s\n%s\n%s", filepath.Base(c.Path), *plan.Changes[i].After, *c.After)
				}
			}
		})
	}
}

func mapFormat(format string) string {
	switch format {
	case "newmax", "alma", "hanako_provider", "hanako_chat":
		return "json"
	}
	return format
}

func TestJSONCCommentsAndLiteralDottedKeysSurvive(t *testing.T) {
	raw := "{\n // 中文 comment\n \"chat.defaultModel\":\"old\",\n \"personal\":{\"x\":42},\n}\n"
	f := File{Format: "jsonc", Content: &raw}
	d, e := parseConfig(f)
	if e != nil {
		t.Fatal(e)
	}
	d["chat.defaultModel"] = "new"
	out, e := renderConfig(f, d)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out, "// 中文 comment") || !strings.Contains(out, `"personal":{"x":42}`) {
		t.Fatal(out)
	}
}
