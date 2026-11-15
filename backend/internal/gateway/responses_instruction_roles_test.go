package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Offering search can translate a Responses request back to Responses. Mode
// changes must keep their authority even though the request is not relayed.
func TestResponsesInstructionRolesThroughSearch(t *testing.T) {
	srv, seen := relayOfEvery(t)
	fresh(t)
	if err := provider.Save(provider.Provider{
		ID: "instruction-relay", Name: "Instruction relay", Key: "k",
		Responses: srv.URL + "/v1", Models: []string{"example-model"},
	}); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"developer", "system"} {
		t.Run(role, func(t *testing.T) {
			seen()
			messages := []struct{ role, text string }{
				{role, "<collaboration_mode>Plan: do not edit files.</collaboration_mode>"},
				{role, "Keep the task local."},
				{"user", "Plan a change."},
				{"assistant", "The plan is ready."},
				{role, "<collaboration_mode>Default: previous Plan instructions no longer apply.</collaboration_mode>"},
				{role, "Implement the accepted plan."},
				{"user", "Implement the plan."},
			}
			var input []string
			for _, m := range messages {
				input = append(input, codexItem(m.role, m.text))
			}
			body := `{"model":"instruction-relay/example-model","stream":true,"instructions":"You are a coding assistant.",` +
				`"input":[` + strings.Join(input, ",") + `],"tools":[{"type":"web_search"}]}`
			if code, out := post(t, "/v1/responses", body); code != 200 {
				t.Fatalf("%d %s", code, out)
			}
			paths, bodies := seen()
			if len(paths) != 1 || paths[0] != "/v1/responses" {
				t.Fatalf("upstream paths %v, want one Responses request", paths)
			}
			var got struct {
				Instructions string `json:"instructions"`
				Input        []struct {
					Type, Role string
					Content    json.RawMessage
				} `json:"input"`
			}
			if err := json.Unmarshal([]byte(bodies[0]), &got); err != nil {
				t.Fatal(err)
			}
			if got.Instructions != "You are a coding assistant." {
				t.Fatalf("instruction item moved into the prefix: %q", got.Instructions)
			}
			if len(got.Input) != len(messages) {
				t.Fatalf("input messages %d, want %d: %s", len(got.Input), len(messages), bodies[0])
			}
			for i, want := range messages {
				m := got.Input[i]
				if m.Type != "message" || m.Role != want.role || text(responsesParts(m.Content)) != want.text {
					t.Errorf("input[%d]: %s %s %s, want %s %q", i, m.Type, m.Role, m.Content, want.role, want.text)
				}
			}
		})
	}
}

// A provider can fall back from Responses to another API. Its existing
// mapping still applies there, and building it must not mutate the native turn.
func TestResponsesInstructionsOtherAPIsUnchanged(t *testing.T) {
	body := []byte(`{"model":"example-model","instructions":"You are Codex.","input":[` +
		codexItem("developer", "Plan: do not edit files.") + `,` +
		codexItem("user", "Plan a change.") + `,` +
		codexItem("assistant", "The plan is ready.") + `,` +
		codexItem("developer", "Default: implement the plan.") + `,` +
		codexItem("user", "Implement it.") + `]}`)
	native := &Request{Model: "example-model", System: "You are Codex.", Messages: []Message{
		{Role: "developer", Parts: []Part{{Kind: Text, Text: "Plan: do not edit files."}}},
		{Role: "user", Parts: []Part{{Kind: Text, Text: "Plan a change."}}},
		{Role: "assistant", Parts: []Part{{Kind: Text, Text: "The plan is ready."}}},
		{Role: "developer", Parts: []Part{{Kind: Text, Text: "Default: implement the plan."}}},
		{Role: "user", Parts: []Part{{Kind: Text, Text: "Implement it."}}},
	}}
	legacy, err := parse(provider.Responses, body)
	if err != nil {
		t.Fatal(err)
	}
	before := string(buildResponses(native, "example-model", "", false))
	for _, proto := range []provider.Protocol{provider.Chat, provider.Anthropic, provider.Gemini} {
		t.Run(string(proto), func(t *testing.T) {
			got, err := build(proto, native, "example-model", "", false)
			if err != nil {
				t.Fatal(err)
			}
			want, err := build(proto, legacy, "example-model", "", false)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Fatalf("non-Responses mapping changed:\ngot  %s\nwant %s", got, want)
			}
			if after := string(buildResponses(native, "example-model", "", false)); after != before {
				t.Fatalf("building %s mutated the native request:\n%s\n%s", proto, before, after)
			}
		})
	}
}
