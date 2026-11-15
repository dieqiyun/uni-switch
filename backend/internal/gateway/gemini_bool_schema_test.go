package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// boolSchemaAsk is han_tiv's case (Discord, v0.1.1982 and v0.1.1989):
// gemini-3.8-flash on Antigravity with MCP tools, one of which has, as its
// fourth property, an array of arrays of anything (items.items true), and
// as its third a property carrying MCP's x-mcp-header (the 2026-07-28
// spec). Google's answers were:
//
//	Invalid value at 'request.tools[0].function_declarations[174].parameters.properties[3].value.items.items' (type.googleapis.com/google.cloud.aiplatform.master.Schema), true
//	Unknown name "x-mcp-header" at 'tools[0].function_declarations[47]...properties[2].value': Cannot find field
const boolSchemaAsk = `{"model":"antigravity/gemini-3.8-flash","max_tokens":32,"messages":[{"role":"user","content":"reply with OK only"}],
 "tools":[{"name":"mcp__sheets__write_rows","description":"write rows","input_schema":{"type":"object","$schema":"https://json-schema.org/draft/2020-12/schema",
   "properties":{
     "spreadsheet":{"type":"string"},
     "sheet":{"type":"string"},
     "region":{"type":"string","x-mcp-header":"Region"},
     "rows":{"type":"array","items":{"type":"array","items":true},"description":"rows of cells"}
   },"required":["spreadsheet","rows"],"additionalProperties":false}}]}`

// geminiSchemaFields is what Google's Schema object holds
// (ai.google.dev/api/caching#Schema); a key outside it is "Cannot find
// field".
var geminiSchemaFields = map[string]bool{
	"type": true, "format": true, "title": true, "description": true, "nullable": true, "enum": true,
	"maxItems": true, "minItems": true, "properties": true, "required": true, "minProperties": true,
	"maxProperties": true, "minLength": true, "maxLength": true, "pattern": true, "example": true,
	"anyOf": true, "propertyOrdering": true, "default": true, "items": true, "minimum": true, "maximum": true,
}

// geminiSchemaFaults lists where s, a Gemini Schema, has a node that is no
// object or a key the Schema has no field for.
func geminiSchemaFaults(s gjson.Result, at string) []string {
	if !s.IsObject() {
		return []string{at + " = " + s.Raw}
	}
	var bad []string
	s.ForEach(func(k, v gjson.Result) bool {
		key := k.String()
		switch {
		case !geminiSchemaFields[key]:
			bad = append(bad, at+"."+key)
		case key == "items":
			bad = append(bad, geminiSchemaFaults(v, at+".items")...)
		case key == "properties":
			v.ForEach(func(n, p gjson.Result) bool {
				bad = append(bad, geminiSchemaFaults(p, at+".properties["+n.String()+"]")...)
				return true
			})
		case key == "anyOf":
			v.ForEach(func(i, p gjson.Result) bool {
				bad = append(bad, geminiSchemaFaults(p, at+".anyOf["+i.String()+"]")...)
				return true
			})
		}
		return true
	})
	return bad
}

func TestAntigravityBooleanSchemaAndXKeys(t *testing.T) {
	r, err := parseAnthropic([]byte(boolSchemaAsk))
	if err != nil {
		t.Fatal(err)
	}
	env := gjson.ParseBytes(buildCodeAssist(r, "gemini-3.8-flash", "antigravity"))
	params := env.Get("request.tools.0.functionDeclarations.0.parameters")
	if bad := geminiSchemaFaults(params, "parameters"); len(bad) > 0 {
		t.Fatalf("Google would refuse %v in %s", bad, params.Raw)
	}
	rows := params.Get("properties.rows")
	if rows.Get("type").String() != "array" || rows.Get("items.type").String() != "array" || rows.Get("items.items").Raw != "{}" {
		t.Errorf("rows: %s", rows.Raw)
	}
	if params.Get("properties.region.type").String() != "string" {
		t.Errorf("region: %s", params.Get("properties.region").Raw)
	}
}

