package gui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #2383: the editor's Certificate tick is saved on a provider of the
// user's, opens as saved, and is kept by a Save that leaves it out; a
// preset's provider is refused it.
func TestProviderSaveInsecureTLS(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	post := func(body string) (int, string) {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/save", strings.NewReader(body)))
		return w.Code, w.Body.String()
	}
	unchecked := func(id string) bool {
		t.Helper()
		p, err := provider.Find(id)
		if err != nil {
			t.Fatal(err)
		}
		info := providerInfo(*p, nil)
		if info.InsecureTLS != p.InsecureTLS {
			t.Fatalf("the editor is shown %v, saved %v", info.InsecureTLS, p.InsecureTLS)
		}
		return p.InsecureTLS
	}

	if code, out := post(`{"id":"vllm","name":"vLLM","key":"sk-v","chat":"https://192.168.1.20:8443/v1","icon":"generic","new":true,"insecureTLS":true}`); code != 200 {
		t.Fatalf("%d %s", code, out)
	}
	if !unchecked("vllm") {
		t.Fatal("added unchecked, saved checked")
	}
	p, _ := provider.Find("vllm")
	if !providerInfo(*p, nil).TakesInsecureTLS {
		t.Fatal("a provider of the user's shown as not taking it")
	}
	if code, out := post(`{"id":"vllm","from":"vllm","name":"vLLM","chat":"https://192.168.1.20:8443/v1","icon":"generic","models":[],"headers":{}}`); code != 200 {
		t.Fatalf("%d %s", code, out)
	}
	if !unchecked("vllm") {
		t.Fatal("a Save that left it out turned the check back on")
	}
	if code, out := post(`{"id":"vllm","from":"vllm","name":"vLLM","chat":"https://192.168.1.20:8443/v1","icon":"generic","models":[],"headers":{},"insecureTLS":false}`); code != 200 {
		t.Fatalf("%d %s", code, out)
	}
	if unchecked("vllm") {
		t.Fatal("unticked, still unchecked")
	}

	ds, err := provider.FromPreset("deepseek")
	if err != nil {
		t.Fatal(err)
	}
	ds.Key = "sk-ds"
	if err := provider.Save(ds); err != nil {
		t.Fatal(err)
	}
	if code, _ := post(`{"id":"deepseek","from":"deepseek","name":"DeepSeek","preset":"deepseek","key":"","chat":"` + ds.Chat + `","models":[],"headers":{},"insecureTLS":true}`); code == 200 {
		t.Error("a preset's provider saved unchecked")
	}
	if unchecked("deepseek") {
		t.Error("a preset's provider unchecked")
	}
	d, _ := provider.Find("deepseek")
	if providerInfo(*d, nil).TakesInsecureTLS {
		t.Error("a preset's provider shown as taking it")
	}
}
