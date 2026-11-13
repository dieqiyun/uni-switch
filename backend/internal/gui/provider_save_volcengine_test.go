package gui

// A Volcengine Ark provider's editor shows the account's control-plane
// AccessKey ID and whether a Secret is saved, never the Secret itself (#1427).
// A Save that leaves the Secret blank keeps the saved one, as a key's own
// Save does; one that doesn't name the access key keeps it; Remove
// (clearAccessKey) drops both.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func TestProviderSaveKeepsVolcengineSecret(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	post := func(body string) {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/save", strings.NewReader(body)))
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}

	v, err := provider.FromPreset("volcengine")
	if err != nil {
		t.Fatal(err)
	}
	v.Key = "ark-key"
	v.AccessKeyID, v.SecretAccessKey = "AK-1", "SK-1"
	if err := provider.Save(v); err != nil {
		t.Fatal(err)
	}
	// the editor's own save: its form's fields, and the AccessKey ID it
	// typed, with the Secret field left blank
	base := `"id":"volcengine","from":"volcengine","name":"Volcengine Ark","preset":"volcengine","key":"","chat":"` +
		v.Chat + `","responses":"` + v.Responses + `","anthropic":"` + v.Anthropic +
		`","catalog":"volcengine","models":[],"headers":{},"searches":false,"pinUpstream":false,"unredacted":false,"contexts":{},"outputs":{},"compacts":{},"proxy":"","maxConcurrency":null,"queueLimit":0,"queueWait":0,"priceRate":null`

	post(`{` + base + `,"accessKeyID":"AK-2"}`)
	p, err := provider.Find("volcengine")
	if err != nil {
		t.Fatal(err)
	}
	if p.AccessKeyID != "AK-2" || p.SecretAccessKey != "SK-1" {
		t.Errorf("blank Secret: ak=%q sk=%q, want AK-2/SK-1", p.AccessKeyID, p.SecretAccessKey)
	}

	// a typed Secret replaces the saved one
	post(`{` + base + `,"accessKeyID":"AK-2","secretAccessKey":"SK-2"}`)
	if p, _ = provider.Find("volcengine"); p.SecretAccessKey != "SK-2" {
		t.Errorf("typed Secret: %q", p.SecretAccessKey)
	}

	// a save from elsewhere that doesn't name the access key keeps it
	post(`{` + base + `}`)
	if p, _ = provider.Find("volcengine"); p.AccessKeyID != "AK-2" || p.SecretAccessKey != "SK-2" {
		t.Errorf("not named: ak=%q sk=%q", p.AccessKeyID, p.SecretAccessKey)
	}

	// the listing tells the editor the AccessKey ID and that a Secret is
	// saved, and never the Secret
	list := func() (string, map[string]*struct {
		ID        string `json:"id"`
		SecretSet bool   `json:"secretSet"`
	}, map[string]bool) {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/providers", nil))
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		var got struct {
			Providers []struct {
				ID        string `json:"id"`
				AccessKey *struct {
					ID        string `json:"id"`
					SecretSet bool   `json:"secretSet"`
				} `json:"accessKey"`
			} `json:"providers"`
			Presets []struct {
				ID        string `json:"id"`
				AccessKey bool   `json:"accessKey"`
			} `json:"presets"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		ps := map[string]*struct {
			ID        string `json:"id"`
			SecretSet bool   `json:"secretSet"`
		}{}
		for _, x := range got.Providers {
			ps[x.ID] = x.AccessKey
		}
		pre := map[string]bool{}
		for _, x := range got.Presets {
			pre[x.ID] = x.AccessKey
		}
		return w.Body.String(), ps, pre
	}
	body, ps, pre := list()
	if strings.Contains(body, "SK-2") || strings.Contains(body, "secretAccessKey") {
		t.Error("the listing told the Secret")
	}
	if a := ps["volcengine"]; a == nil || a.ID != "AK-2" || !a.SecretSet {
		t.Errorf("provider json: %+v", a)
	}
	if !pre["volcengine"] || pre["deepseek"] {
		t.Errorf("presets: %v", pre)
	}

	// an editor opened before the access key was given (in another window,
	// or before a sync brought it) sends the ID blank and no Secret: both
	// are kept, as only Remove drops them (#1821)
	post(`{` + base + `,"accessKeyID":""}`)
	if p, _ = provider.Find("volcengine"); p.AccessKeyID != "AK-2" || p.SecretAccessKey != "SK-2" {
		t.Errorf("blank ID: ak=%q sk=%q, want AK-2/SK-2", p.AccessKeyID, p.SecretAccessKey)
	}

	// Remove clears both, and the editor still asks for them
	post(`{` + base + `,"accessKeyID":"","clearAccessKey":true}`)
	if p, _ = provider.Find("volcengine"); p.AccessKeyID != "" || p.SecretAccessKey != "" || p.Key != "ark-key" {
		t.Errorf("removed: %+v", p)
	}
	if _, ps, _ = list(); ps["volcengine"] == nil || ps["volcengine"].ID != "" || ps["volcengine"].SecretSet {
		t.Errorf("removed, listed: %+v", ps["volcengine"])
	}

	// another vendor is asked for none
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Chat: "https://relay.example.com/v1", Key: "k"}); err != nil {
		t.Fatal(err)
	}
	if _, ps, _ = list(); ps["relay"] != nil {
		t.Errorf("relay: %+v", ps["relay"])
	}

	// an Ark provider added with its access key keeps it
	post(`{"new":true,"preset":"volcengine","id":"volc-2","name":"Ark 2","key":"ark-2","chat":"` + v.Chat + `","accessKeyID":" AK-9 ","secretAccessKey":" SK-9 "}`)
	if p, err = provider.Find("volc-2"); err != nil || p.AccessKeyID != "AK-9" || p.SecretAccessKey != "SK-9" {
		t.Errorf("added: %+v %v", p, err)
	}
}
