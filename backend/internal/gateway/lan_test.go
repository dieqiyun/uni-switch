package gateway

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// A gateway on every interface is reached here on loopback.
func TestURLOfWildcard(t *testing.T) {
	t.Setenv("UNI_SWITCH_ADDR", "0.0.0.0:3499")
	if u := URL(); u != "http://127.0.0.1:3425" {
		t.Fatal(u)
	}
	t.Setenv("UNI_SWITCH_ADDR", "")
	if u := URL(); u != "http://127.0.0.1:3425" {
		t.Fatal(u)
	}
}

// An agent whose User-Agent is only the AI SDK's (Alma) is known by
// the token it was given; the others still by their User-Agent.
func TestAgentOf(t *testing.T) {
	for _, c := range []struct{ auth, key, ua, want string }{
		{"Bearer " + TokenFor("alma"), "", "ai-sdk/openai/2.0.52 ai-sdk/provider-utils/3.0.12 runtime/node.js/v22", "alma"},
		// alma-server's (Alma 0.4's @ai-sdk/openai 4.0.15 under Node 22,
		// whose navigator.userAgent the AI SDK appends): Alma's by the key
		// uni-switch gives its provider, the AI SDK's without it (Lutra.x)
		{"Bearer " + TokenFor("alma"), "", "ai-sdk/openai/4.0.15 ai-sdk/provider-utils/5.0.10 runtime/node.js/22", "alma"},
		{"Bearer " + Token, "", "ai-sdk/openai/4.0.15 ai-sdk/provider-utils/5.0.10 runtime/node.js/22", "ai-sdk"},
		{"", TokenFor("alma"), "ai-sdk/anthropic/2.0.1", "alma"},
		{"Bearer " + TokenFor("qoder"), "", "Bun/1.4.2", "qoder"},
		{"Bearer " + TokenFor("hanako"), "", "OpenAI/JS 6.0.0", "hanako"},
		{"Bearer " + Token, "", "claude-cli/2.1.0 (external, cli)", "claude-cli"},
		{"Bearer " + Token, "", "ai-sdk/openai/2.0.52", "ai-sdk"},
		{"Bearer " + Token + "-", "", "codex_cli_rs/0.40.0", "codex_cli_rs"},
	} {
		r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
		if c.auth != "" {
			r.Header.Set("Authorization", c.auth)
		}
		if c.key != "" {
			r.Header.Set("x-api-key", c.key)
		}
		r.Header.Set("User-Agent", c.ua)
		if got := agentOf(r); got != c.want {
			t.Errorf("%q %q %q: %q, want %q", c.auth, c.key, c.ua, got, c.want)
		}
	}
}

// A container is known by Docker's or Podman's marker file, or a runtime
// in PID 1's cgroup; a plain system has none of them.
func TestInContainer(t *testing.T) {
	mk := func(file, body string) string {
		root := t.TempDir()
		if file != "" {
			p := filepath.Join(root, file)
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.WriteFile(p, []byte(body), 0o644)
		}
		return root
	}
	for _, c := range []struct {
		file, body string
		want       bool
	}{
		{"", "", false},
		{"proc/1/cgroup", "0::/init.scope\n", false},
		{".dockerenv", "", true},
		{"run/.containerenv", "", true},
		{"proc/1/cgroup", "12:pids:/docker/4f1c…\n", true},
		{"proc/1/cgroup", "0::/kubepods/besteffort/pod1\n", true},
	} {
		if got := inContainer(mk(c.file, c.body)); got != c.want {
			t.Errorf("%s %q: %v", c.file, c.body, got)
		}
	}
}

