package gateway

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// noNamespaces answers as a vendor's Responses API that knows no
// namespaces does (refuse): a bare 400 for a request offering one, else a
// turn; without refuse, as a Codex backend such as sub2api takes them.
func noNamespaces(got *[]string, refuse bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*got = append(*got, string(b))
		if refuse && hasImageTool(string(b)) {
			w.WriteHeader(400)
			io.WriteString(w, `{"error":{"message":"bad response status code 400","type":"invalid_request_error"}}`)
			return
		}
		w.Header()["Content-Type"] = nil
		io.WriteString(w, sse(`data: {"type":"response.output_text.delta","delta":"OK"}`,
			`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}}`,
			`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[]}}`))
	}
}

// hasImageTool reports whether a request offers Codex's image tool, in its
// namespace or flat as uni-switch translates it.
func hasImageTool(body string) bool {
	return strings.Contains(body, `"type":"namespace"`) || strings.Contains(body, `"image_gen__imagegen"`)
}

// Codex on a uni-switch model offers its image tool, as #870 has it, in the
// namespace image_gen (testdata: what Codex 0.160.0 sends, uni-switch's
// provider table and catalog). A vendor that knows no namespaces turned
// every chat away with a bare 400 (#949, AnyRouter's "bad response status
// code 400" to "只回复 OK"). It is asked again without the tool, and not
// sent it again once that worked; a vendor that takes it still gets it.
func TestImageToolLeftOutWhereRefused(t *testing.T) {
	tool, err := os.ReadFile("testdata/codex_image_gen_tool.json")
	if err != nil {
		t.Fatal(err)
	}
	// Codex's own web_search has uni-switch translate the request and search
	// for the vendor, which then gets the tool flat; without it the body
	// passes through with the namespace
	for name, search := range map[string]string{
		"translated":     `,{"type":"web_search","external_web_access":false}`,
		"passed through": "",
	} {
		t.Run(name, func(t *testing.T) {
			fresh(t)
			body := `{"model":"%s","stream":true,"store":false,"tools":[{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{}}},` +
				strings.TrimSpace(string(tool)) + search + `],` +
				`"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"只回复 OK"}]}]}`
			ask := func(s *Server, model string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(strings.Replace(body, "%s", model, 1)))
				s.Handler().ServeHTTP(rec, req)
				return rec
			}

			var refusing, taking []string
			strict := httptest.NewServer(noNamespaces(&refusing, true))
			t.Cleanup(strict.Close)
			backend := httptest.NewServer(noNamespaces(&taking, false))
			t.Cleanup(backend.Close)
			for _, p := range []provider.Provider{
				{ID: "strict", Name: "Strict", Key: "k", Models: []string{"m1"}, Responses: strict.URL + "/v1"},
				{ID: "backend", Name: "Backend", Key: "k", Models: []string{"m1"}, Responses: backend.URL + "/v1"},
			} {
				if err := provider.Save(p); err != nil {
					t.Fatal(err)
				}
			}
			s := New()

			rec := ask(s, "strict/m1")
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), "OK") {
				t.Fatalf("strict: %d %s", rec.Code, rec.Body)
			}
			if len(refusing) != 2 || !hasImageTool(refusing[0]) || hasImageTool(refusing[1]) {
				t.Fatalf("strict was sent %d requests, want the tool then none: %.600s", len(refusing), refusing[0])
			}
			for _, want := range []string{`"exec_command"`, "只回复 OK"} {
				if !strings.Contains(refusing[1], want) {
					t.Errorf("retry lacks %s: %s", want, refusing[1])
				}
			}
			rec = ask(s, "strict/m1")
			if rec.Code != 200 || len(refusing) != 3 || hasImageTool(refusing[2]) {
				t.Fatalf("strict again: %d, %d requests, tool sent: %v", rec.Code, len(refusing), hasImageTool(refusing[len(refusing)-1]))
			}

			rec = ask(s, "backend/m1")
			if rec.Code != 200 || len(taking) != 1 || !hasImageTool(taking[0]) {
				t.Fatalf("backend: %d, %d requests, tool sent: %v", rec.Code, len(taking), len(taking) > 0 && hasImageTool(taking[0]))
			}
		})
	}
}

// A 400 that isn't over the tool is the vendor's answer: asked once more
// without it, and the tool is not given up for that provider.
func TestImageToolKeptOverAnotherRefusal(t *testing.T) {
	fresh(t)
	var got []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, string(b))
		w.WriteHeader(400)
		io.WriteString(w, `{"error":{"message":"context too long"}}`)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "up", Name: "Up", Key: "k", Models: []string{"m1"}, Responses: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	s := New()
	body := `{"model":"up/m1","stream":true,"tools":[{"type":"namespace","name":"image_gen","tools":[{"type":"function","name":"imagegen"}]}],"input":"hi"}`
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body)))
		if rec.Code != 400 {
			t.Fatalf("%d: %d %s", i, rec.Code, rec.Body)
		}
	}
	if len(got) != 4 || !hasImageTool(got[2]) {
		t.Fatalf("sent %d requests; the second turn's first had the tool: %v", len(got), len(got) > 2 && hasImageTool(got[2]))
	}
}

// A ChatGPT sign-in is the backend #870 offers the tool for: it gets it on
// every request, and a 400 from it is its answer, not a sign the tool is
// what it turned away.
func TestImageToolKeptForChatGPT(t *testing.T) {
	tool, err := os.ReadFile("testdata/codex_image_gen_tool.json")
	if err != nil {
		t.Fatal(err)
	}
	body := `{"model":"codex/gpt-6-luna","stream":true,"store":false,"tools":[{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{}}},` +
		strings.TrimSpace(string(tool)) + `],"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"只回复 OK"}]}]}`
	for _, refuse := range []bool{false, true} {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
		restingUntil.Lock()
		restingUntil.m = map[string]time.Time{}
		restingUntil.Unlock()
		claims := func(m map[string]any) string {
			b, _ := json.Marshal(m)
			return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
		}
		os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
		os.WriteFile(filepath.Join(home, ".codex", "auth.json"), mustJSON(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
			"id_token": claims(map[string]any{"email": "me@example.com",
				"https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "plus"}}),
			"access_token":  claims(map[string]any{"exp": time.Now().Add(time.Hour).Unix()}),
			"refresh_token": "r", "account_id": "acct-1"}}), 0o600)

		var mu sync.Mutex
		var got []string
		answer := noNamespaces(&got, refuse)
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/models") {
				io.WriteString(w, `{"models":[{"slug":"gpt-6-luna","visibility":"list"}]}`)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			answer(w, r)
		}))
		old := provider.CodexBase
		provider.CodexBase = up.URL + "/backend-api/codex"

		code, out := post(t, "/v1/responses", body)
		code2, _ := post(t, "/v1/responses", body)
		provider.CodexBase = old
		up.Close()
		mu.Lock()
		sent := got
		mu.Unlock()
		if want := map[bool]int{false: 200, true: 400}[refuse]; code != want || code2 != want {
			t.Fatalf("refusing %v: %d, %d, want %d: %s", refuse, code, code2, want, out)
		}
		if len(sent) == 0 {
			t.Fatalf("refusing %v: nothing reached the backend", refuse)
		}
		for i, b := range sent {
			if !hasImageTool(b) {
				t.Fatalf("refusing %v: request %d of %d went without the tool", refuse, i+1, len(sent))
			}
		}
	}
}
