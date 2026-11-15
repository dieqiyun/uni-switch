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

func TestRetiredObservabilityAssetsAreAbsent(t *testing.T) {
	sandboxHome(t)
	h := Handler(nil, nil)
	for _, path := range []string{"/plugins/otel/page.js", "/plugins/otel/i18n.js"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("retired asset %s: %d", path, w.Code)
		}
	}
}

func TestRetiredObservabilitySettingsAreIgnored(t *testing.T) {
	sandboxHome(t)
	// Malformed retired preferences and environment values cannot block an
	// unrelated save or reactivate the removed exporter.
	t.Setenv("UNI_SWITCH_OTEL_ENABLED", "retired-not-a-bool")
	t.Setenv("UNI_SWITCH_OTEL_ENDPOINT", "not-a-url")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "retired")
	const legacy = `{"theme":"dark","otel":{"enabled":true,"endpoint":"invalid endpoint","headers":{"Content-Type":"retired"},"metrics":true,"sessions":true,"bodies":true,"bodiesWhole":true}}`
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
			if key == "otel" || key == "otelEnv" {
				t.Errorf("retired setting exposed: %s", key)
			}
		}
	}
	data, err := os.ReadFile(settings.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"otel"`) || settings.Load().Theme != "dark" {
		t.Fatalf("retired settings retained or ordinary setting lost: %s", data)
	}
}

func TestRetiredObservabilityEnvironmentCannotEnableExport(test *testing.T) {
	sandboxHome(test)
	for _, name := range []string{"UNI_SWITCH_OTEL_ENABLED", "UNI_SWITCH_OTEL_METRICS", "UNI_SWITCH_OTEL_SESSIONS", "UNI_SWITCH_OTEL_BODIES", "UNI_SWITCH_OTEL_BODIES_WHOLE"} {
		test.Setenv(name, "true")
	}
	test.Setenv("UNI_SWITCH_OTEL_ENDPOINT", "http://127.0.0.1:4318")
	test.Setenv("UNI_SWITCH_OTEL_HEADERS", "Authorization=Bearer%20synthetic-retired-key")
	test.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.name=retired")
	test.Setenv("OTEL_SERVICE_NAME", "retired")
	handler := Handler(nil, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("POST", "/api/settings", strings.NewReader(`{"theme":"dark","otel":{"enabled":true,"metrics":true,"sessions":true,"bodies":true,"bodiesWhole":true}}`)))
	if response.Code != http.StatusOK {
		test.Fatalf("save: %d %s", response.Code, response.Body)
	}
	var exposed map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &exposed); err != nil {
		test.Fatal(err)
	}
	for _, key := range []string{"otel", "otelEnv"} {
		if _, exists := exposed[key]; exists {
			test.Errorf("retired environment exposed %s", key)
		}
	}
	data, err := os.ReadFile(settings.Path())
	if err != nil {
		test.Fatal(err)
	}
	if strings.Contains(string(data), "otel") || settings.Load().Theme != "dark" {
		test.Fatalf("retired environment changed settings: %s", data)
	}
}
