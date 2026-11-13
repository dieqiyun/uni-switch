package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/tidwall/gjson"
)

// A tool schema with an enum and no type goes to Kimi with the type its
// values have: Kimi Code refused cua-driver's parse_visual_regions, 400
// "not a valid moonshot flavored json schema … At path
// 'properties.options.properties.kinds.anyOf.items': type is not defined"
// (#886), and the whole request failed. Values of mixed types, a schema
// with a type, a property called enum and the rest of the body are left as
// they were; a null among one type's values gives that type and "null",
// the type array Kimi takes beside an enum. Anyone else is sent the tools
// unchanged.
func TestKimiToolEnumTypes(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	kimi, err := provider.FromPreset("kimi-code-cn")
	if err != nil {
		t.Fatal(err)
	}
	kimi.ID, kimi.Key, kimi.Models = "kimi", "k", []string{"m"}
	for _, p := range []provider.Provider{kimi,
		{ID: "other", Name: "Other", Key: "k", Chat: "https://other.test/v1", Models: []string{"m"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]string{}
	s := New()
	s.client = &http.Client{Transport: countTransport(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		got[r.URL.Host] = string(b)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"id":"c","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))}, nil
	})}
	tools := `"tools":[{"type":"function","function":{"name":"parse_visual_regions","parameters":{"type":"object","properties":{"options":{"type":"object","properties":{` +
		`"kinds":{"anyOf":[{"items":{"enum":["text","icon"]},"maxItems":2,"type":"array"},{"type":"null"}]},` +
		`"level":{"enum":[1,2,3]},"scale":{"enum":[1,1.5]},"on":{"const":true},` +
		`"mixed":{"enum":["a",1]},"maybe":{"enum":["a",null]},"typed":{"type":"string","enum":["x"]},` +
		`"enum":{"type":"string"}}}},"$defs":{"mode":{ "enum" : ["fast","slow"]}}}}}]`
	for _, id := range []string{"kimi", "other"} {
		body := `{"model":"` + id + `/m","messages":[{"role":"user","content":"hi"}],` + tools + `}`
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", id, rec.Code, rec.Body)
		}
	}
	want := strings.NewReplacer(
		`"items":{"enum"`, `"items":{"type":"string","enum"`,
		`"level":{"enum"`, `"level":{"type":"integer","enum"`,
		`"scale":{"enum"`, `"scale":{"type":"number","enum"`,
		`"on":{"const"`, `"on":{"type":"boolean","const"`,
		`"mode":{ "enum"`, `"mode":{"type":"string", "enum"`,
		`"maybe":{"enum"`, `"maybe":{"type":["string","null"],"enum"`,
	).Replace(tools)
	b := got["api.kimi.com"]
	if !strings.Contains(b, want) {
		t.Errorf("Kimi was sent:\n%s\nwant its tools:\n%s", b, want)
	}
	if !json.Valid([]byte(b)) {
		t.Errorf("Kimi was sent invalid JSON:\n%s", b)
	}
	if b := got["other.test"]; !strings.Contains(b, tools) {
		t.Errorf("another provider was sent:\n%s", b)
	}
}

