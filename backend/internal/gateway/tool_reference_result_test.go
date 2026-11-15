package gateway

import (
	"encoding/json"
	"io"
	"net/http"

	"strings"
	"sync"
	"testing"
)

// openDesignVendor answers as OpenDesign's Anthropic endpoint answered
// #2124's reporter: a tool_result whose content is tool_reference blocks
// alone is a 422 "missing field `content`" (its index counting something
// other than the messages sent), and the same result with a text block
// beside the references is taken. takes is a vendor that reads either.
type openDesignVendor struct {
	takes  bool
	mu     sync.Mutex
	paths  []string
	bodies []string
}

func (v *openDesignVendor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	v.mu.Lock()
	v.paths = append(v.paths, r.URL.Path)
	v.bodies = append(v.bodies, string(b))
	v.mu.Unlock()
	var m struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	json.Unmarshal(b, &m)
	refused := false
	for _, msg := range m.Messages {
		var blocks []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
			} `json:"content"`
		}
		json.Unmarshal(msg.Content, &blocks)
		for _, bl := range blocks {
			if bl.Type != "tool_result" || len(bl.Content) == 0 {
				continue
			}
			text := false
			for _, c := range bl.Content {
				text = text || c.Type == "text"
			}
			refused = refused || !text && !v.takes
		}
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case refused:
		w.WriteHeader(422)
		io.WriteString(w, "Failed to deserialize the JSON body into the target type: messages[4]: missing field `content` at line 38 column 3")
	case strings.HasSuffix(r.URL.Path, "/count_tokens"):
		io.WriteString(w, `{"input_tokens":12}`)
	default:
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"deepseek-v4.1-flash","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}
}

func (v *openDesignVendor) sent() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string(nil), v.bodies...)
}

// #2124's repro.json, as the reporter posted it, with uni-switch's model.
func reporterRefsOnly(model string) string {
	return `{
  "model": "` + model + `",
  "max_tokens": 64,
  "thinking": {"type": "disabled"},
  "system": "This is a synthetic format test. Reply OK. Do not call any tool.",
  "tools": [
    {
      "name": "ToolSearch",
      "description": "Find a test tool.",
      "input_schema": {
        "type": "object",
        "properties": {"query": {"type": "string"}},
        "required": ["query"]
      }
    },
    {
      "name": "check",
      "description": "Return a fixed synthetic result.",
      "input_schema": {"type": "object", "properties": {}}
    }
  ],
  "messages": [
    {"role": "user", "content": "Reply with OK only."},
    {
      "role": "assistant",
      "content": [
        {"type": "text", "text": "I will find the check tool."},
        {
          "type": "tool_use",
          "id": "toolu_direct_probe",
          "name": "ToolSearch",
          "input": {"query": "check"}
        }
      ]
    },
    {
      "role": "user",
      "content": [
        {
          "type": "tool_result",
          "tool_use_id": "toolu_direct_probe",
          "content": [{"type": "tool_reference", "tool_name": "check"}]
        }
      ]
    }
  ]
}`
}

// A tool_result that already has text beside its references, or no
// references, is sent as it was, and so is a body without any.
func TestRefsSaidLeavesOtherResults(t *testing.T) {
	for _, body := range []string{
		`{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"a","content":[{"type":"text","text":"x"},{"type":"tool_reference","tool_name":"check"}]}]}]}`,
		`{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"a","content":"done"}]}]}`,
		`{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"a","content":[]}]}]}`,
		`{"messages":[{"role":"user","content":"hi"}]}`,
	} {
		if got := string(withRefsSaid([]byte(body))); got != body {
			t.Errorf("%s\nwent as %s", body, got)
		}
	}
	two := `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"a","content":[{"type":"tool_reference","tool_name":"WebFetch"},{"type":"tool_reference","tool_name":"WebSearch"}]}]}]}`
	if got := string(withRefsSaid([]byte(two))); !strings.Contains(got, `"text":"Tool WebFetch is loaded and can be called now.\nTool WebSearch is loaded and can be called now."`) ||
		!strings.Contains(got, `{"type":"tool_reference","tool_name":"WebFetch"},{"type":"tool_reference","tool_name":"WebSearch"}`) {
		t.Errorf("two references went as %s", got)
	}
}
