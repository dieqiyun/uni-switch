package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// codexViewImage is a Codex turn whose view_image call came back with an
// image, as codex-rs 0.162 sends it (FunctionCallOutputContentItem in
// protocol/src/models.rs; view_image.rs answers with the image alone, its
// detail "high"): the function_call_output's output is a list holding one
// input_image, a data: URL (#1861).
const codexViewImage = `{"model":"m","stream":true,"input":[
{"type":"message","role":"user","content":[{"type":"input_text","text":"使用computer use截图当前屏幕看下"}]},
{"type":"function_call","name":"view_image","arguments":"{\"path\":\"/tmp/uni_switch_icon.png\"}","call_id":"call_view1"},
{"type":"function_call_output","call_id":"call_view1","output":[{"type":"input_image","image_url":"data:image/png;base64,` + viewedPNG + `","detail":"high"}]}
]}`

// a 1x1 PNG
const viewedPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func codexViewImageRequest(t *testing.T) *Request {
	t.Helper()
	r, err := parse(provider.Responses, []byte(codexViewImage))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// What a plugin on Anthropic's Messages (Kiro's) is handed: the image in
// the tool_result, as an image block of base64 bytes. Kiro's plugin reads it
// there (uni-switch-community/plugins kiro 0.1.9).
func TestCodexToolImageReachesAnthropicPlugin(t *testing.T) {
	var body struct {
		Messages []struct {
			Content []struct {
				Type      string `json:"type"`
				ToolUseID string `json:"tool_use_id"`
				Content   []struct {
					Type string `json:"type"`
				} `json:"content"`
			} `json:"content"`
		} `json:"messages"`
	}
	b := buildAnthropic(codexViewImageRequest(t), "claude-sonnet-4.5")
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatal(err)
	}
	last := body.Messages[len(body.Messages)-1].Content
	if len(last) != 1 || last[0].Type != "tool_result" || len(last[0].Content) != 1 || last[0].Content[0].Type != "image" {
		t.Fatalf("tool_result = %s", b)
	}
	if !strings.Contains(string(b), `"source":{"type":"base64","media_type":"image/png","data":"`+viewedPNG+`"}`) {
		t.Fatalf("the image's bytes aren't in the tool_result: %s", b)
	}
}
