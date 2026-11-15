package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// NVIDIA NIM and 魔搭 ModelScope are presets (#197): chat completions (and
// Responses at ModelScope), no Anthropic endpoint, their models.dev
// catalogs, and an entry imported at their base URLs is the preset.
func TestNvidiaModelScopePresets(t *testing.T) {
	for _, c := range []struct{ id, base string }{{"nvidia", "https://integrate.api.nvidia.com/v1"}, {"modelscope", "https://api-inference.modelscope.cn/v1"}} {
		if Preset(c.id) != nil {
			t.Fatalf("retired preset %s restored", c.id)
		}
		if _, err := FromPreset(c.id); err == nil {
			t.Fatalf("retired preset %s addable", c.id)
		}
		p, _ := imported("x", "k", endpoints{chat: c.base + "/"}, nil)
		if p.Preset != "" || p.Chat != c.base || p.Key != "k" {
			t.Fatalf("explicit custom endpoint: %+v", p)
		}
	}

}

// Their /v1/models, as each answers it: NVIDIA's embedding models are kept
// for /v1/embeddings, out of the chat models (#5339); ModelScope's rows
// (object "") are kept.
func TestNvidiaModelScopeLiveList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer k" {
			rw.WriteHeader(http.StatusUnauthorized)
			return
		}
		rw.Write([]byte(`{"object":"list","data":[
{"id":"deepseek-ai/deepseek-v4-pro","object":"model","created":735790403,"owned_by":"deepseek-ai"},
{"id":"nvidia/nemotron-3-embed-1b","object":"model","created":735790403,"owned_by":"nvidia"},
{"id":"ZhipuAI/GLM-5.2","object":"","owned_by":"system","created":1785767088}]}`))
	}))
	defer srv.Close()
	ms, _, err := catalog.FetchAt(context.Background(), srv.URL+"/v1", "k", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if chat := catalog.Chat(ms); len(chat) != 2 || chat[0].ID != "deepseek-ai/deepseek-v4-pro" || chat[1].ID != "ZhipuAI/GLM-5.2" {
		t.Fatalf("%+v", ms)
	}
	if len(ms) != 3 || ms[1].ID != "nvidia/nemotron-3-embed-1b" || ms[1].Serves != catalog.ServesEmbedding {
		t.Fatalf("the embedding model: %+v", ms)
	}
}
