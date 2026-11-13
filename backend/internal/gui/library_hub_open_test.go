package gui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/library"
)

// opens records the links handed to the system, opening none.
type opens struct {
	webHost
	links []string
}

func (o *opens) OpenURL(u string) error { o.links = append(o.links, u); return nil }

// The Connect sheet's "Add to Cursor" names the app, and the window opens
// that app's install link for the hub, built here; an app with no link is
// an error and nothing is opened. The page never sends a link to open.
func TestHubOpenInstallLink(t *testing.T) {
	sandboxHome(t)
	o := &opens{}
	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		Handler(o, nil).ServeHTTP(rec, httptest.NewRequest("POST", "/api/library/servers/hub-open", strings.NewReader(body)))
		return rec
	}
	for _, app := range []string{"cursor", "vscode"} {
		if rec := post(`{"app":"` + app + `"}`); rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", app, rec.Code, rec.Body)
		}
		want, _ := library.HubLink(app)
		if len(o.links) == 0 || o.links[len(o.links)-1] != want {
			t.Fatalf("%s opened %q, want %q", app, o.links, want)
		}
	}
	for _, body := range []string{`{"app":"javascript:alert(1)"}`, `{"app":"https://evil.example"}`, `{}`} {
		if rec := post(body); rec.Code == http.StatusOK {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}
	if len(o.links) != 2 {
		t.Fatalf("opened %q", o.links)
	}
}
