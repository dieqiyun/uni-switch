package gui

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAsideConnectAPIKeepsNativeSelections(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	dir := filepath.Join(home, ".aside", "u", "0")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")
	before := `{"defaultModel":{"provider":"minimax","modelId":"native","thinkingLevel":"high","fastMode":false},"imageGenerationModel":null}`
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	h := Handler(nil, nil)
	call := func(path string) agentJSON {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader("{}")))
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
		var s stateJSON
		if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		for _, a := range s.Agents {
			if a.ID == "aside" {
				return a
			}
		}
		t.Fatal("Aside missing")
		return agentJSON{}
	}
	a := call("/api/agents/connect/aside")
	if !a.Wired || a.Native == nil || a.Native.Provider != "connected" || a.Fields[0].Value != "minimax/native" {
		t.Fatalf("connect state %+v", a)
	}
	b, _ := os.ReadFile(path)
	if string(b) != before {
		t.Fatal("connect changed native selection")
	}
	a = call("/api/agents/disconnect/aside")
	if a.Wired || a.Fields[0].Value != "minimax/native" {
		t.Fatalf("disconnect state %+v", a)
	}
}
