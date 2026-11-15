package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// inputImages is every input_image object anywhere in a Responses body:
// message content and function_call_output lists alike.
func inputImages(t *testing.T, body string) []map[string]any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("body: %v\n%s", err, body)
	}
	var out []map[string]any
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if x["type"] == "input_image" {
				out = append(out, x)
			}
			for _, c := range x {
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(v)
	return out
}

// A strict Responses upstream (vLLM, built on openai-python's types) refuses
// an input_image without detail: "ResponseInputImageParam detail Field
// required" (#1474). Claude Code's pasted screenshot and a tool's image both
// go out with one.
func TestResponsesInputImageHasDetail(t *testing.T) {
	sent := toolImageUp(t, provider.Responses)
	postToolImages(t, "/v1/messages", `{"model":"probe/eye","max_tokens":64,"messages":[
		{"role":"user","content":[{"type":"text","text":"what is this?"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"`+pngA+`"}}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Read","input":{}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"shot"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"`+pngB+`"}}]}]}]}`)
	images := inputImages(t, sent())
	if len(images) != 2 {
		t.Fatalf("want 2 input_image, got %d: %s", len(images), sent())
	}
	for _, im := range images {
		if d, _ := im["detail"].(string); d == "" {
			t.Errorf("input_image without detail: %v", im)
		}
	}
}

// A tool output with no call_id becomes a user message; its image is a
// message input_image too, so it needs detail as well.
func TestOrphanedToolOutputImageHasDetail(t *testing.T) {
	body := `{"model":"m","input":[{"type":"function_call_output","output":[{"type":"input_text","text":"shot"},{"type":"input_image","image_url":"data:image/png;base64,` + pngA + `"}]}]}`
	got := string(orphanedToolOutputs([]byte(body)))
	images := inputImages(t, got)
	if len(images) != 1 || !strings.Contains(got, `"role":"user"`) {
		t.Fatalf("want one input_image in a user message, got %s", got)
	}
	if d, _ := images[0]["detail"].(string); d == "" {
		t.Errorf("input_image without detail: %s", got)
	}
}
