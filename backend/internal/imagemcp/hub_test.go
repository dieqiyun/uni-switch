package imagemcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// Behind uni-switch's MCP hub the server is started once for every agent, in
// the home folder: a call the hub makes carries its agent's roots and
// name in _meta, and the images go into that agent's project, asked for
// under its name. With no roots (an agent that says none) they go into
// ~/Pictures/uni-switch, never the folder the server was started in. The hub
// keeps the call's row in the request log, so the server reports none.
func TestHubCallSavesInTheCallersProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	g := &reportGW{}
	mine := t.TempDir() // what this server's own client says is its root
	c := start(t, g, mine)
	c.call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"roots": map[string]any{}}, "clientInfo": map[string]any{"name": "uni-switch", "version": "1"}})

	project := t.TempDir()
	r := c.call("tools/call", map[string]any{"name": "generate_image", "arguments": map[string]any{"prompt": "a uni-switch"},
		"_meta": map[string]any{gateway.HubRootsMeta: []string{"https://example.com/x", fileURI(project)}, gateway.HubClientMeta: "claude-code/2.1.314"}})
	if r["isError"] == true || !strings.Contains(text(r), filepath.Join(project, Folder)) {
		t.Fatalf("%v", r)
	}
	if files, _ := filepath.Glob(filepath.Join(project, Folder, "a-uni-switch-*.png")); len(files) != 1 {
		t.Fatalf("saved %v", files)
	}
	if got := g.ua[len(g.ua)-1]; got != "claude-code/2.1.314 uni-switch-image/1" {
		t.Errorf("asked as %q", got)
	}
	// the hub's row names the model, from the result's _meta
	if m, _ := r["_meta"].(map[string]any); m == nil || !strings.Contains(fmt.Sprint(m[gateway.HubModelMeta]), "/") {
		t.Errorf("no model in the result's _meta: %v", r)
	}

	r = c.call("tools/call", map[string]any{"name": "generate_image", "arguments": map[string]any{"prompt": "a uni-switch"},
		"_meta": map[string]any{gateway.HubRootsMeta: []string{}}})
	pictures := filepath.Join(home, "Pictures", "uni-switch")
	if r["isError"] == true || !strings.Contains(text(r), pictures) {
		t.Fatalf("no roots: %v", r)
	}
	if files, _ := filepath.Glob(filepath.Join(mine, Folder, "*")); len(files) != 0 {
		t.Errorf("saved into the server's own root: %v", files)
	}
	if _, err := os.Stat(pictures); err != nil {
		t.Errorf("nothing in %s: %v", pictures, err)
	}

	// a call of its own client's is reported; the hub's two were not
	if r = c.call("tools/call", map[string]any{"name": "generation_result", "arguments": map[string]any{"id": "img_nothing"}}); r["_meta"] != nil {
		t.Errorf("its own client's call has the hub's _meta: %v", r)
	}
	var got []string
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end) && !strings.Contains(strings.Join(got, " "), "generation_result"); time.Sleep(5 * time.Millisecond) {
		got = g.reported(1)
	}
	if len(got) != 1 || !strings.Contains(got[0], `"generation_result"`) {
		t.Fatalf("reported %v", got)
	}
}
