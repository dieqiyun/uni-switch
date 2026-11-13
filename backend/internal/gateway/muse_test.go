package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Muse Code lists its models from /muse-code/models on its endpoint's host
// and starts no session without it; a row it shows has metadata["muse-code"]
func TestMuseModels(t *testing.T) {
	setup(t, provider.Responses, &fake{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/muse-code/models", nil)
	req.Header.Set("User-Agent", "muse-build/1.4.2 (non-interactive; macos-aarch64; build 0)")
	New().Handler().ServeHTTP(rec, req)
	var got struct {
		Object string
		Data   []struct {
			ID       string
			Metadata map[string]struct {
				Name       string
				ToolCall   bool `json:"tool_call"`
				IsHidden   bool `json:"is_hidden"`
				Modalities struct{ Input, Output []string }
				Limit      map[string]int
			}
		}
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.Object != "list" || len(got.Data) != 1 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	row := got.Data[0]
	meta, ok := row.Metadata["muse-code"]
	if row.ID != "fake/m1" || !ok || meta.Name == "" || !meta.ToolCall || meta.IsHidden || len(meta.Modalities.Output) == 0 {
		t.Fatalf("row: %s", rec.Body)
	}
	// a limit without both its context and its output hides the model in
	// Muse, and none has it ask for 128K-token replies
	if meta.Limit["context"] != museContext || meta.Limit["output"] != museOutput {
		t.Fatalf("limit of a model of unknown window: %s", rec.Body)
	}
}

// A Responses stream uni-switch makes from another API starts with output an
// empty list, as OpenAI's does: Muse Code takes output null for a stream it
// can't read and retries it until it gives up.
func TestResponsesStartOutputList(t *testing.T) {
	setup(t, provider.Chat, &fake{reply: sse(
		`data: {"id":"c","object":"chat.completion.chunk","model":"m1","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":null}]}`,
		`data: {"id":"c","object":"chat.completion.chunk","model":"m1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`)})
	code, body := post(t, "/v1/responses", `{"model":"fake/m1","stream":true,"input":[{"role":"user","content":"hi"}]}`)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	var seen int
	for _, ev := range events(body) {
		switch ev["type"] {
		case "response.created", "response.in_progress":
			seen++
			if out, ok := ev["response"].(map[string]any)["output"].([]any); !ok || len(out) != 0 {
				t.Fatalf("%s: output %v", ev["type"], ev["response"].(map[string]any)["output"])
			}
		}
	}
	if seen != 2 {
		t.Fatalf("no start events:\n%s", body)
	}
}
