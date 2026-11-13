package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/pluginrt"

	_ "github.com/dieqiyun/uni-switch/backend/plugins/all"
)

func builtinHome(t *testing.T) http.Handler {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	return Handler(nil, nil)
}

func postBuiltin(h http.Handler, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/plugins/builtin", strings.NewReader(body)))
	return rec
}

func TestUniSwitchNativeModulesStayEnabled(t *testing.T) {
	h := builtinHome(t)
	if err := pluginrt.SetPreset(pluginrt.PresetEssentials); err != nil {
		t.Fatal(err)
	}
	if err := pluginrt.Set("library", false); err != nil {
		t.Fatal(err)
	}
	for _, b := range builtinsState() {
		if !b.On {
			t.Fatalf("native module disabled by old plugin configuration: %+v", b)
		}
	}
	if rec := postBuiltin(h, "{}"); rec.Code != http.StatusNotFound {
		t.Fatalf("plugin manager still exposed: %d", rec.Code)
	}
}

func TestUniSwitchNativeModulesIgnoreBrokenLegacyConfig(t *testing.T) {
	builtinHome(t)
	if err := os.MkdirAll(filepath.Dir(pluginrt.Path()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pluginrt.Path(), []byte("plugins: [\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, b := range builtinsState() {
		if !b.On || b.Broken != "" {
			t.Fatalf("legacy config affected native module: %+v", b)
		}
	}
	if b, _ := os.ReadFile(pluginrt.Path()); string(b) != "plugins: [\n" {
		t.Fatalf("legacy data modified: %q", b)
	}
}

func TestUniSwitchBootKeepsNativePages(t *testing.T) {
	h := builtinHome(t)
	if err := pluginrt.SetPreset(pluginrt.PresetEssentials); err != nil {
		t.Fatal(err)
	}
	if err := pluginrt.Set("library", false); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/boot.js", nil))
	if !strings.Contains(rec.Body.String(), "\"featuresOff\":[]") {
		t.Fatalf("native page hidden by old plugin choice: %s", rec.Body)
	}
}

// boot.js says where the gateway listens when this process serves it, so
// the sidebar's line is drawn before the first paint (#3429), and nothing
// when it doesn't: one served by another uni-switch is the providers' read to
// find, never this process's guess.
func TestBootSaysWhereItsGatewayListens(t *testing.T) {
	h := builtinHome(t)
	t.Setenv("UNI_SWITCH_ADDR", "127.0.0.1:3481")
	boot := func() string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/boot.js", nil))
		return rec.Body.String()
	}
	prev := served.Swap(nil)
	t.Cleanup(func() { served.Store(prev) })
	if b := boot(); strings.Contains(b, "sideGateway") {
		t.Fatalf("no gateway served here: %s", b)
	}
	served.Store(gateway.New())
	if b := boot(); !strings.Contains(b, `"sideGateway":{"boot":true,"running":true,"url":"http://127.0.0.1:3481"}`) {
		t.Fatalf("served here: %s", b)
	}
	t.Setenv("UNI_SWITCH_ADDR", "0.0.0.0:3483")
	t.Setenv("UNI_SWITCH_PUBLIC_URL", "http://nas.local:3425")
	if b := boot(); !strings.Contains(b, `"url":"`+gateway.URL()+`"`) || strings.Contains(b, "nas.local") || strings.Contains(b, `"lan"`) || strings.Contains(b, `"lanURLs"`) || strings.Contains(b, `"publicURL"`) {
		t.Fatalf("legacy sharing leaked into boot state: %s", b)
	}
	t.Setenv("UNI_SWITCH_ADDR", "127.0.0.1:3483")
	if b := boot(); !strings.Contains(b, `"url":"http://127.0.0.1:3483"`) || strings.Contains(b, "nas.local") {
		t.Fatalf("local listener missing from boot state: %s", b)
	}
	served.Store(nil)
	if b := boot(); strings.Contains(b, "sideGateway") {
		t.Fatalf("stopped: %s", b)
	}
}

func TestUniSwitchNativeModuleDefaults(t *testing.T) {
	builtinHome(t)
	if err := pluginrt.SetConfig("gateway", map[string]any{"callsPage": 50}); err != nil {
		t.Fatal(err)
	}
	for _, b := range builtinsState() {
		if b.ID == "gateway" {
			if b.Config["callsPage"] != int64(20) {
				t.Fatalf("old plugin config applied: %+v", b)
			}
			return
		}
	}
	t.Fatal("native gateway module missing")
}

// Every file a built-in plugin says it has is on disk, loaded by the page
// and named with its hash so a new one isn't read from the cache: a script
// after the kernel and before app.js, which looks for its pages, a
// stylesheet in the head with the others.
func TestBuiltinClientsLoaded(t *testing.T) {
	page, err := os.ReadFile("assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	src := string(page)
	at := func(name string) int {
		tag := `<script src="` + name + `"></script>`
		if strings.HasSuffix(name, ".css") {
			tag = `<link rel="stylesheet" href="` + name + `">`
		}
		i := strings.Index(src, tag)
		if i < 0 {
			t.Fatalf("index.html doesn't load %s", name)
		}
		return i
	}
	kernel, app, start := at("kernel.js"), at("app.js"), at("start.js")
	if kernel > app || app > start {
		t.Fatal("kernel.js, app.js and start.js out of order")
	}
	n := 0
	for _, m := range pluginrt.Registered() {
		for _, c := range m.Client {
			name := "plugins/" + m.ID + "/" + c
			if _, err := os.Stat(filepath.Join("assets", name)); err != nil {
				t.Errorf("%s: %v", name, err)
			}
			attr := "src"
			if strings.HasSuffix(name, ".css") {
				attr = "href"
				if i := at(name); i > strings.Index(src, "</head>") {
					t.Errorf("%s isn't in the head", name)
				}
			} else if c == "page.js" {
				// a page's code that runs on app.js's helpers: after app.js,
				// before start.js shows the first page
				if i := at(name); i < app || i > start {
					t.Errorf("%s isn't between app.js and start.js", name)
				}
			} else if i := at(name); i < kernel || i > app {
				t.Errorf("%s isn't between kernel.js and app.js", name)
			}
			if !strings.Contains(string(versionedPage(page)), attr+`="`+name+`?v=`) {
				t.Errorf("%s isn't named with its hash", name)
			}
			n++
		}
	}
	if n == 0 {
		t.Fatal("no plugin has a script")
	}
}

// Every string of every built-in plugin is said in every language.
func TestBuiltinsTranslated(t *testing.T) {
	src, err := os.ReadFile("assets/i18n.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"zh: {", `"zh-TW": {`, "ja: {", "de: {"} {
		_, block, ok := strings.Cut(string(src), "\n  "+lang+"\n")
		if !ok {
			t.Fatalf("no %s block", lang)
		}
		block, _, _ = strings.Cut(block, "\n  },")
		for _, m := range pluginrt.Registered() {
			says := []string{m.Name, m.Sub, m.One, m.Many}
			if sc := m.Schema(); sc != nil {
				for _, p := range sc.Properties {
					says = append(says, p.Title, p.Description)
				}
			}
			for _, s := range says {
				if s == "" {
					continue
				}
				k, _ := json.Marshal(s)
				if !strings.Contains(block, "\n    "+string(k)+": ") {
					t.Errorf("%s doesn't say %s", lang, k)
				}
			}
		}
	}
}
