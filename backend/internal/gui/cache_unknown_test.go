package gui

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// A Kiro request's prompt is counted as Kiro counts it, prompt tokens but
// no cache, so the Context page scores its agent neither up nor down for
// the cache and doesn't call it cache-misses, and each point says its cache
// is unknown (#3464): the plugin said so (CacheUnknown), or it was kept
// before the plugin did and Kiro never counts it.
func TestRoutingKiroCacheKeptBefore(t *testing.T) {
	kept := []gateway.RouteUsage{{Provider: "kiro", Input: 148000, Output: 19}, {Provider: "anthropic", Input: 1200, Output: 1}}
	got := cacheCounted(kept)
	if !got[0].CacheUnknown || got[1].CacheUnknown {
		t.Fatalf("%+v", got)
	}
	if kept[0].CacheUnknown {
		t.Fatal("the route read was changed")
	}
	routes := pricedRoutes([]gateway.Route{{ID: 1, Agent: "claude", Provider: "kiro", Model: "claude-sonnet-4.5", Done: true, Status: 200, Usage: kept[:1]}})
	if len(routes) != 1 || !routes[0].Usage[0].CacheUnknown {
		t.Fatalf("%+v", routes)
	}
}
