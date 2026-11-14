package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Claude Code's interactive mode takes --json-schema and does nothing with
// it (2.1.316: its StructuredOutput tool is -p's alone), so a request with
// a JSON schema is given an answer tool of uni-switch's, through the bridge's
// MCP server like the client's tools: its input schema is the request's,
// uni-switch checks each call against it, and the answer is the input of the
// call that fits, told when the turn's result comes. A call that doesn't
// fit is told why and made again, as -p's is; a turn that ends with no
// call is asked for one once.

// answerToolName is the answer tool's name, unless a client's tool has it.
const answerToolName = "StructuredOutput"

const answerToolDescription = "Use this tool to give your final answer, as its input, in the shape its input schema asks. Call it once, at the end of your work, instead of answering in words."

// answerTool is the tool a request with a schema is answered through, named
// apart from the client's tools.
func answerTool(tools []Tool, schema json.RawMessage) Tool {
	name := answerToolName
	for n := 2; slices.ContainsFunc(tools, func(t Tool) bool { return t.Name == name }); n++ {
		name = fmt.Sprintf("%s%d", answerToolName, n)
	}
	return Tool{Name: name, Description: answerToolDescription, Schema: cliSchema(schema)}
}

// answerAsk asks a turn that ended without the answer tool for its call.
func answerAsk(name string) string {
	return "Give your final answer now by calling the " + name + " tool, with the answer as its input."
}

// noSchemaLoads refuses every $ref outside the schema itself: a schema is
// the caller's, and nothing it names is fetched or read.
type noSchemaLoads struct{}

func (noSchemaLoads) Load(url string) (any, error) {
	return nil, fmt.Errorf("the schema refers to %s, outside itself", url)
}

// compileAnswerSchema compiles a request's schema to check the answer
// tool's calls against: as draft-07 without a $schema of its own, as
// Claude Code's -p reads one.
func compileAnswerSchema(raw json.RawMessage) (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("the JSON schema isn't valid JSON: %v", err)
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft7)
	c.UseLoader(noSchemaLoads{})
	const loc = "uni-switch:answer.json"
	if err := c.AddResource(loc, doc); err != nil {
		return nil, fmt.Errorf("the JSON schema can't be used: %v", err)
	}
	s, err := c.Compile(loc)
	if err != nil {
		return nil, fmt.Errorf("the JSON schema can't be used: %v", err)
	}
	return s, nil
}

// answer takes a call of the answer tool: the answer when it fits the
// schema, kept for the turn's result; why not, for the model to call it
// again, when it doesn't.
func (r *subscriptionRun) answer(w http.ResponseWriter, args json.RawMessage) {
	r.mu.Lock()
	schema := r.answerSchema
	r.mu.Unlock()
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(args))
	if err == nil {
		err = schema.Validate(v)
	}
	if err != nil {
		var ve *jsonschema.ValidationError
		why := err.Error()
		if errors.As(err, &ve) {
			why = ve.Error()
		}
		writeJSON(w, 200, mcpToolResult{Content: []map[string]any{{"type": "text", "text": "The input doesn't fit the schema: " + why + "\nCall the tool again with input that fits."}}, IsError: true})
		return
	}
	r.mu.Lock()
	r.answerArgs = slices.Clone(args)
	r.mu.Unlock()
	writeJSON(w, 200, mcpToolResult{Content: []map[string]any{{"type": "text", "text": "Answer received."}}})
}

// takeAnswer is the turn's answer, if a call gave one, and forgets it.
func (r *subscriptionRun) takeAnswer() json.RawMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.answerArgs
	r.answerArgs = nil
	return a
}
