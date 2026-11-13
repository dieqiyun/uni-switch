package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func TestWorkBuddy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("WORKBUDDY_CONFIG_DIR", "")
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".workbuddy", "models.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	// WorkBuddy's own local model, which stays
	os.WriteFile(path, []byte(`[{"id":"qwen-local","local":true,"url":"http://127.0.0.1:8080/v1/chat/completions"}]`), 0o600)
	read := func() []map[string]any {
		var ms []map[string]any
		b, _ := os.ReadFile(path)
		if err := json.Unmarshal(b, &ms); err != nil {
			t.Fatalf("%v\n%s", err, b)
		}
		return ms
	}

	// found by its folder, as off a Mac (TestWorkBuddyOnMacIsItsApp)
	notOnMac(t)
	a := workbuddy(home)
	if !a.Detected() {
		t.Fatal("not detected")
	}
	f := a.Field("provider")
	if f.Get() != "" {
		t.Fatalf("get: %q", f.Get())
	}
	if err := f.Set("uni-switch"); err != nil {
		t.Fatal(err)
	}
	ms := read()
	if len(ms) != 2 || ms[0]["id"] != "qwen-local" || ms[0]["local"] != true {
		t.Fatalf("models: %v", ms)
	}
	pro := ms[1]
	if pro["id"] != "deepseek/pro" || pro["vendor"] != "uni-switch" || pro["apiKey"] != "uni-switch-workbuddy" ||
		pro["url"] != gatewayV1()+"/chat/completions" || pro["supportsToolCall"] != true || pro["maxInputTokens"] == nil {
		t.Fatalf("uni-switch model: %v", pro)
	}
	if f.Get() != "uni-switch" {
		t.Fatalf("get: %q", f.Get())
	}

	// turned off in WorkBuddy, it stays off through a sync
	ms[1]["disabled"] = true
	b, _ := json.Marshal(ms)
	os.WriteFile(path, b, 0o600)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if ms = read(); len(ms) != 2 || ms[1]["disabled"] != true {
		t.Fatalf("after sync: %v", ms)
	}

	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if ms = read(); len(ms) != 1 || ms[0]["id"] != "qwen-local" {
		t.Fatalf("after off: %v", ms)
	}
	if f.Get() != "" {
		t.Fatalf("get: %q", f.Get())
	}
	// off, a sync leaves it off
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if len(read()) != 1 {
		t.Fatal("sync put uni-switch back")
	}

	// the CLI's object form, with a list of the models to show
	os.WriteFile(path, []byte(`{"models":[{"id":"mine","url":"https://x/v1/chat/completions"}],"availableModels":["mine"],"other":1}`), 0o600)
	if err := f.Set("uni-switch"); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Models    []map[string]any `json:"models"`
		Available []string         `json:"availableModels"`
		Other     int              `json:"other"`
	}
	b, _ = os.ReadFile(path)
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Models) != 2 || doc.Other != 1 || len(doc.Available) != 2 || doc.Available[1] != "deepseek/pro" {
		t.Fatalf("object form: %s", b)
	}
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	doc.Available = nil
	json.Unmarshal(b, &doc)
	if len(doc.Models) != 1 || len(doc.Available) != 1 {
		t.Fatalf("object form off: %s", b)
	}
}

func TestWorkBuddyEfforts(t *testing.T) {
	e := buddyModel("workbuddy", "x/y", "Y", 0, 500000, true, []string{"none", "low", "high"})
	r, _ := e["reasoning"].(map[string]any)
	if e["maxInputTokens"] != 200000 || e["maxOutputTokens"] != 500000 || e["supportsReasoning"] != true ||
		r["canDisableThinking"] != true || r["defaultEffort"] != "high" || len(r["supportedEfforts"].([]string)) != 2 {
		t.Fatalf("%v", e)
	}
	if e := buddyModel("workbuddy", "x/z", "Z", 1000, 0, false, nil); e["reasoning"] != nil || e["supportsReasoning"] != false || e["maxOutputTokens"] != nil {
		t.Fatalf("%v", e)
	}
}

// A model with more than 200K of context gets WorkBuddy's context menu,
// set to its whole window (xiao9could on X: no context button for Opus).
func TestWorkBuddyContextChoice(t *testing.T) {
	e := buddyModel("workbuddy", "claude/claude-opus-5-5", "Opus", 1000000, 128000, true, nil)
	cw, _ := e["contextWindow"].(map[string]any)
	if l, _ := cw["supportedLengths"].([]int); len(l) != 2 || l[0] != 200000 || l[1] != 1000000 || cw["defaultLength"] != 1000000 || e["maxInputTokens"] != 1000000 {
		t.Fatalf("%v", e)
	}
	for _, c := range []int{0, 128000, 200000} {
		if e := buddyModel("workbuddy", "x/y", "Y", c, 0, false, nil); e["contextWindow"] != nil {
			t.Fatalf("%d: %v", c, e)
		}
	}
}