// Every route the gateway serves — the model calls, count_tokens, System
// One, the Responses websocket, images, retrieval, videos, Gemini, the MCP
// sign-ins, Codex's backend and catalog, the quotas, the route and the
// rest — refuses another machine without an enabled gateway key, when
// UNI_SWITCH_ADDR puts the gateway on the network and it isn't shared (the
// Docker image, whose host reaches it from Docker's bridge). Through the
// whole server stack, lanGuard in front of Handler.
func TestNetworkGatewayGuardsEveryRoute(t *testing.T) {
	fresh(t)
	t.Setenv("UNI_SWITCH_ADDR", "0.0.0.0:3425")
	t.Setenv("UNI_SWITCH_TRUST_PROXY", "")
	if err := settings.Save(settings.Settings{}); err != nil {
		t.Fatal(err)
	}
	h := localGuard(New().Handler())
	// a Claude subscription run is live: its callback token lets in its
	// own callback alone (TestOnlyALiveRunsCallbackSkipsTheKey)
	live := randomToken()
	addCallback(live)
	t.Cleanup(func() { dropCallback(live) })
	routes := [][2]string{
		{"GET", "/"}, {"GET", "/v1"}, {"GET", "/v1/models"}, {"GET", "/models"}, {"GET", "/v1/models/x"},
		{"GET", "/muse-code/models"}, {"GET", "/api/hello"},
		{"GET", "/api/version"}, {"GET", "/api/tags"}, {"POST", "/api/show"},
		{"GET", "/v1/uni-switch/quotas"}, {"GET", "/v1/uni-switch/quotas/history"},
		{"GET", "/v1/uni-switch/route?session=s"}, {"GET", "/v1/uni-switch/concurrency"}, {"GET", "/v1/uni-switch/limit"},
		{"POST", "/v1/chat/completions"}, {"POST", "/chat/completions"},
		{"POST", "/v1/responses"}, {"POST", "/responses"}, {"GET", "/v1/responses"}, {"GET", "/responses"},
		{"POST", "/v1/messages"}, {"POST", "/messages"}, {"POST", "/v1/messages/count_tokens"},
		{"POST", "/v1/systemone"},
		{"POST", "/v1/images/generations"}, {"POST", "/v1/images/edits"},
		{"POST", "/v1/embeddings"}, {"POST", "/v1/rerank"}, {"POST", "/v1/decisions"},
		{"POST", "/v1/audio/transcriptions"}, {"POST", "/v1/audio/translations"}, {"POST", "/v1/audio/speech"},
		{"POST", "/v1/videos"}, {"GET", "/v1/videos/v1"}, {"GET", "/v1/videos/v1/content"},
		{"POST", "/_uni-switch/claude-mcp/tok"}, {"POST", "/mcp/neon"},
		{"POST", CodexPath + "/responses"}, {"GET", CodexCatalogPath},
		{"GET", "/v1beta/models"}, {"POST", "/v1beta/models/m:generateContent"},
		{"GET", "/nowhere"},
	}
	call := func(method, path, from string, hdr ...string) (int, string) {
		r := httptest.NewRequest(method, path, strings.NewReader("{}"))
		r.RemoteAddr = from
		if path == "/v1/responses" || path == "/responses" {
			r.Header.Set("Connection", "Upgrade")
			r.Header.Set("Upgrade", "websocket")
			r.Header.Set("Sec-WebSocket-Version", "13")
			r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		}
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	for _, rt := range routes {
		for _, from := range []string{"172.17.0.1:40312", "192.168.1.9:5000"} {
			for _, hdr := range [][]string{nil, {"Authorization", "Bearer uni-switch"}, {"x-api-key", "anything"}, {"x-goog-api-key", "uni-switch"}, {"Authorization", "Bearer " + live}} {
				resetAuthThrottle() // test each route's own guard, not the flood limiter that would 429 later tries
				if c, b := call(rt[0], rt[1], from, hdr...); c != http.StatusForbidden {
					t.Errorf("%s %s from %s with %v: %d %.120s", rt[0], rt[1], from, hdr, c, b)
				}
			}
		}
	}
	// a gateway key gets past the guard; loopback still takes any token
	_, secrets := newCaller(t, "Host")
	settings.Save(settings.Settings{}) // newCaller shares uni-switch
	if c, b := call("GET", "/v1/models", "172.17.0.1:40312", "Authorization", "Bearer "+secrets[0]); c != http.StatusForbidden {
		t.Fatal("with the key:", c, b)
	}
	if c, b := call("GET", "/v1/uni-switch/quotas", "172.17.0.1:40312", "x-api-key", secrets[0]); c != http.StatusForbidden {
		t.Fatal("quotas with the key:", c, b)
	}
	if c, b := call("GET", "/v1/models", "127.0.0.1:5000", "Authorization", "Bearer uni-switch"); c != 200 {
		t.Fatal("loopback:", c, b)
	}
}
