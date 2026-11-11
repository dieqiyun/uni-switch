package gateway

import (
	"testing"
)

func TestGatewayQueryUsesUserWords(t *testing.T) {
	for _, tc := range []struct {
		name     string
		messages []Message
		want     string
	}{
		{"Codex environment", []Message{userMsg(envContext, "Quartz codename?")}, "Quartz codename?"},
		{"agent notices", []Message{userMsg("Quartz codename?"), userMsg(pluginRef, taskNotification)}, "Quartz codename?"},
		{"tool-only increment", []Message{{Role: "user", Parts: []Part{{Kind: ToolResult, Text: "QUARTZ-8413"}}}}, ""},
		{"context only", []Message{userMsg(envContext)}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := gatewayQuery(&Request{Messages: tc.messages}); got != tc.want {
				t.Fatalf("query %q, want %q", got, tc.want)
			}
		})
	}
}