// WorkBuddy pointed at a uni-switch on another machine stays there through a
// sync, with that uni-switch's key (悠悠哥 on Discord: models moved to the
// uni-switch on a NAS were put back on this machine's), while one on this
// machine's loopback follows the gateway.
func TestWorkBuddyKeepsRemoteAddress(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("WORKBUDDY_CONFIG_DIR", "")
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro", "flash"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".workbuddy", "models.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	a := workbuddy(home)
	if err := a.Field("provider").Set(uniSwitchID); err != nil {
		t.Fatal(err)
	}
	read := func() []map[string]any {
		t.Helper()
		var ms []map[string]any
		b, _ := os.ReadFile(path)
		if err := json.Unmarshal(b, &ms); err != nil {
			t.Fatalf("%v\n%s", err, b)
		}
		return ms
	}
	point := func(url, key string) {
		t.Helper()
		ms := read()
		for _, m := range ms {
			m["url"], m["apiKey"] = url, key
		}
		b, _ := json.Marshal(ms)
		os.WriteFile(path, b, 0o600)
	}

	const nas = "http://192.168.1.20:3425/v1/chat/completions"
	point(nas, "nas-key")
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	ms := read()
	if len(ms) != 2 {
		t.Fatalf("models: %v", ms)
	}
	for _, m := range ms {
		if m["url"] != nas || m["apiKey"] != "nas-key" {
			t.Fatalf("NAS address not kept: %v", m)
		}
	}

	// an address on this machine is uni-switch's own and follows the gateway
	point("http://localhost:9999/v1/chat/completions", "old")
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	for _, m := range read() {
		if m["url"] != gatewayV1()+"/chat/completions" || m["apiKey"] != "uni-switch-workbuddy" {
			t.Fatalf("local address not followed: %v", m)
		}
	}
}

// WorkBuddy AI, the international build, reads ~/.workbuddy-ai/models.json
// (its product.json's dataFolderName), not ~/.workbuddy's (#1494, ysicing):
// uni-switch's models go there, as a bare list, under its own key, and the
// China build's file is left as it was.
func TestWorkBuddyAIWritesItsOwnFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("WORKBUDDY_CONFIG_DIR", "")
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro"}}); err != nil {
		t.Fatal(err)
	}
	cn := filepath.Join(home, ".workbuddy", "models.json")
	os.MkdirAll(filepath.Dir(cn), 0o755)
	os.WriteFile(cn, []byte(`[{"id":"qwen-local","local":true}]`), 0o600)
	// a folder WorkBuddy has started in, with no models.json: argv.json
	// and device-id are among what the China build keeps in ~/.workbuddy
	intl := filepath.Join(home, ".workbuddy-ai", "models.json")
	os.MkdirAll(filepath.Join(home, ".workbuddy-ai", "logs"), 0o755)
	os.WriteFile(filepath.Join(home, ".workbuddy-ai", "argv.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(home, ".workbuddy-ai", "device-id"), []byte("d"), 0o644)

	notOnMac(t)
	a := workbuddyAI(home)
	if a.ID != "workbuddy-ai" || a.Path != intl {
		t.Fatalf("agent: %s at %s", a.ID, a.Path)
	}
	if !a.Detected() {
		t.Fatal("not detected")
	}
	if err := a.Field("provider").Set("uni-switch"); err != nil {
		t.Fatal(err)
	}
	var ms []map[string]any
	b, _ := os.ReadFile(intl)
	if err := json.Unmarshal(b, &ms); err != nil {
		t.Fatalf("not a bare list: %v\n%s", err, b)
	}
	if len(ms) != 1 || ms[0]["id"] != "deepseek/pro" || ms[0]["apiKey"] != "uni-switch-workbuddy-ai" {
		t.Fatalf("models: %v", ms)
	}
	if got := a.Field("provider").Get(); got != "uni-switch" {
		t.Fatalf("get: %q", got)
	}
	if b, _ := os.ReadFile(cn); string(b) != `[{"id":"qwen-local","local":true}]` {
		t.Fatalf("China build's file changed: %s", b)
	}
	if workbuddy(home).Field("provider").Get() != "" {
		t.Fatal("WorkBuddy (China) reads as wired")
	}
	// WORKBUDDY_CONFIG_DIR is the China build's; the international one
	// keeps its own folder
	t.Setenv("WORKBUDDY_CONFIG_DIR", filepath.Join(home, "elsewhere"))
	if workbuddyAI(home).Path != intl {
		t.Fatal("WorkBuddy AI followed WORKBUDDY_CONFIG_DIR")
	}
}
