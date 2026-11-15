package gui

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The Agents page's Disconnect: an agent uni-switch is in says so (wired), and
// disconnecting it puts back the model the user had and answers with the
// agent no longer wired.
func TestAgentDisconnectAPI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	cfg := filepath.Join(home, ".codex", "config.toml")
	os.WriteFile(cfg, []byte("model = \"gpt-5.4\"\n"), 0o600)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	h := Handler(nil, nil)
	call := func(path, body string) stateJSON {
		t.Helper()
		method := "POST"
		if body == "" {
			method = "GET"
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("%s %d %s", path, rec.Code, rec.Body)
		}
		var s stateJSON
		json.Unmarshal(rec.Body.Bytes(), &s)
		return s
	}
	codex := func(s stateJSON) agentJSON {
		for _, a := range s.Agents {
			if a.ID == "codex" {
				return a
			}
		}
		t.Fatalf("no codex in %+v", s.Agents)
		return agentJSON{}
	}
	if codex(call("/api/state", "")).Wired {
		t.Fatal("wired before uni-switch set anything")
	}
	if !codex(call("/api/set", `{"agent":"codex","field":"model","value":"relay/m1"}`)).Wired {
		t.Fatal("not wired on a uni-switch model")
	}
	a := codex(call("/api/agents/disconnect/codex", "{}"))
	if a.Wired || a.Fields[0].Value != "gpt-5.4" {
		t.Fatalf("after disconnect: wired %v, model %q", a.Wired, a.Fields[0].Value)
	}
}
