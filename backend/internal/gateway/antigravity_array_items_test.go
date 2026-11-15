package gateway

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// arrayItemsAsk is plugins#375's minimal reproduction, as gcq909893267
// posted it: Claude Desktop's own MCP tools __batch and __guide, each with
// an array property that has no items.
const arrayItemsAsk = `{"model":"antigravity/gemini-3.8-flash","max_tokens":32,"messages":[{"role":"user","content":"reply with OK only"}],
 "tools":[{"name":"art_batch","description":"batch","input_schema":{"type":"object","properties":{"batch":{"type":"array"},"verbose":{"type":"boolean"}}}},
          {"name":"art_guide","description":"read topics","input_schema":{"type":"object","properties":{"items":{"type":"array","description":"topics"}}}}]}`

// Google's 400 to it, the reporter's error line in Google's error shape.
const arrayItems400 = `{"error":{"code":400,"message":"* GenerateContentRequest.tools[0].function_declarations[17].parameters.properties[batch].items: missing field.\n* GenerateContentRequest.tools[0].function_declarations[18].parameters.properties[items].items: missing field.\n","status":"INVALID_ARGUMENT"}}`

// An array with no items goes to Antigravity with items {}, which Google
// takes (the reporter's table: {} and {"type":"string"} both answer 200);
// before, it went as it came and Google turned the whole turn away.
func TestAntigravityArrayGetsItems(t *testing.T) {
	r, err := parseAnthropic([]byte(arrayItemsAsk))
	if err != nil {
		t.Fatal(err)
	}
	env := gjson.ParseBytes(buildCodeAssist(r, "gemini-3.8-flash", "antigravity"))
	decls := env.Get("request.tools.0.functionDeclarations")
	if n := len(decls.Array()); n != 2 {
		t.Fatalf("declarations %s", decls.Raw)
	}
	for i, path := range []string{"parameters.properties.batch", "parameters.properties.items"} {
		node := decls.Get(string(rune('0'+i)) + "." + path)
		if node.Get("type").String() != "array" || node.Get("items").Raw != "{}" {
			t.Errorf("declaration %d %s: %s", i, path, node.Raw)
		}
	}
	if d := decls.Get("1.parameters.properties.items.description").String(); d != "topics" {
		t.Errorf("description lost: %q", d)
	}
}

// Every array plainSchema writes has items, however deep it sits: under
// properties, items, an anyOf/oneOf member, an allOf, a $ref to $defs, a
// type list with null; one that has items keeps its own.
func TestPlainSchemaArraysAllHaveItems(t *testing.T) {
	in := `{"type":"object","$defs":{"Tags":{"type":"array","description":"tags"}},
	"properties":{
		"grid":{"type":"array","items":{"type":"array"}},
		"maybe":{"anyOf":[{"type":"null"},{"type":"array"}]},
		"one":{"oneOf":[{"type":"array","minItems":1},{"type":"string"}]},
		"both":{"allOf":[{"type":"object","properties":{"list":{"type":"array"}}}]},
		"tags":{"$ref":"#/$defs/Tags"},
		"opt":{"type":["array","null"]},
		"kept":{"type":"array","items":{"type":"string"}},
		"deep":{"type":"object","properties":{"rows":{"type":"array","items":{"type":"object","properties":{"cells":{"type":"array"}}}}}}
	}}`
	want := `{"type":"object","properties":{
		"grid":{"type":"array","items":{"type":"array","items":{}}},
		"maybe":{"type":"array","items":{},"nullable":true},
		"one":{"type":"array","items":{},"minItems":1},
		"both":{"type":"object","properties":{"list":{"type":"array","items":{}}}},
		"tags":{"type":"array","items":{},"description":"tags"},
		"opt":{"type":"array","items":{},"nullable":true},
		"kept":{"type":"array","items":{"type":"string"}},
		"deep":{"type":"object","properties":{"rows":{"type":"array","items":{"type":"object","properties":{"cells":{"type":"array","items":{}}}}}}}
	}}`
	var got, exp any
	if err := json.Unmarshal(plainSchema(json.RawMessage(in)), &got); err != nil {
		t.Fatal(err)
	}
	json.Unmarshal([]byte(want), &exp)
	if !reflect.DeepEqual(got, exp) {
		b, _ := json.Marshal(got)
		t.Errorf("got %s", b)
	}
	// a schema that isn't an array gets no items
	if b := plainSchema(json.RawMessage(`{"type":"object","properties":{"s":{"type":"string"}}}`)); strings.Contains(string(b), "items") {
		t.Errorf("items added to a non-array: %s", b)
	}
}
