package agent

import (
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/modelmeta"
	"strings"
	"testing"
)

func ptr(s string) *string { return &s }
func fixture(client string) Input {
	p := Input{Client: client, Name: "synthetic supplier", Model: "test-model", Models: []gateway.Model{{ID: "test-model", Enabled: true, Metadata: &modelmeta.Resolved{Profile: Object{"contextWindow": 65536, "toolCalls": false, "inputModalities": []string{"text"}}, Capabilities: Object{"imageInput": false}, PublishedOutput: 4096}}}, Protocol: "chat_completions", Upstream: "openai", BaseURL: "https://example.test/custom/v4", APIKey: "synthetic", AuthMode: "bearer", Files: []File{{Path: "synthetic.json", Format: "json"}}}
	if client == "dsh" {
		p.Files = []File{{Path: "patch.yml", Format: "yaml"}, {Path: "credentials.yml", Format: "yaml"}}
	}
	return p
}
func TestZcodePreservesOtherRulesAndIsIdempotent(t *testing.T) {
	p := fixture("zcode")
	p.Files[0].Content = ptr(`{"schemaVersion":1,"config":{"providerConfigRules":{"providerRules":[{"providerId":"personal","other":true}]}}}`)
	plan, e := Build(p)
	if e != nil {
		t.Fatal(e)
	}
	doc, e := jsonObject(plan.Changes[0].After)
	if e != nil {
		t.Fatal(e)
	}
	config := object(doc["config"])
	rules := array(object(config["providerConfigRules"])["providerRules"])
	if len(rules) != 2 || object(rules[0])["providerId"] != "personal" {
		t.Fatal(doc)
	}
	p.Files[0].Content = plan.Changes[0].After
	again, e := Build(p)
	if e != nil || *again.Changes[0].After != *plan.Changes[0].After {
		t.Fatal("non-idempotent", e)
	}
}
func TestDshPreservesTaggedPatchAndCredentialRecords(t *testing.T) {
	p := fixture("dsh")
	original := "# 中文\n- id: custom\n  config: !!js |\n    ({enabled:true})\n"
	p.Files[0].Content = &original
	p.Files[1].Content = ptr("version: 1\nrefs:\n  KEEP: existing\nrecords: {}\n")
	plan, e := Build(p)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(*plan.Changes[0].After, original) || !strings.Contains(*plan.Changes[1].After, "KEEP: existing") {
		t.Fatal(plan)
	}
	for i := range p.Files {
		p.Files[i].Content = plan.Changes[i].After
	}
	again, e := Build(p)
	if e != nil {
		t.Fatal(e)
	}
	for i, c := range again.Changes {
		if *c.After != *plan.Changes[i].After {
			t.Fatal("non-idempotent", i)
		}
	}
}
func TestWorkbuddyDoesNotInheritOldCapabilities(t *testing.T) {
	p := fixture("workbuddy")
	p.Files[0].Content = ptr(`{"models":[{"id":"personal","custom":42}],"availableModels":["personal"],"other":true}`)
	first, e := Build(p)
	if e != nil {
		t.Fatal(e)
	}
	p.Files[0].Content = first.Changes[0].After
	p.Previous = first.WorkbuddyRoutes
	p.Models[0].Metadata.Profile = Object{}
	p.Models[0].Metadata.Capabilities = Object{}
	p.Models[0].Metadata.PublishedOutput = 0
	second, e := Build(p)
	if e != nil {
		t.Fatal(e)
	}
	doc, _ := jsonObject(second.Changes[0].After)
	models := array(doc["models"])
	if len(models) != 2 || object(models[0])["custom"] == nil || doc["other"] != true {
		t.Fatal(doc)
	}
	for _, key := range []string{"maxInputTokens", "maxOutputTokens", "supportsToolCall", "supportsImages"} {
		if object(models[1])[key] != nil {
			t.Fatal("inherited", key, doc)
		}
	}
}
func TestSchemaAuthAndProtocolRejectBeforePlan(t *testing.T) {
	p := fixture("workbuddy")
	p.AuthMode = "x-api-key"
	if _, e := Build(p); e == nil {
		t.Fatal("auth ignored")
	}
	p = fixture("zcode")
	p.Files[0].Content = ptr(`{"schemaVersion":2}`)
	if _, e := Build(p); e == nil {
		t.Fatal("schema ignored")
	}
	p = fixture("dsh")
	p.Files[1].Content = ptr("version: 2\nrefs: {}\nrecords: {}\n")
	if _, e := Build(p); e == nil {
		t.Fatal("credentials schema ignored")
	}
}