// A tool schema with items and a string enum goes to Kimi typed from
// its items, an array, not from its values, a string: the node was read
// from its enum first and went on with items beside a string, as a
// Gemini one did before geminiSchemaType read its keywords first
// (d71362af; yetone, #3867). A $ref's target is typed the same way. An
// enum with no items or properties is still typed from its values.
func TestKimiToolEnumTypesItemsBeforeEnum(t *testing.T) {
	kimi, err := provider.FromPreset("kimi-code-cn")
	if err != nil {
		t.Fatal(err)
	}
	ask := `{"model":"kimi/m","tools":[{"type":"function","function":{"name":"show","description":"show rows","parameters":{"type":"object","properties":{` +
		`"display":{"description":"columns to display","items":{"type":"string"},"enum":["name","size","date"]},` +
		`"filter":{"description":"a filter","properties":{"q":{"type":"string"}},"const":"x"},` +
		`"limit":{"type":"integer"},"m":{"enum":["a","b"]}}}}}]}`

	out := kimiToolEnumTypes(kimi, provider.Chat, []byte(ask))
	if !json.Valid(out) {
		t.Fatalf("Kimi was sent invalid JSON: %s", out)
	}
	props := gjson.GetBytes(out, "tools.0.function.parameters.properties")
	if got := props.Get("display.type").String(); got != "array" {
		t.Errorf("display typed %q, want array: %s", got, props.Get("display").Raw)
	}
	if it := props.Get("display.items"); !it.Exists() || it.Get("type").String() != "string" {
		t.Errorf("display's items: %s", it.Raw)
	}
	if got := props.Get("filter.type").String(); got != "object" {
		t.Errorf("filter typed %q, want object: %s", got, props.Get("filter").Raw)
	}
	if got := props.Get("m.type").String(); got != "string" {
		t.Errorf("an enum without items typed %q, want string", got)
	}

	ref := `{"model":"kimi/m","tools":[{"type":"function","function":{"name":"show","parameters":{"type":"object","properties":{"a":{"$ref":"#/$defs/choice"}},"$defs":{"choice":{"description":"a choice","items":{"type":"string"},"enum":["a","b"]}}}}}]}`
	out = kimiToolRefTypes(kimi, provider.Chat, []byte(ref))
	if !json.Valid(out) {
		t.Fatalf("Kimi was sent invalid JSON: %s", out)
	}
	choice := gjson.GetBytes(out, "tools.0.function.parameters.$defs.choice")
	if got := choice.Get("type").String(); got != "array" {
		t.Errorf("the $ref's target typed %q, want array: %s", got, choice.Raw)
	}
	if it := choice.Get("items"); !it.Exists() || it.Get("type").String() != "string" {
		t.Errorf("the target's items: %s", it.Raw)
	}
}

