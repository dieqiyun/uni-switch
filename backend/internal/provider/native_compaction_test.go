package provider

import (
	"path/filepath"
	"strings"
	"testing"
)

// Codex's own compaction (#1099) is offered only where the wire carries
// it: a Responses API, a ChatGPT account, GitHub Copilot. Another agent's
// sign-in, a plugin's, and a provider without a Responses API can't be
// set to it; a model a provider asks on another API isn't compacted there.
func TestCarriesCompaction(t *testing.T) {
	for _, tt := range []struct {
		name string
		p    Provider
		want bool
	}{
		{"a relay on Responses", Provider{Responses: "https://relay.example/v1"}, true},
		{"chat completions only", Provider{Chat: "https://relay.example/v1"}, false},
		{"anthropic only", Provider{Anthropic: "https://relay.example"}, false},
		{"a ChatGPT account", Provider{Responses: "https://chatgpt.com/backend-api/codex", Account: &Account{Agent: "codex"}}, true},
		{"GitHub Copilot", Provider{Chat: "https://api.githubcopilot.com", Responses: "https://api.githubcopilot.com", Account: &Account{Agent: "copilot"}}, true},
		{"another agent's sign-in", Provider{Responses: "https://x.example/v1", Account: &Account{Agent: "workbuddy"}}, false},
		{"a plugin's", Provider{Responses: "plugin://x", Account: &Account{Agent: "plugin:x"}}, false},
	} {
		if got := tt.p.CarriesCompaction(); got != tt.want {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
	on := Provider{Responses: "https://relay.example/v1", NativeCompaction: true}
	if !on.CompactsOn("m") {
		t.Error("a relay set to compact natively doesn't")
	}
	on.NativeCompaction = false
	if on.CompactsOn("m") {
		t.Error("a relay not set to compacts natively")
	}
	chat := Provider{Chat: "https://relay.example/v1", NativeCompaction: true}
	if chat.CompactsOn("m") {
		t.Error("a chat-only relay compacts natively")
	}
}

// The option is saved, refused where the wire can't carry it, turned off
// again, and kept by a copy.
func TestSetNativeCompaction(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := Save(Provider{ID: "relay", Name: "Relay", Responses: "https://relay.example/v1", Key: "sk-1"}); err != nil {
		t.Fatal(err)
	}
	if err := Save(Provider{ID: "chat", Name: "Chat", Chat: "https://chat.example/v1", Key: "sk-2"}); err != nil {
		t.Fatal(err)
	}
	if err := SetNativeCompaction("relay", true); err != nil {
		t.Fatal(err)
	}
	if p, _ := Find("relay"); !p.NativeCompaction {
		t.Fatal("not saved")
	}
	if err := SetNativeCompaction("chat", true); err == nil || !strings.Contains(err.Error(), "can't carry") {
		t.Fatalf("a chat-only provider took it: %v", err)
	}
	if p, _ := Find("chat"); p.NativeCompaction {
		t.Fatal("saved on a chat-only provider")
	}
	id, err := AddCopy(Provider{Name: "Relay copy", Responses: "https://relay.example/v1", Models: []string{"m"}}, "relay")
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := Find(id); !c.NativeCompaction {
		t.Error("a copy lost it")
	}
	if err := SetNativeCompaction("relay", false); err != nil {
		t.Fatal(err)
	}
	if p, _ := Find("relay"); p.NativeCompaction {
		t.Fatal("not turned off")
	}
}