// Every place JSON Schema takes a schema takes a boolean too, and every
// level may carry an x- keyword; none of them reaches Google, and a
// property named x-… is a name, not a keyword, so it stays.
func TestPlainSchemaBooleansAndXKeysEverywhere(t *testing.T) {
	in := `{"type":"object","x-root":1,"$defs":{"Any":true,"None":false,"Cell":{"type":"array","items":true,"x-def":"d"}},
	"properties":{
		"any":true,
		"gone":false,
		"x-region":{"type":"string","x-mcp-header":"Region"},
		"grid":{"type":"array","items":{"type":"array","items":{"type":"array","items":true}}},
		"empty":{"type":"array","items":false},
		"ref":{"$ref":"#/$defs/Any"},
		"cell":{"$ref":"#/$defs/Cell"},
		"never":{"$ref":"#/$defs/None"},
		"either":{"anyOf":[true,{"type":"null"}]},
		"first":{"anyOf":[false,{"type":"integer","x-k":1}]},
		"merged":{"allOf":[true,{"type":"object","properties":{"a":true,"b":{"type":"string","x-b":2}}}]},
		"obj":{"type":"object","properties":{"deep":{"type":"array","items":{"type":"object","properties":{"v":true},"x-n":{"a":1}}}}}
	},"required":["any","gone","never","x-region"]}`
	out := plainSchema(json.RawMessage(in))
	g := gjson.ParseBytes(out)
	if bad := geminiSchemaFaults(g, "parameters"); len(bad) > 0 {
		t.Fatalf("Google would refuse %v in %s", bad, out)
	}
	for _, p := range []string{"properties.gone", "properties.never"} {
		if g.Get(p).Exists() {
			t.Errorf("%s (false) kept: %s", p, g.Get(p).Raw)
		}
	}
	if req := g.Get("required").Raw; strings.Contains(req, "gone") || strings.Contains(req, "never") || !strings.Contains(req, "x-region") {
		t.Errorf("required %s", req)
	}
	if g.Get(`properties.x-region.type`).String() != "string" {
		t.Errorf("x-region property lost: %s", out)
	}
	if e := g.Get("properties.empty"); e.Get("items").Raw != "{}" || e.Get("maxItems").Int() != 0 || !e.Get("maxItems").Exists() {
		t.Errorf("items false: %s", e.Raw)
	}
	if g.Get("properties.grid.items.items.items").Raw != "{}" {
		t.Errorf("grid: %s", g.Get("properties.grid").Raw)
	}
	if g.Get("properties.first.type").String() != "integer" {
		t.Errorf("first: %s", g.Get("properties.first").Raw)
	}
}

// The same tools sent on to whoever turns them into Vertex's Schema (a
// relay's or plugin's gemini-3.8-flash) go without booleans or x-
// keywords, a property named x-… and additionalProperties kept, and a
// schema with neither goes byte for byte.
func TestGeminiToolTypesDropsBooleansAndXKeys(t *testing.T) {
	body := []byte(`{"model":"google/gemini-3.8-flash","tools":[{"type":"function","function":{"name":"w","parameters":{"type":"object",
	"properties":{"x-region":{"type":"string","x-mcp-header":"Region"},"rows":{"type":"array","items":{"type":"array","items":true}},
	"gone":false,"u":{"anyOf":[false,{"type":"string"}]}},"additionalProperties":false}}},
	{"type":"function","function":{"name":"plain","parameters":{"type":"object",  "properties":{"a":{"type":"string"}}}}}]}`)
	out := geminiToolTypes(body)
	if !json.Valid(out) {
		t.Fatalf("invalid: %s", out)
	}
	p := gjson.GetBytes(out, "tools.0.function.parameters")
	if strings.Contains(p.Raw, "x-mcp-header") || strings.Contains(p.Raw, "true") || p.Get("properties.gone").Exists() {
		t.Errorf("left in: %s", p.Raw)
	}
	if p.Get("properties.x-region.type").String() != "string" || p.Get("additionalProperties").Raw != "false" {
		t.Errorf("lost: %s", p.Raw)
	}
	if it := p.Get("properties.rows.items.items"); !it.IsObject() {
		t.Errorf("items.items: %s", it.Raw)
	}
	if u := p.Get("properties.u.anyOf").Array(); len(u) != 1 {
		t.Errorf("u: %s", p.Get("properties.u").Raw)
	}
	if !strings.Contains(string(out), `{"type":"object",  "properties":{"a":{"type":"string"}}}`) {
		t.Errorf("an untouched schema was written again: %s", out)
	}
	// another model's body goes as it came
	other := []byte(strings.Replace(string(body), "google/gemini-3.8-flash", "gpt-5.5", 1))
	if string(geminiToolTypes(other)) != string(other) {
		t.Error("a non-Gemini body was changed")
	}
}
