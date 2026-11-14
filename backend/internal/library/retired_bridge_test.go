package library

import (
	"encoding/json"
	"testing"
)

func TestRetiredRemoteBridgeInactive(t *testing.T) {
	var l Library
	if err := json.Unmarshal([]byte(`{"mcp":[{"name":"retired","bridge":"remote-uni-switch","transport":"http","url":"http://192.168.1.2:3425/mcp","agents":["codex"]},{"name":"custom","transport":"http","url":"http://192.168.1.3:8000/mcp"}],"mcpHub":true}`), &l); err != nil {
		t.Fatal(err)
	}
	if len(l.MCP) != 1 || l.MCP[0].Name != "custom" || !l.MCPHub {
		t.Fatalf("retired bridge activated or ordinary server lost: %+v", l)
	}
}
