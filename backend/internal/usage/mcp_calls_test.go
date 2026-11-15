package usage

import (
	"slices"
	"testing"
	"time"
)

// ferock on Discord: an agent's call to a tool of uni-switch's MCP servers
// (generate_image) is a row of the request log of its own, apart from
// the model requests, picked by the MCP tool purpose, naming the tool,
// its server, the provider and model it used, how long it took and how it
// ended, and counted in nothing: its image request is the call that is.
func TestMCPCallRowsListedNotCounted(t *testing.T) {
	reportedHome(t)
	now := time.Now().Add(-time.Hour)
	Append(Record{Time: now, Agent: "claude", Provider: "openai", Host: "api.openai.com", Model: "gpt-image-2", Requested: "gpt-image-2", Operation: "generate_content", Endpoint: "/v1/images/generations", Status: 200, Millis: 9000})
	Append(Record{Time: now.Add(time.Second), Agent: "claude", Operation: "execute_tool", Provider: "openai", Model: "gpt-image-2", Requested: "generate_image",
		Host: "uni-switch-image", Millis: 9100, Endpoint: "/mcp/uni-switch/image", Kind: KindMCP, Source: SourceMCP})
	Append(Record{Time: now.Add(2 * time.Second), Agent: "claude", Operation: "execute_tool", Requested: "github__create_issue",
		Host: "github", Millis: 40, Error: "it didn't answer", Endpoint: "/mcp/uni-switch/hub/claude", Kind: KindMCP, Source: SourceMCP})

	page := QueryPage(All, Filter{}, 0, 50)
	if page.Total != 3 || len(page.Rows) != 3 {
		t.Fatalf("total %d, %d rows: %+v", page.Total, len(page.Rows), page.Rows)
	}
	hub, image := page.Rows[0], page.Rows[1] // newest first
	if hub.Source != SourceMCP || !hub.IsProbe() || hub.Requested != "github__create_issue" || hub.Host != "github" || !hub.Failed() || hub.Provider != "" {
		t.Fatalf("hub row %+v", hub)
	}
	if image.Source != SourceMCP || image.Requested != "generate_image" || image.Provider != "openai" || image.Model != "gpt-image-2" ||
		image.Host != "uni-switch-image" || image.Millis != 9100 || image.Failed() || image.Endpoint != "/mcp/uni-switch/image" {
		t.Fatalf("image row %+v", image)
	}
	if page.Sum.Calls != 1 || page.Sum.Errors != 0 || page.Through.Calls != 1 {
		t.Fatalf("sum %+v through %+v", page.Sum, page.Through)
	}
	if !slices.Contains(page.Purposes, "kind:mcp") || PurposeOf(KindMCP) != "kind:mcp" || PurposeKinds()[KindMCP].Name != "MCP tool" {
		t.Fatalf("purposes %v", page.Purposes)
	}
	for dim, shares := range page.By {
		for _, s := range shares {
			if s.ID == "generate_image" || s.ID == "github__create_issue" || (dim == "provider" && s.Calls != 1) {
				t.Fatalf("%s counts a tool call: %+v", dim, shares)
			}
		}
	}
	if s := Summarize(All); s.Calls != 1 || s.Errors != 0 {
		t.Fatalf("summary %+v", s.Totals)
	}
	if rs := Load(time.Time{}); len(rs) != 1 {
		t.Fatalf("Load %+v", rs)
	}
	// the purpose picks the tool calls alone, and counts nothing still
	p := QueryPage(All, Filter{Purpose: "kind:mcp"}, 0, 50)
	if p.Total != 2 || p.Sum.Calls != 0 || p.Rows[0].Source != SourceMCP || p.Rows[1].Source != SourceMCP {
		t.Fatalf("purpose filter: total %d sum %+v rows %+v", p.Total, p.Sum, p.Rows)
	}
	// a tool call's server is no provider's host: a probe on the host is
	// not listed under the provider the tool used
	if hosts := readLogSnapshot().hosts; hosts["uni-switch-image"] != "" || hosts["github"] != "" {
		t.Fatalf("hosts %v", hosts)
	}
}
