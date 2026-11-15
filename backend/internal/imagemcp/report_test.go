package imagemcp

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// reportGW is gw that keeps the tool calls the server reports.
type reportGW struct {
	gw
	mu    sync.Mutex
	calls []string
}

func (g *reportGW) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == gateway.ImageMCPPath {
		b, _ := io.ReadAll(r.Body)
		var m struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		json.Unmarshal(b, &m)
		if m.Method == gateway.MCPCalledMethod {
			g.mu.Lock()
			g.calls = append(g.calls, string(m.Params))
			g.mu.Unlock()
			io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
			return
		}
		modelsMCP(w, b)
		return
	}
	g.gw.ServeHTTP(w, r)
}

func (g *reportGW) reported(n int) []string {
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		g.mu.Lock()
		got := append([]string(nil), g.calls...)
		g.mu.Unlock()
		if len(got) >= n {
			return got
		}
	}
	return nil
}

// Each tool call the uni-switch Image server answers is reported to the
// gateway (MCPCalledMethod), for its row in the request log: the tool,
// how long it took, the model the gateway named, and never the prompt.
func TestToolCallsAreReported(t *testing.T) {
	g := &reportGW{}
	project := t.TempDir()
	c := start(t, g, project)
	c.call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"roots": map[string]any{}}, "clientInfo": map[string]any{"name": "claude-code", "version": "2.1.0"}})
	r := c.call("tools/call", map[string]any{"name": "generate_image", "arguments": map[string]any{"prompt": "secret-plum-branch"}})
	if r["isError"] == true {
		t.Fatalf("%v", r)
	}
	c.call("tools/call", map[string]any{"name": "generation_result", "arguments": map[string]any{"id": "img_nothing"}})
	got := g.reported(2)
	if len(got) != 2 {
		t.Fatalf("reported %v", got)
	}
	by := map[string]gateway.MCPCalled{}
	for _, x := range got {
		var c gateway.MCPCalled
		json.Unmarshal([]byte(x), &c)
		by[c.Tool] = c
	}
	first, second := by["generate_image"], by["generation_result"]
	if first.Tool != "generate_image" || first.Model != "art/gpt-image-1" || first.Error != "" || first.Millis < 0 {
		t.Fatalf("reported %+v", first)
	}
	if second.Tool != "generation_result" || second.Error == "" {
		t.Fatalf("reported %+v", second)
	}
	if strings.Contains(strings.Join(got, ""), "secret-plum") {
		t.Fatalf("the prompt was reported: %v", got)
	}
}
