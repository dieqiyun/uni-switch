package agent

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

func TestT3LaunchArgsSurviveClientTokenizationAndCodexTOML(t *testing.T) {
	p := fixture("t3code")
	p.Protocol = "messages"
	p.Routed = true
	p.Name = "中文 supplier \"quoted\" $dollar `tick` \\path\n\a\x7f"
	p.APIKey = "synthetic\\key\"' $value `literal`"
	p.Files = []File{{Path: filepath.Join(t.TempDir(), "settings.json"), Format: "json"}, {Path: filepath.Join(t.TempDir(), "模型 catalog.json"), Format: "json"}}
	plan, e := Build(p)
	if e != nil {
		t.Fatal(e)
	}
	d, e := Parse("json", plan.Changes[0].After)
	if e != nil {
		t.Fatal(e)
	}
	if Get(d, "providerInstances.uni-switch.driver") != "claudeAgent" || Get(d, "providerInstances.uni-switch-codex.driver") != "codex" {
		t.Fatal("independent instances missing")
	}
	args := text(Get(d, "providerInstances.uni-switch-codex.config.launchArgs"))
	// T3's tokenizer unquotes double quoted arguments and consumes only
	// backslash escapes for double quote, backslash, dollar and backtick.
	pattern := regexp.MustCompile(`-c "((?:\\.|[^"\\])*)"`)
	escape := regexp.MustCompile("\\\\([\"\\\\$`])")
	assignments := []string{}
	rest := args
	for _, match := range pattern.FindAllStringSubmatch(args, -1) {
		assignments = append(assignments, escape.ReplaceAllString(match[1], "$1"))
		rest = strings.Replace(rest, match[0], "", 1)
	}
	if len(assignments) != 7 || strings.TrimSpace(rest) != "" {
		t.Fatal("invalid launch argument boundaries")
	}
	config, e := edit.TOML(strings.Join(assignments, "\n"))
	if e != nil {
		t.Fatal("Codex could not parse overrides", e)
	}
	if Get(config, "model_providers.uni-switch.name") != p.Name || Get(config, "model_providers.uni-switch.experimental_bearer_token") != p.APIKey || config["model_catalog_json"] != p.Files[1].Path || config["model"] != p.Model {
		t.Fatal("quoted arguments changed their values")
	}
}
