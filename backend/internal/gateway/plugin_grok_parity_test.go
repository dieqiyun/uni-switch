package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"

	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A stream whose response.completed names no output, and which sent no
// output_item.done either, still gives the client the text it streamed.
func TestGatheredTextOnlyStream(t *testing.T) {
	g := &gathered{h: http.Header{}, status: 200}
	g.h.Set("Content-Type", "text/event-stream")
	io.WriteString(g, sse(
		`data: {"type":"response.created","response":{"id":"r1","model":"m"}}`,
		`data: {"type":"response.output_text.delta","delta":"po"}`,
		`data: {"type":"response.output_text.delta","delta":"ng"}`,
		`data: {"type":"response.completed","response":{"id":"r1","object":"response","status":"completed","usage":{"input_tokens":7,"output_tokens":1}}}`))
	rec := httptest.NewRecorder()
	if code, _ := g.finish(rec, provider.Provider{Name: "p"}, true, "", ""); code != 200 {
		t.Fatalf("%d %s", code, rec.Body)
	}
	var res struct {
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &res) != nil || len(res.Output) != 1 || res.Output[0].Type != "message" || len(res.Output[0].Content) != 1 || res.Output[0].Content[0].Text != "pong" {
		t.Fatalf("%s", rec.Body)
	}
}
