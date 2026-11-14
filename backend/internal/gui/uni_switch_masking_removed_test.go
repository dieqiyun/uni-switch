package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func TestRetiredMaskingEndpointsAndAssetsAreAbsent(t *testing.T) {
	sandboxHome(t)
	h := Handler(nil, nil)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/masking/summary"}, {"GET", "/api/masking/records"},
		{"GET", "/api/masking/record"}, {"GET", "/api/masking/value"},
		{"POST", "/api/masking/preview"}, {"POST", "/api/masking/allow"},
		{"POST", "/api/masking/clear"}, {"POST", "/api/masking/values"},
		{"GET", "/api/privacy-model"}, {"POST", "/api/privacy-model"},
		{"POST", "/api/settings/redact-route"}, {"POST", "/api/settings/redact-rules"},
		{"POST", "/api/settings/redact-rule"}, {"POST", "/api/settings/redact-actions"},
		{"POST", "/api/settings/redact-allow"}, {"POST", "/api/local-hosts"},
		{"GET", "/plugins/redact/client.js"}, {"GET", "/plugins/redact/page.js"},
		{"GET", "/plugins/redact/settings.js"}, {"GET", "/ruleedit.js"},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`)))
		if w.Code != http.StatusNotFound {
			t.Errorf("retired %s %s: %d", tc.method, tc.path, w.Code)
		}
	}
}

func TestRetiredMaskingSettingsAreNotKeptOrExposed(t *testing.T) {
	sandboxHome(t)
	const legacy = `{"theme":"dark","redact":true,"redactPersonal":true,"redactWords":["Nightjar"],"redactActions":{"API_KEY":"block"},"redactModel":true,"redactModelName":"obsolete","redactRouteTo":"absent/model","maskAlert":true}`
	if err := os.MkdirAll(filepath.Dir(settings.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings.Path(), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	h := Handler(nil, nil)
	for _, method := range []string{"GET", "POST"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, "/api/settings", strings.NewReader(legacy)))
		if w.Code != http.StatusOK {
			t.Fatalf("%s settings: %d %s", method, w.Code, w.Body)
		}
		var exposed map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &exposed); err != nil {
			t.Fatal(err)
		}
		for key := range exposed {
			if strings.HasPrefix(key, "redact") || key == "maskAlert" || key == "maskRouteModels" {
				t.Errorf("retired setting exposed: %s", key)
			}
		}
	}
	data, err := os.ReadFile(settings.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"redact`) || strings.Contains(string(data), `"maskAlert"`) || settings.Load().Theme != "dark" {
		t.Fatalf("retired settings retained or ordinary setting lost: %s", data)
	}
	for _, name := range []string{"redact.key", "masking", "pii"} {
		if _, err := os.Stat(filepath.Join(settings.Dir(), name)); !os.IsNotExist(err) {
			t.Errorf("retired state created: %s (%v)", name, err)
		}
	}
}
