package gateway

import (
	"reflect"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/modelmeta"
)

func TestCursorModelListCarriesNativeAPIsAndReasoningControls(t *testing.T) {
	c := Call{Source: Chat, Upstream: Responses, Models: []Model{
		{ID: "chat-fixture", Enabled: true, Metadata: &modelmeta.Resolved{Profile: Object{"contextWindow": 128000, "reasoningEfforts": []string{"low", "high"}, "endpoints": Object{"chatCompletions": true, "responses": false}}, PublishedOutput: 8192, Capabilities: Object{"imageInput": false}}},
		{ID: "responses-fixture", Enabled: true, Metadata: &modelmeta.Resolved{Profile: Object{"endpoints": Object{"responses": true}}}},
		{ID: "disabled", Enabled: false},
	}}
	rows := list(c.cursorModels()["data"])
	if len(rows) != 2 || !reflect.DeepEqual(obj(rows[0])["api_types"], []string{"chat_completions"}) || !reflect.DeepEqual(obj(rows[1])["api_types"], []string{"responses"}) {
		t.Fatal("wrong native protocol list")
	}
	caps := obj(obj(rows[0])["capabilities"])
	if integer(caps["context_length"]) != 128000 || integer(caps["max_output_tokens"]) != 8192 || caps["supports_vision"] != false || caps["supports_reasoning"] != true || caps["supports_streaming"] != true || caps["supports_tool_use"] != true || len(list(caps["reasoning_effort"])) != 2 || !reflect.DeepEqual(caps["output_modalities"], []string{"text"}) {
		t.Fatal("Cursor picker would discard capability data")
	}
	if obj(obj(rows[1])["capabilities"])["supports_vision"] != nil {
		t.Fatal("unknown image capability became an explicit guess")
	}
	c.Upstream = Messages
	c.Models[0].ID = "vendor/claude-fixture"
	if types := obj(list(c.cursorModels()["data"])[0])["api_types"]; !reflect.DeepEqual(types, []string{"anthropic_messages"}) {
		t.Fatal("Claude did not stay on Messages")
	}
	for _, row := range list(c.modelList()["data"]) {
		if obj(row)["api_types"] != nil || obj(row)["capabilities"] != nil {
			t.Fatal("Cursor-only data leaked to another client")
		}
	}
}
