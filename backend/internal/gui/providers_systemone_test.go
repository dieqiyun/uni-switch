package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The editor's System One: a custom provider's decision API is probed at
// POST …/systemone and saved with any model name, and Bailian's preset
// takes the workspace's host (#647).
func TestProviderSystemOne(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer sk-1" {
			http.Error(w, `{"error":{"message":"no"}}`, http.StatusNotFound)
			return
		}
		w.Write([]byte(`{"answers":{"ok":{"type":"noul","noul":0.9}}}`))
	}))
	defer vendor.Close()
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	post := func(action, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/"+action, strings.NewReader(body)))
		return w
	}
	w := post("detect", `{"typed":true,"key":"sk-1","decide":"`+vendor.URL+`/v1","base":"`+vendor.URL+`/v1","model":"my-decider"}`)
	var got struct{ Results []provider.Detection }
	json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || len(got.Results) != 1 || !got.Results[0].OK || got.Results[0].Protocol != "decide" || got.Results[0].Model != "my-decider" {
		t.Fatalf("detect: %d %s", w.Code, w.Body)
	}
	if w := post("save", `{"name":"Decider","decide":"`+vendor.URL+`/v1","key":"sk-1","models":["my-decider"],"new":true}`); w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	if p, err := provider.Find("decider"); err != nil || !p.DecideOnly() || p.Jev() != "my-decider" {
		t.Fatalf("saved: %+v %v", p, err)
	}
	if w := post("save", `{"preset":"bailian-decision","key":"sk-2","new":true}`); w.Code == 200 {
		t.Fatalf("Bailian saved with no workspace: %s", w.Body)
	}
	if w := post("save", `{"preset":"bailian-decision","key":"sk-2","decide":"https://ws-9.cn-beijing.maas.aliyuncs.com/compatible-mode/v1","new":true}`); w.Code != 200 {
		t.Fatalf("Bailian: %d %s", w.Code, w.Body)
	}
	if p, err := provider.Find("bailian-decision"); err != nil || p.Decide != "https://ws-9.cn-beijing.maas.aliyuncs.com/compatible-mode/v1" || p.Jev() != provider.BailianDecision {
		t.Fatalf("Bailian saved: %+v %v", p, err)
	}
}
