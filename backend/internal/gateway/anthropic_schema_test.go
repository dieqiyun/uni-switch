package gateway

import (
	"encoding/json"
	"testing"
)

// A tool offered to an Anthropic-protocol vendor never has anyOf, oneOf or
// allOf at its input_schema's root, which Anthropic refuses: Codex's
// codex_app automation_update has an anyOf of objects (#646).
func TestAnthropicToolSchemaRoot(t *testing.T) {
	for name, schema := range map[string]string{
		"anyOf": `{"anyOf":[{"type":"object","properties":{"mode":{"const":"create"},"name":{"type":"string"}},"required":["mode","name"]},{"type":"object","properties":{"mode":{"const":"delete"},"id":{"type":"string"}},"required":["mode","id"]}]}`,
		"oneOf": `{"type":"object","oneOf":[{"$ref":"#/$defs/a"}],"$defs":{"a":{"type":"object","properties":{"mode":{"type":"string"}},"required":["mode"]}}}`,
		"allOf": `{"allOf":[{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]},{"type":"object","properties":{"b":{"type":"number"}},"required":["b"]}]}`,
	} {
		r := &Request{Tools: []Tool{{Name: "automation_update", Schema: json.RawMessage(schema)}}}
		var out struct {
			Tools []struct {
				InputSchema map[string]any `json:"input_schema"`
			} `json:"tools"`
		}
		if err := json.Unmarshal(buildAnthropic(r, "claude-opus-5-5"), &out); err != nil || len(out.Tools) != 1 {
			t.Fatalf("%s: %v %+v", name, err, out)
		}
		s := out.Tools[0].InputSchema
		for _, k := range []string{"anyOf", "oneOf", "allOf"} {
			if _, ok := s[k]; ok {
				t.Fatalf("%s: %s still at the root: %v", name, k, s)
			}
		}
		props, _ := s["properties"].(map[string]any)
		if s["type"] != "object" || len(props) == 0 {
			t.Fatalf("%s: root %v", name, s)
		}
		req, _ := s["required"].([]any)
		switch name {
		case "anyOf":
			if len(props) != 3 || len(req) != 1 || req[0] != "mode" {
				t.Fatalf("anyOf merged as %v", s)
			}
		case "allOf":
			if len(props) != 2 || len(req) != 2 {
				t.Fatalf("allOf merged as %v", s)
			}
		}
	}
	plain := `{"type":"object","properties":{"x":{"anyOf":[{"type":"string"},{"type":"null"}]}}}`
	r := &Request{Tools: []Tool{{Name: "t", Schema: json.RawMessage(plain)}}}
	var out struct {
		Tools []struct {
			InputSchema json.RawMessage `json:"input_schema"`
		} `json:"tools"`
	}
	json.Unmarshal(buildAnthropic(r, "claude-opus-5-5"), &out)
	if string(out.Tools[0].InputSchema) != plain {
		t.Fatalf("a nested anyOf was changed: %s", out.Tools[0].InputSchema)
	}
}
