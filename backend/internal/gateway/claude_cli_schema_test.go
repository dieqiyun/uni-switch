package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// codexTitleSchema is a Codex title request's schema as #3036's reporter
// had it reach Claude Code: zod's toJSONSchema, its draft 2020-12 named
// at the root.
const codexTitleSchema = `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"title":{"type":"string"}},"required":["title"],"additionalProperties":false}`

// fakeClaudeAjv stands in for Claude Code checking --json-schema as its
// Ajv does: a schema naming another draft than 07 at its root, or one
// with a keyword only a later draft has, ends it before it reads its
// input, with Claude Code 2.1.307's own words. Each run's arguments are
// kept in dir.
func fakeClaudeAjv(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for Claude Code")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
case "$1" in auth) exit 1;; esac
if [ "$2" = "/usage" ]; then
  echo '{"type":"result","is_error":false,"result":"Current session: 10% used"}'
  exit 0
fi
for a in "$@"; do echo "$a"; done > ` + dir + `/args.$$
for a in "$@"; do
  case "$a" in
  *'"$schema":"https://json-schema.org/draft/2020-12/schema"'*)
    echo 'Error: --json-schema is not a valid JSON Schema: no schema with key or ref "https://json-schema.org/draft/2020-12/schema"' >&2
    exit 1;;
  *'"prefixItems"'*)
    echo 'Error: --json-schema is not a valid JSON Schema: strict mode: unknown keyword: "prefixItems"' >&2
    exit 1;;
  esac
done
while read -r line; do
  echo '{"type":"stream_event","event":{"type":"message_start","message":{"id":"m","model":"claude-haiku-5-5","usage":{"input_tokens":1}}}}'
  echo '{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"t1","name":"StructuredOutput","input":{}}}}'
  echo '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"title\": \"Fix login bug\"}"}}}'
  echo '{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","is_error":false,"content":"Structured output provided successfully"}]}}'
  echo '{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}}'
  echo '{"type":"stream_event","event":{"type":"message_stop"}}'
  echo '{"type":"result","subtype":"success","is_error":false,"result":"","structured_output":{"title":"Fix login bug"}}'
done
`
	testenv.Program(t, filepath.Join(dir, "claude"), script)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

// cliSchemas are the --json-schema arguments of the runs fakeClaudeAjv
// kept in dir.
func cliSchemas(t *testing.T, dir string) []string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "args.*"))
	var out []string
	for _, f := range files {
		b, _ := os.ReadFile(f)
		args := strings.Split(strings.TrimSpace(string(b)), "\n")
		if i := slices.Index(args, "--json-schema"); i >= 0 && i+1 < len(args) {
			out = append(out, args[i+1])
		}
	}
	return out
}

// A Codex title request whose schema names draft 2020-12 is answered
// through a Claude subscription (#3036): Claude Code is given the schema
// without its root $schema, which its Ajv can't look up, and the rest of
// it as it came.
func TestClaudeCodeTakesCodexTitleSchema(t *testing.T) {
	dir := fakeClaudeAjv(t)
	s := New()
	t.Cleanup(s.subscription.abortAll)
	p := provider.Provider{ID: "claude", Account: &provider.Account{Agent: "claude", User: "u"}}
	body := `{"model":"claude-haiku-5-5","stream":true,"reasoning":{"effort":"low"},"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Generate a concise title. User prompt:\nfix the login bug"}]}],` +
		`"text":{"format":{"type":"json_schema","name":"codex_output_schema","strict":true,"schema":` + codexTitleSchema + `}}}`
	rec := httptest.NewRecorder()
	var u Usage
	if code, msg := s.serveClaudeSubscription(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body)), provider.Responses, p, "claude-haiku-5-5", []byte(body), &u); code != 200 {
		t.Fatalf("%d %s", code, msg)
	}
	if text, _ := outputText(t, rec.Body.String()); text != `{"title":"Fix login bug"}` {
		t.Fatalf("answer %q", text)
	}
	schemas := cliSchemas(t, dir)
	if len(schemas) != 1 {
		t.Fatalf("--json-schema of runs: %q", schemas)
	}
	var got, want map[string]any
	json.Unmarshal([]byte(schemas[0]), &got)
	json.Unmarshal([]byte(codexTitleSchema), &want)
	delete(want, "$schema")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("--json-schema %s, want %v", schemas[0], want)
	}
}

// cliSchema takes the root $schema out and nothing else: $id, $defs and a
// $schema below the root pass Claude Code's Ajv as they are, and a schema
// without a root $schema goes byte for byte.
func TestCLISchemaKeepsTheRest(t *testing.T) {
	in := `{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"https://example.com/title.json","type":"object",` +
		`"properties":{"title":{"$ref":"#/$defs/t"},"note":{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"string"}},"$defs":{"t":{"type":"string"}}}`
	var got, want map[string]any
	json.Unmarshal(cliSchema(json.RawMessage(in)), &got)
	json.Unmarshal([]byte(in), &want)
	delete(want, "$schema")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cliSchema: %v", got)
	}
	for _, plain := range []string{`{"type":"object","properties":{"title":{"type":"string"}}}`, `not json`, `true`} {
		if out := string(cliSchema(json.RawMessage(plain))); out != plain {
			t.Fatalf("%s became %s", plain, out)
		}
	}
}

// A schema Claude Code's Ajv won't compile ends it the same way every
// time, on every account: the request's 400, said in Claude Code's words,
// from one run — not a 502 asked again after a moment and of the next
// account, three runs and more for the same refusal (#3036) — and no
// account rests for it.
func TestClaudeSchemaRefusedIsAskedOnce(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "whole", true: "stream"}[stream], func(t *testing.T) { schemaRefusedOnce(t, stream) })
	}
}

func schemaRefusedOnce(t *testing.T, stream bool) {
	fresh(t)
	claudeMadeFirst(t, false) // two Claude accounts
	dir := fakeClaudeAjv(t)
	s := New()
	t.Cleanup(s.subscription.abortAll)
	streamed := ""
	if stream {
		streamed = `"stream":true,`
	}
	body := `{"model":"claude/claude-sonnet-5","max_tokens":100,` + streamed + `"messages":[{"role":"user","content":"pair?"}],` +
		`"output_config":{"format":{"type":"json_schema","schema":{"type":"array","prefixItems":[{"type":"string"},{"type":"number"}]}}}}`
	r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	r.Header.Set("x-api-key", Token)
	r.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	localGuard(s.Handler()).ServeHTTP(w, r)
	if w.Code != 400 || !strings.Contains(w.Body.String(), `--json-schema is not a valid JSON Schema: strict mode: unknown keyword`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if runs := cliSchemas(t, dir); len(runs) != 1 {
		t.Fatalf("Claude Code ran %d times: %q", len(runs), runs)
	}
	noneRested(t, "claude")
}
