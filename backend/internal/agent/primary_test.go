package agent

import (
	"reflect"
	"testing"
)

func mutation(p PrimaryPlan, key string) (Mutation, bool) {
	for i := len(p.Mutations) - 1; i >= 0; i-- {
		m := p.Mutations[i]
		joined := ""
		for i, k := range m.Path {
			if i > 0 {
				joined += "."
			}
			joined += k
		}
		if joined == key {
			return m, true
		}
	}
	return Mutation{}, false
}
func TestPrimaryCodexKeepsPersonalEffortAndOwnedFields(t *testing.T) {
	p := PrimaryInput{Client: "codex", Name: "synthetic", Model: "m", BaseURL: "https://example.test/v1", APIKey: "synthetic", AuthMode: "bearer", Config: Object{"model_reasoning_effort": "xhigh", "projects": Object{"personal": true}}, Auth: Object{"tokens": Object{"access_token": "synthetic-official"}}}
	r, e := Primary(p)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := mutation(r, "model_reasoning_effort"); ok {
		t.Fatal("personal effort changed")
	}
	m, _ := mutation(r, "model_providers.uni_switch")
	if object(m.Value)["requires_openai_auth"] != true {
		t.Fatal(m)
	}
	p.Config["profile"] = "active"
	p.Config["profiles"] = Object{"active": Object{"model": "overridden"}}
	if _, e := Primary(p); e == nil {
		t.Fatal("profile conflict ignored")
	}
}
func TestPrimaryClaudeRolesAndDesktopContextAreDeterministic(t *testing.T) {
	p := PrimaryInput{Client: "claude_cli", Model: "default", BaseURL: "https://example.test/custom/v1/", IDs: []string{"claude-3-5-haiku-20241022", "claude-opus-4-6[1m]"}, Config: Object{}}
	r, e := Primary(p)
	if e != nil {
		t.Fatal(e)
	}
	m, _ := mutation(r, "env.ANTHROPIC_DEFAULT_FABLE_MODEL")
	if m.Value != "claude-opus-4-6[1m]" {
		t.Fatal(m)
	}
	m, _ = mutation(r, "env.ANTHROPIC_BASE_URL")
	if m.Value != "https://example.test/custom" {
		t.Fatal(m)
	}
	p.Client = "claude_desktop"
	p.IDs = []string{"claude-opus-4-6", "claude-opus-4-6[1m]"}
	a, e := Primary(p)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := Primary(p)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("plan order unstable")
	}
	m, _ = mutation(a, "inferenceModels")
	models := array(m.Value)
	if len(models) != 1 || object(models[0])["supports1m"] != true {
		t.Fatal(models)
	}
}
func TestPrimaryRejectsMalformedConfigBeforePlanningWrites(t *testing.T) {
	for _, p := range []PrimaryInput{{Client: "claude_cli", Config: Object{"env": "invalid"}}, {Client: "claude_desktop", Kind: "meta", Config: Object{"entries": Object{}}}, {Client: "codex", Config: Object{"desktop": Object{"enabled-reasoning-efforts": []any{"not-a-level"}}}}} {
		if _, e := Primary(p); e == nil {
			t.Fatal(p)
		}
	}
}
