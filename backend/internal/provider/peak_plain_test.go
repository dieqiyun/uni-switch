package provider

import "testing"

// The Providers page asks PeakOf of every model of every list: a model no
// vendor bills by the hour is answered without parsing the provider's
// URLs, and the ones billed so still are.
func TestPeakOfPlainModelsAllocateNothing(t *testing.T) {
	gw := Provider{ID: "og", Chat: "https://opencode.ai/zen/go/v1", Responses: "https://opencode.ai/zen/go/v1"}
	if n := testing.AllocsPerRun(100, func() { gw.PeakOf("glm-5.1") }); n != 0 {
		t.Fatalf("PeakOf of a model billed one price allocated %v times", n)
	}
	if gw.PeakOf("glm-5.1") != "" {
		t.Fatal("glm-5.1 has peak hours on OpenCode Go")
	}
	if gw.PeakOf("deepseek-v4-pro") == "" {
		t.Fatal("OpenCode Go's deepseek-v4-pro has no peak hours")
	}
	if (Provider{ID: "ds", Chat: "https://api.deepseek.com/v1"}).PeakOf("anything") == "" {
		t.Fatal("DeepSeek's own models have no peak hours")
	}
}
