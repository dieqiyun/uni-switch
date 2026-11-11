package provider

import "testing"

// oMLX (omlx.ai, github.com/jundot/omlx) is a local server on :8000 that
// speaks all three APIs. A fresh install binds loopback and asks no key, so
// it's a NoKey local preset; its Anthropic base is the root, /v1/messages is
// uni-switch's path there, as it is for Ollama.
func TestOmlxPreset(t *testing.T) {
	pr := Preset("omlx")
	if pr == nil {
		t.Fatal("no omlx preset")
	}
	if pr.Kind != KindLocal || !pr.NoKey || pr.KeysURL != "" {
		t.Fatalf("kind/noKey/keysURL: %q %v %q", pr.Kind, pr.NoKey, pr.KeysURL)
	}
	if pr.Chat != "http://localhost:8000/v1" || pr.Responses != "http://localhost:8000/v1" || pr.Anthropic != "http://localhost:8000" {
		t.Fatalf("endpoints: %q %q %q", pr.Chat, pr.Responses, pr.Anthropic)
	}
	p, err := FromPreset("omlx")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "omlx" || p.Preset != "omlx" || p.Icon != "omlx" {
		t.Fatalf("from preset: %+v", p)
	}
}
