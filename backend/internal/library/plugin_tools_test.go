package library

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// fakePlugins has the plugins installed be specs (none on when off), each
// noted giving search and crawl, and the gateway's plugin tools be theirs,
// recording each call run.
type fakePlugins struct {
	specs []string
	off   bool
	err   error
	ran   []string
}

const search1api = "@uni-switch-community/tools-search1api@0.1.1"

// An agent that reaches the gateway at another address (a WSL distro under
// NAT) is given the plugin's tools at that address.
func TestPluginToolsServerThroughAnotherAddress(t *testing.T) {
	sandbox(t)
	s := &Server{Name: "search1api", Transport: "http", URL: "http://127.0.0.1:3425" + gateway.ToolsMCPOne + "search1api", Plugin: "@uni-switch-community/tools-search1api", Local: true}
	c := through(s, "http://172.20.0.1:3425")
	if c.URL != "http://172.20.0.1:3425"+gateway.ToolsMCPOne+"search1api" {
		t.Errorf("through another address: %s", c.URL)
	}
	if s.URL != "http://127.0.0.1:3425"+gateway.ToolsMCPOne+"search1api" {
		t.Errorf("the stored server changed: %s", s.URL)
	}
}
