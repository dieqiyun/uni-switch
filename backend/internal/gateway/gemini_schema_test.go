package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/tidwall/gjson"
)

// cua-driver's drag tool, as its MCP server listed it before cua's #4888
// (the schema kthink's dsh sent): target is an anyOf of an untyped oneOf
// and null, scope a const with no type. Command Code's
// google/gemini-3.8-flash, served on Vertex, refused every request with
// it: 400 "`mcp__cua-driver__drag` functionDeclaration `parameters.target`
// schema didn't specify the schema type field." A Gemini model is sent
// each untyped schema with its type, on the chat route, the Anthropic
// route and from a Responses body, and the rest of the body as it came;
// a GPT model on the same provider is sent the body byte for byte.
func TestGeminiToolTypesCuaDriverDrag(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := provider.Save(provider.Provider{ID: "cc", Name: "CC", Key: "k", Chat: "https://cc.test/v1", Anthropic: "https://cc.test",
		Models: []string{"google/gemini-3.8-flash", "openai/gpt-5.5"}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/cua_driver_drag_untyped.json")
	if err != nil {
		t.Fatal(err)
	}
	schema := strings.TrimSpace(string(raw))
	want := strings.NewReplacer(
		`"scope":{"const"`, `"scope":{"type":"string","const"`,
		`"target":{"anyOf":[{"description"`, `"target":{"type":"object","anyOf":[{"type":"object","description"`,
	).Replace(schema)
	if want == schema || strings.Count(want, `"type":"object","`) != strings.Count(schema, `"type":"object","`)+2 {
		t.Fatal("the fixture no longer has the shape the test is about")
	}
	var got []byte
	s := New()
	s.client = &http.Client{Transport: countTransport(func(r *http.Request) (*http.Response, error) {
		got, _ = io.ReadAll(r.Body)
		return &http.Response{StatusCode: 400, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"error":{"message":"no","type":"invalid_request_error"}}`))}, nil
	})}
	routes := []struct{ path, tools, at string }{
		{"/v1/chat/completions", `"tools":[{"type":"function","function":{"name":"mcp__cua-driver__drag","parameters":%s}}]`, "tools.0.function.parameters"},
		{"/v1/messages", `"tools":[{"name":"mcp__cua-driver__drag","input_schema":%s}]`, "tools.0.input_schema"},
		{"/v1/responses", `"tools":[{"type":"function","name":"mcp__cua-driver__drag","parameters":%s}]`, ""},
	}
	for _, r := range routes {
		for _, model := range []string{"google/gemini-3.8-flash", "openai/gpt-5.5"} {
			got = nil
			msgs := `"messages":[{"role":"user","content":"hi"}],"max_tokens":16,`
			if r.path == "/v1/responses" {
				msgs = `"input":"hi",`
			}
			body := `{"model":"cc/` + model + `",` + msgs + fmt.Sprintf(r.tools, schema) + `}`
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", r.path, strings.NewReader(body)))
			if got == nil {
				t.Fatalf("%s %s: nothing went upstream: %d %s", r.path, model, rec.Code, rec.Body)
			}
			if !json.Valid(got) {
				t.Fatalf("%s %s: invalid JSON went upstream:\n%s", r.path, model, got)
			}
			sent := ""
			for _, at := range []string{"tools.0.function.parameters", "tools.0.input_schema", "tools.0.parameters"} {
				if v := gjson.GetBytes(got, at); v.Exists() {
					sent = v.Raw
					break
				}
			}
			gemini := strings.HasPrefix(model, "google/")
			switch {
			case gemini && sent != want:
				t.Errorf("%s: Gemini was sent the schema\n%s\nwant\n%s", r.path, sent, want)
			case !gemini && sent != schema:
				t.Errorf("%s: GPT was sent the schema\n%s\nwant it as it came\n%s", r.path, sent, schema)
			}
			if !gemini && r.path != "/v1/responses" {
				// The same route, untranslated: the tools go byte for byte.
				if exp := fmt.Sprintf(r.tools, schema); !bytes.Contains(got, []byte(exp)) {
					t.Errorf("%s: GPT's tools changed:\n%s\nwant\n%s", r.path, got, exp)
				}
			}
		}
	}
}

// Each schema a Gemini model is sent without a type gets the one its
// values, keywords or branches say; one that could be several, a "not"
// and every typed schema are left as they were, and a model no one names
// Gemini gets the body back as it came.
func TestGeminiToolTypes(t *testing.T) {
	params := `{"properties":{` +
		`"any":{},"spaced":{ },"said":{"description":"x"},` +
		`"n":{"enum":[1,2]},"f":{"enum":[1,2.5]},"b":{"const":true},"maybe":{"enum":["a",null]},"mixed":{"enum":["a",1]},` +
		`"list":{"items":{}},"obj":{"properties":{"k":{"type":"string"}}},"pat":{"pattern":"^a"},` +
		`"u":{"anyOf":[{"type":"string"},{"type":"null"}]},"two":{"anyOf":[{"type":"string"},{"type":"integer"}]},` +
		`"num":{"oneOf":[{"type":"integer"},{"type":"number"}]},"r":{"$ref":"#/$defs/D"},"dflt":{"default":3},` +
		`"neg":{"type":"string","not":{"enum":["x"]}},"typed":{"type":"array","items":{"type":"integer"}}},` +
		`"$defs":{"D":{"properties":{}}}}`
	want := strings.NewReplacer(
		`{"properties":{"any":{}`, `{"type":"object","properties":{"any":{"type":"string"}`,
		`"spaced":{ }`, `"spaced":{"type":"string" }`,
		`"said":{`, `"said":{"type":"string",`,
		`"n":{`, `"n":{"type":"integer",`,
		`"f":{`, `"f":{"type":"number",`,
		`"b":{`, `"b":{"type":"boolean",`,
		`"maybe":{`, `"maybe":{"type":"string",`,
		`"list":{"items":{}}`, `"list":{"type":"array","items":{"type":"string"}}`,
		`"obj":{`, `"obj":{"type":"object",`,
		`"pat":{`, `"pat":{"type":"string",`,
		`"u":{`, `"u":{"type":"string",`,
		`"num":{`, `"num":{"type":"number",`,
		`"r":{`, `"r":{"type":"object",`,
		`"dflt":{`, `"dflt":{"type":"integer",`,
		`"D":{`, `"D":{"type":"object",`,
	).Replace(params)
	for _, id := range []string{"gemini-3.8-flash", "google/gemini-3.8-flash", "vertex_ai/Gemini-2.5-pro"} {
		body := []byte(`{"model":"` + id + `","tools":[{"type":"function","function":{"name":"t","parameters":` + params + `}}]}`)
		out := geminiToolTypes(body)
		if !json.Valid(out) {
			t.Fatalf("%s: invalid JSON:\n%s", id, out)
		}
		if got := gjson.GetBytes(out, "tools.0.function.parameters").Raw; got != want {
			t.Errorf("%s: sent\n%s\nwant\n%s", id, got, want)
		}
	}
	for _, id := range []string{"claude-sonnet-4.6", "gpt-5.5", "google/gemma-4", "my-gemini/m", "kimi-k3", ""} {
		body := []byte(`{"model":"` + id + `","tools":[{"type":"function","function":{"name":"t","parameters":` + params + `}}]}`)
		if out := geminiToolTypes(body); !bytes.Equal(out, body) {
			t.Errorf("%s: the body changed:\n%s", id, out)
		}
	}
}
