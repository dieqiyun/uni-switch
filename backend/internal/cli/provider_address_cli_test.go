package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// An LM Studio on another computer (mengxiangziyou on X: "baseUrl 不能改"):
// address= puts the preset's provider there, its models listed from that
// server, and address= alone puts it back at the preset's port.
func TestProviderAddressMovesALocalServer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			w.Write([]byte(`{"object":"list","data":[{"id":"qwen3.5-9b-mlx","object":"model","owned_by":"organization_owner"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	if err := addProvider([]string{"lmstudio", "address=" + strings.TrimPrefix(srv.URL, "http://")}); err != nil {
		t.Fatal(err)
	}
	p, err := provider.Find("lmstudio")
	if err != nil {
		t.Fatal(err)
	}
	if p.Chat != srv.URL+"/v1" {
		t.Fatalf("chat %q, want %s/v1", p.Chat, srv.URL)
	}
	mu.Lock()
	got := strings.Join(asked, ",")
	mu.Unlock()
	if !strings.Contains(got, "GET /v1/models") {
		t.Fatalf("its models were not asked of the server it was moved to: %q", got)
	}
	if ids := p.Exposed(); len(ids) != 1 || ids[0].ID != "qwen3.5-9b-mlx" {
		t.Fatalf("models %+v, want the server's", ids)
	}

	// back where the preset puts it
	if err := providerCmd([]string{"provider", "set", "lmstudio", "address="}); err != nil {
		t.Fatal(err)
	}
	if p, _ = provider.Find("lmstudio"); p.Chat != "http://localhost:1234/v1" {
		t.Fatalf("chat %q after address=, want the preset's", p.Chat)
	}
}

// Every URL of a local server moves with its address, each keeping its
// path: Ollama's chat, Anthropic and decision APIs, from the preset's when
// it was saved with none; anything that is no address is refused, the URLs
// left as they were.
func TestProviderAddressPairs(t *testing.T) {
	p := provider.Provider{ID: "ollama", Name: "Ollama", Preset: "ollama"}
	if err := applyPairs(&p, []string{"address=192.168.1.5:11434"}); err != nil {
		t.Fatal(err)
	}
	if p.Chat != "http://192.168.1.5:11434/v1" || p.Anthropic != "http://192.168.1.5:11434" || p.Decide != "http://192.168.1.5:11434/v1" {
		t.Fatalf("not moved: %+v", p)
	}
	if err := applyPairs(&p, []string{"address=https://studio.lan:8443/v1"}); err != nil {
		t.Fatal(err)
	}
	if p.Chat != "https://studio.lan:8443/v1" || p.Anthropic != "https://studio.lan:8443" {
		t.Fatalf("not moved again: %+v", p)
	}
	if err := applyPairs(&p, []string{"address=ftp://box"}); err == nil || p.Chat != "https://studio.lan:8443/v1" {
		t.Fatalf("ftp://box: %v, chat %q", err, p.Chat)
	}
	// a provider of the user's own moves its own URLs
	c := provider.Provider{Name: "vLLM box", Chat: "http://10.0.0.7:8000/v1"}
	if err := applyPairs(&c, []string{"address=10.0.0.8:8001"}); err != nil || c.Chat != "http://10.0.0.8:8001/v1" {
		t.Fatalf("custom: %v, chat %q", err, c.Chat)
	}
	if err := applyPairs(&c, []string{"address="}); err == nil {
		t.Fatal("an empty address of a provider with no preset's to go back to was taken")
	}
}
