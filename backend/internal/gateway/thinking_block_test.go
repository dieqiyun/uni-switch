package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

// A signed thinking block with no text goes with "thinking": "", the
// field Messages requires. Without it OpenCode Go's Claude models answer
// "messages.1.content.0.thinking.thinking: Field required" (#1447).
func TestSignedThinkingWithNoTextKeepsTheField(t *testing.T) {
	r := &Request{Messages: []Message{
		{Role: "user", Parts: []Part{{Kind: Text, Text: "hi"}}},
		{Role: "assistant", Parts: []Part{{Kind: Thinking, Signature: "abc123"}, {Kind: Text, Text: "hello"}}},
		{Role: "user", Parts: []Part{{Kind: Text, Text: "go on"}}},
	}}
	var out struct {
		Messages []struct {
			Content []map[string]any `json:"content"`
		} `json:"messages"`
	}
	body := buildAnthropic(r, "claude-haiku-5-5")
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	think := out.Messages[1].Content[0]
	if v, ok := think["thinking"]; !ok || v != "" || think["signature"] != "abc123" {
		t.Fatalf("thinking block = %v: a signed block with no text must carry \"thinking\": \"\"", think)
	}
	// Other blocks keep leaving their empty fields out.
	if text := out.Messages[1].Content[1]; len(text) != 2 || text["text"] != "hello" {
		t.Fatalf("text block = %v", text)
	}
	if strings.Count(string(body), `"thinking":`) != 1 {
		t.Fatalf("only the thinking block names thinking: %s", body)
	}
}