// A schema a tool's $ref reaches goes to Kimi with a type: Kimi expands
// the $ref and refuses a target with none, 400 "At path 'properties.b':
// detected infinite recursion without termination condition", and Codex on
// k3 failed outright (chaosy on Discord). Codex sends zod's $defs with a
// oneOf or allOf def untyped, as captured from Codex 0.162.1 (b, c); raw
// clients send defs typed by nothing but their keywords, or by nothing at
// all. Each goes on the chat route, the Anthropic route and from Codex's
// Responses; a {} def, an anyOf (Kimi refuses a type beside one), a typed
// def, an unreferenced one and the rest of the body are left as they were,
// and anyone else is sent the tools unchanged.
func TestKimiToolRefTypes(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	kimi, err := provider.FromPreset("kimi-code-cn")
	if err != nil {
		t.Fatal(err)
	}
	kimi.ID, kimi.Key, kimi.Models = "kimi", "k", []string{"m"}
	for _, p := range []provider.Provider{kimi,
		{ID: "other", Name: "Other", Key: "k", Chat: "https://other.test/v1", Anthropic: "https://other.test", Models: []string{"m"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]string{}
	s := New()
	s.client = &http.Client{Transport: countTransport(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		got[r.URL.Host+r.URL.Path] = string(b)
		return &http.Response{StatusCode: 400, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"error":{"message":"no","type":"invalid_request_error"}}`))}, nil
	})}
	schema := `{"type":"object","properties":{"a":{"$ref":"#/$defs/__schema0"},"b":{"$ref":"#/$defs/__schema1"},` +
		`"c":{"items":{"$ref":"#/$defs/__schema2"},"type":"array"},"d":{"anyOf":[{"$ref":"#/$defs/P"},{"type":"null"}]},` +
		`"e":{"$ref":"#/$defs/M"},"f":{"$ref":"#/definitions/D"},"g":{"$ref":"#/$defs/R"},"h":{"$ref":"#/$defs/U"},"i":{"$ref":"#/$defs/S"}},` +
		`"$defs":{"__schema0":{},"__schema1":{"oneOf":[{"type":"string"},{"type":"number"}]},"__schema2":{"allOf":[{"type":"string"}]},` +
		`"P":{"properties":{"x":{"type":"string"}}},"M":{"minLength":1},"R":{"oneOf":[{"$ref":"#/$defs/P"},{"$ref":"#/definitions/D"}]},` +
		`"U":{"anyOf":[{"type":"string"},{"$ref":"#/$defs/U"}]},"S":{"type":"string","enum":["x"]},"V":{"oneOf":[{"type":"string"}]}},` +
		`"definitions":{"D":{"description":"anything"}}}`
	anything := `["string","number","integer","boolean","object","array","null"]`
	want := strings.NewReplacer(
		`"__schema1":{`, `"__schema1":{"type":["string","number"],`,
		`"__schema2":{`, `"__schema2":{"type":"string",`,
		`"P":{`, `"P":{"type":"object",`,
		`"M":{`, `"M":{"type":"string",`,
		`"R":{`, `"R":{"type":`+anything+`,`,
		`"D":{`, `"D":{"type":`+anything+`,`,
	).Replace(schema)
	routes := []struct{ path, tools, upstream, otherUp string }{
		{"/v1/chat/completions", `"tools":[{"type":"function","function":{"name":"automation_update","parameters":%s}}]`, "api.kimi.com/coding/v1/chat/completions", "other.test/v1/chat/completions"},
		{"/v1/messages", `"tools":[{"name":"automation_update","input_schema":%s}]`, "api.kimi.com/coding/v1/messages", "other.test/v1/messages"},
		{"/v1/responses", `"tools":[{"type":"function","name":"automation_update","parameters":%s}]`, "api.kimi.com/coding/v1/chat/completions", "other.test/v1/chat/completions"},
	}
	for _, r := range routes {
		for _, id := range []string{"kimi", "other"} {
			clear(got)
			msgs := `"messages":[{"role":"user","content":"hi"}],"max_tokens":16,`
			if r.path == "/v1/responses" {
				msgs = `"input":"hi",`
			}
			body := `{"model":"` + id + `/m",` + msgs + strings.Replace(r.tools, "%s", schema, 1) + `}`
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", r.path, strings.NewReader(body)))
			up := r.upstream
			if id == "other" {
				up = r.otherUp
			}
			b, ok := got[up]
			if !ok {
				t.Fatalf("%s %s: nothing sent to %s, only %v (%d %s)", r.path, id, up, got, rec.Code, rec.Body)
			}
			if !json.Valid([]byte(b)) {
				t.Fatalf("%s %s: invalid JSON sent:\n%s", r.path, id, b)
			}
			sent := gjson.Get(b, "tools.0.function.parameters")
			if !sent.Exists() {
				sent = gjson.Get(b, "tools.0.input_schema")
			}
			w := schema
			if id == "kimi" {
				w = want
			}
			if sent.Raw != w {
				t.Errorf("%s %s was sent:\n%s\nwant:\n%s", r.path, id, sent.Raw, w)
			}
		}
	}
}

// A $ref whose target is itself a $ref goes without a type beside it:
// Moonshot refuses one there, 400 "At path '$defs.__schema14': when using
// $ref, type should be defined in the referenced schema instead of the
// parent schema" (#3205), and the whole request fails. Codex writes such
// defs beside what it changed of a reused one — its automation_update's
// targetThreadId reaches {"format":"uuid","description":"tt","$ref":
// "#/$defs/__schema1"} (the fixture is what ChatGPT.app 26.930's zod
// builds) — so the chain is resolved to the final target and the type, if
// one is due, is written there; __schema1 has a type, so the def goes as
// Codex sent it.
func TestKimiToolRefTypesRefChain(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	kimi, err := provider.FromPreset("kimi-code-cn")
	if err != nil {
		t.Fatal(err)
	}
	kimi.ID, kimi.Key, kimi.Models = "kimi", "k", []string{"m"}
	if err := provider.Save(kimi); err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile("../provider/testdata/codex_automation_update_schema.json")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	s := New()
	s.client = &http.Client{Transport: countTransport(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		got[r.URL.Host+r.URL.Path] = string(b)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"id":"c","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))}, nil
	})}
	body := `{"model":"kimi/m","messages":[{"role":"user","content":"hi"}],` +
		`"tools":[{"type":"function","function":{"name":"mcp__codex_app__automation_update","parameters":` + string(schema) + `}}]}`
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	b := got["api.kimi.com/coding/v1/chat/completions"]
	if b == "" {
		t.Fatal("nothing was sent to Kimi")
	}
	if !json.Valid([]byte(b)) {
		t.Fatalf("Kimi was sent invalid JSON:\n%s", b)
	}
	sent := gjson.Get(b, "tools.0.function.parameters")
	if !sent.Exists() {
		t.Fatalf("no parameters were sent:\n%s", b)
	}
	// Moonshot's check, as an assertion: nothing it refuses may be left —
	// no type beside a $ref anywhere in the schema as it goes.
	var besideRef []string
	walkSchema(sent, func(s gjson.Result) {
		if s.Get("type").Exists() && s.Get(`\$ref`).Type == gjson.String {
			besideRef = append(besideRef, s.Raw)
		}
	})
	if len(besideRef) > 0 {
		t.Errorf("Kimi was sent a type beside a $ref, refused 400 \"when using $ref, type should be defined in the referenced schema instead of the parent schema\":\n%s",
			strings.Join(besideRef, "\n"))
	}
	if def := sent.Get(`$defs.__schema14`); !def.Exists() {
		t.Error("Codex's format-and-$ref def is gone")
	} else {
		var buf bytes.Buffer
		if err := json.Compact(&buf, []byte(def.Raw)); err != nil {
			t.Fatal(err)
		}
		if buf.String() != `{"format":"uuid","description":"tt","$ref":"#/$defs/__schema1"}` {
			t.Errorf("Codex's format-and-$ref def was rewritten:\n%s", def.Raw)
		}
	}
}

// A request to a model whose server locks temperature and top_p — Kimi
// Code's kimi-for-coding and k3*, kimi-k2.5 and later on either Kimi
// platform — goes without them: the whitelist refuses the ones clients
// send (Copilot's temperature 0.1, top_p 1, 400 "invalid temperature:
// only 1 is allowed for this model"), and left out the server fills in
// what the current model and mode take. A request that never named them
// stays without them; a model that takes a range (moonshot-v1-8k,
// kimi-k2-0905-preview, kimi-k2-thinking) keeps the user's own
// temperature; a relay's vendor-prefixed name (moonshotai/kimi-k2.6)
// still names the locked model; and anyone else is sent the body as it
// was.
func TestKimiLockedSampling(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	kimi, err := provider.FromPreset("kimi-code-cn")
	if err != nil {
		t.Fatal(err)
	}
	kimi.ID, kimi.Key, kimi.Models = "kimi", "k", []string{"kimi-for-coding", "k3"}
	moonshot, err := provider.FromPreset("moonshot-cn")
	if err != nil {
		t.Fatal(err)
	}
	moonshot.ID, moonshot.Key = "moonshot", "k"
	moonshot.Models = []string{"moonshot-v1-8k", "kimi-k2.5", "kimi-k2.6", "kimi-k2-0905-preview", "kimi-k2-thinking", "moonshotai/kimi-k2.6"}
	volc, err := provider.FromPreset("volcengine")
	if err != nil {
		t.Fatal(err)
	}
	volc.ID, volc.Key, volc.Models = "volc", "k", []string{"kimi-k3.1-pro", "kimi-k2.8-preview", "doubao-seed-2.1-pro"}
	arkpay := provider.Provider{ID: "arkpay", Name: "Ark pay", Key: "k", Chat: "https://ark.ap-southeast.volces.com/api/v3", Models: []string{"kimi-k3"}}
	other := provider.Provider{ID: "other", Name: "Other", Key: "k", Chat: "https://other.test/v1", Models: []string{"m"}}
	for _, p := range []provider.Provider{kimi, moonshot, volc, arkpay, other} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	type sentBody struct{ host, path, body string }
	var sent []sentBody
	s := New()
	s.client = &http.Client{Transport: countTransport(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		sent = append(sent, sentBody{r.URL.Host, r.URL.Path, string(b)})
		if r.URL.Path == "/v1/messages" {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))}, nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"id":"c","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))}, nil
	})}
	post := func(t *testing.T, path, body string) sentBody {
		t.Helper()
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		return sent[len(sent)-1]
	}
	// the request VS Code Copilot's chat sends, refused 400 by Kimi Code
	t.Run("refused values are dropped", func(t *testing.T) {
		got := post(t, "/v1/chat/completions", `{"model":"kimi/kimi-for-coding","messages":[{"role":"user","content":"hi"}],"temperature":0.1,"top_p":1,"max_tokens":5}`)
		if gjson.Get(got.body, "temperature").Exists() || gjson.Get(got.body, "top_p").Exists() {
			t.Errorf("Kimi Code was sent the sampling fields: %s", got.body)
		}
		if !json.Valid([]byte(got.body)) {
			t.Errorf("Kimi Code was sent invalid JSON: %s", got.body)
		}
	})
	t.Run("absent stays absent", func(t *testing.T) {
		got := post(t, "/v1/chat/completions", `{"model":"kimi/kimi-for-coding","messages":[{"role":"user","content":"hi"}],"max_tokens":5}`)
		if gjson.Get(got.body, "temperature").Exists() || gjson.Get(got.body, "top_p").Exists() {
			t.Errorf("sampling fields were added: %s", got.body)
		}
	})
	t.Run("accepted values are dropped too", func(t *testing.T) {
		got := post(t, "/v1/chat/completions", `{"model":"kimi/kimi-for-coding","messages":[{"role":"user","content":"hi"}],"temperature":1,"top_p":0.95}`)
		if gjson.Get(got.body, "temperature").Exists() || gjson.Get(got.body, "top_p").Exists() {
			t.Errorf("Kimi Code was sent the sampling fields: %s", got.body)
		}
	})
	t.Run("kimi code's k3 drops the fields", func(t *testing.T) {
		got := post(t, "/v1/chat/completions", `{"model":"kimi/k3","messages":[{"role":"user","content":"hi"}],"temperature":0.1,"top_p":1}`)
		if got.host != "api.kimi.com" {
			t.Fatalf("the request went to %s", got.host)
		}
		if gjson.Get(got.body, "temperature").Exists() || gjson.Get(got.body, "top_p").Exists() {
			t.Errorf("k3 was sent the sampling fields: %s", got.body)
		}
	})
	t.Run("anthropic endpoint", func(t *testing.T) {
		got := post(t, "/v1/messages", `{"model":"kimi/kimi-for-coding","messages":[{"role":"user","content":"hi"}],"temperature":0.1,"top_p":1,"max_tokens":5}`)
		if got.host != "api.kimi.com" {
			t.Fatalf("the request went to %s", got.host)
		}
		if gjson.Get(got.body, "temperature").Exists() || gjson.Get(got.body, "top_p").Exists() {
			t.Errorf("Kimi Code was sent the sampling fields: %s", got.body)
		}
	})
	t.Run("moonshot pay-as-you-go kimi-k2.6 drops the fields", func(t *testing.T) {
		got := post(t, "/v1/chat/completions", `{"model":"moonshot/kimi-k2.6","messages":[{"role":"user","content":"hi"}],"temperature":0.1,"top_p":1}`)
		if got.host != "api.moonshot.cn" {
			t.Fatalf("the request went to %s", got.host)
		}
		if gjson.Get(got.body, "temperature").Exists() || gjson.Get(got.body, "top_p").Exists() {
			t.Errorf("kimi-k2.6 was sent the sampling fields: %s", got.body)
		}
	})
	t.Run("moonshot kimi-k2.5, the first locked version, drops the fields", func(t *testing.T) {
		got := post(t, "/v1/chat/completions", `{"model":"moonshot/kimi-k2.5","messages":[{"role":"user","content":"hi"}],"temperature":0.1,"top_p":1}`)
		if got.host != "api.moonshot.cn" {
			t.Fatalf("the request went to %s", got.host)
		}
		if gjson.Get(got.body, "temperature").Exists() || gjson.Get(got.body, "top_p").Exists() {
			t.Errorf("kimi-k2.5 was sent the sampling fields: %s", got.body)
		}
	})
	t.Run("a relay's vendor-prefixed name still names the locked model", func(t *testing.T) {
		got := post(t, "/v1/chat/completions", `{"model":"moonshot/moonshotai/kimi-k2.6","messages":[{"role":"user","content":"hi"}],"temperature":0.1,"top_p":1}`)
		if got.host != "api.moonshot.cn" {
			t.Fatalf("the request went to %s", got.host)
		}
		if gjson.Get(got.body, "temperature").Exists() || gjson.Get(got.body, "top_p").Exists() {
			t.Errorf("moonshotai/kimi-k2.6 was sent the sampling fields: %s", got.body)
		}
	})
	t.Run("moonshot pay-as-you-go keeps the user's temperature", func(t *testing.T) {
		got := post(t, "/v1/chat/completions", `{"model":"moonshot/moonshot-v1-8k","messages":[{"role":"user","content":"hi"}],"temperature":0.1,"top_p":1}`)
		if got.host != "api.moonshot.cn" {
			t.Fatalf("the request went to %s", got.host)
		}
		if v := gjson.Get(got.body, "temperature"); !v.Exists() || v.Raw != "0.1" {
			t.Errorf("Moonshot's own temperature was not kept: %s", got.body)
		}
	})
	t.Run("moonshot kimi-k2-0905-preview keeps the user's temperature", func(t *testing.T) {
		got := post(t, "/v1/chat/completions", `{"model":"moonshot/kimi-k2-0905-preview","messages":[{"role":"user","content":"hi"}],"temperature":0.1}`)
		if got.host != "api.moonshot.cn" {
			t.Fatalf("the request went to %s", got.host)
		}
		if v := gjson.Get(got.body, "temperature"); !v.Exists() || v.Raw != "0.1" {
			t.Errorf("kimi-k2-0905-preview's temperature was not kept: %s", got.body)
		}
	})
	t.Run("moonshot kimi-k2-thinking keeps the user's temperature", func(t *testing.T) {
		got := post(t, "/v1/chat/completions", `{"model":"moonshot/kimi-k2-thinking","messages":[{"role":"user","content":"hi"}],"temperature":0.1}`)
		if got.host != "api.moonshot.cn" {
			t.Fatalf("the request went to %s", got.host)
		}
		if v := gjson.Get(got.body, "temperature"); !v.Exists() || v.Raw != "0.1" {
			t.Errorf("kimi-k2-thinking's temperature was not kept: %s", got.body)
		}
	})
	// #4495: Volcengine Ark locks the same models' sampling the same way
	// ("Volcengine Ark: `temperature` only 1 is allowed for this model"),
	// at its coding plan and — an Ark host being an Ark host — wherever
	// else its Kimi models are served.
	t.Run("volcengine's coding plan drops the fields", func(t *testing.T) {
		got := post(t, "/v1/chat/completions", `{"model":"volc/kimi-k3.1-pro","messages":[{"role":"user","content":"hi"}],"temperature":0.6,"top_p":0.95}`)
		if got.host != "ark.cn-beijing.volces.com" {
			t.Fatalf("the request went to %s", got.host)
		}
		if gjson.Get(got.body, "temperature").Exists() || gjson.Get(got.body, "top_p").Exists() {
			t.Errorf("Ark was sent the sampling fields: %s", got.body)
		}
	})
	t.Run("volcengine's anthropic endpoint drops the fields", func(t *testing.T) {
		got := post(t, "/v1/messages", `{"model":"volc/kimi-k3.1-pro","messages":[{"role":"user","content":"hi"}],"temperature":0.6,"top_p":0.95,"max_tokens":5}`)
		if got.host != "ark.cn-beijing.volces.com" {
			t.Fatalf("the request went to %s", got.host)
		}
		if gjson.Get(got.body, "temperature").Exists() || gjson.Get(got.body, "top_p").Exists() {
			t.Errorf("Ark was sent the sampling fields: %s", got.body)
		}
	})
	t.Run("volcengine keeps the fields for a model its server doesn't lock", func(t *testing.T) {
		got := post(t, "/v1/chat/completions", `{"model":"volc/doubao-seed-2.1-pro","messages":[{"role":"user","content":"hi"}],"temperature":0.6,"top_p":0.95}`)
		if !strings.Contains(got.body, `"temperature":0.6`) {
			t.Errorf("doubao was not sent its temperature: %s", got.body)
		}
	})
	t.Run("a provider added by hand on an Ark host drops the fields", func(t *testing.T) {
		got := post(t, "/v1/chat/completions", `{"model":"arkpay/kimi-k3","messages":[{"role":"user","content":"hi"}],"temperature":0.6,"top_p":0.95}`)
		if got.host != "ark.ap-southeast.volces.com" {
			t.Fatalf("the request went to %s", got.host)
		}
		if gjson.Get(got.body, "temperature").Exists() || gjson.Get(got.body, "top_p").Exists() {
			t.Errorf("Ark was sent the sampling fields: %s", got.body)
		}
	})
	t.Run("another provider is sent the body as it was", func(t *testing.T) {
		got := post(t, "/v1/chat/completions", `{"model":"other/m","messages":[{"role":"user","content":"hi"}],"temperature":0.1,"top_p":1}`)
		if !strings.Contains(got.body, `"temperature":0.1,"top_p":1`) {
			t.Errorf("another provider was sent: %s", got.body)
		}
	})
}
