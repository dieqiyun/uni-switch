package agent

import (
	"path/filepath"
	"testing"
)

// akic404 on Discord: with "Record gateway conversations" on, OpenCode's
// turns were kept and Pi's and omp's weren't. The gateway records a turn
// only under the session the agent names, and Pi (1.1.0) names it on Chat
// and Anthropic's Messages API only when its provider's compat says
// sendSessionAffinityHeaders, omp (18.6.1) on Chat and Responses only to
// OpenAI itself. Both were run against a header dump: with the provider
// block uni-switch writes, Pi sends x-session-affinity on Chat and Messages
// and session_id on Responses, omp x-session-affinity on Chat and
// Responses (and X-Claude-Code-Session-Id on Messages, as before).
func TestPiNamesItsSessionToTheGateway(t *testing.T) {
	home := syncHome(t)
	piModels := filepath.Join(home, ".pi", "agent", "models.json")
	if err := pi(home).Fields[0].Set("uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	compat, _ := providerAt(t, piModels, "providers")["compat"].(map[string]any)
	if compat["sendSessionAffinityHeaders"] != true {
		t.Fatalf("Pi isn't told to name its session:\n%s", readFile(piModels))
	}

	// one the user turned off stays off, through a sync and a pick
	writeFile(t, piModels, `{"providers":{"uni-switch":{"name":"uni-switch","baseUrl":"http://127.0.0.1:3425/v1","api":"openai-completions","apiKey":"uni-switch","compat":{"sendSessionAffinityHeaders":false,"supportsStore":false},"models":[]}}}`)
	SyncCatalog()
	if err := pi(home).Fields[0].Set("uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	compat, _ = providerAt(t, piModels, "providers")["compat"].(map[string]any)
	if compat["sendSessionAffinityHeaders"] != false || compat["supportsStore"] != false {
		t.Fatalf("the user's own compat was written over:\n%s", readFile(piModels))
	}
}

func TestOmpNamesItsSessionToTheGateway(t *testing.T) {
	syncHome(t)
	for _, c := range []struct {
		version string
		want    string
	}{
		{"18.6.1", "x-session-affinity"},
		{"16.0.6", "x-session-affinity"},
		// before 16.0.6 omp had no promptCacheSessionHeader; an omp whose
		// version isn't known is taken for an older one
		{"16.0.5", ""},
		{"", ""},
	} {
		if got := ompProviderAt("http://127.0.0.1:3425", c.version).Compat["promptCacheSessionHeader"]; got != c.want {
			t.Errorf("omp %q: promptCacheSessionHeader %q, want %q", c.version, got, c.want)
		}
	}
}
