package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// codexOwnOffUniSwitchSetup: Codex signed in to ChatGPT, its models cache
// listing gpt-6.1-sol, and no ChatGPT account in uni-switch serving it: the
// sign-in removed from uni-switch's Providers (hidden), or with discoveryOff
// subscription accounts turned off. The ChatGPT backend stands in and
// notes what it was sent.
func codexOwnOffUniSwitchSetup(t *testing.T, discoveryOff bool) (got *http.Header, models *[]string) {
	t.Helper()
	codexSignedIn(t)
	if discoveryOff {
		t.Setenv("UNI_SWITCH_NO_ACCOUNTS", "1")
	} else if err := provider.Delete("codex"); err != nil {
		t.Fatal(err)
	}
	provider.ForgetAccounts()
	home := os.Getenv("HOME")
	os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"),
		[]byte(`{"etag":"e1","models":[{"slug":"gpt-6.1-sol","display_name":"gpt-6.1-sol","priority":1}]}`), 0o600)
	got, models = &http.Header{}, &[]string{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*got = r.Header.Clone()
		*models = append(*models, modelOf(b))
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"r1","model":"gpt-6.1-sol"}}`,
			`data: {"type":"response.output_text.delta","delta":"pong"}`,
			`data: {"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":7,"output_tokens":1}}}`))
	}))
	t.Cleanup(up.Close)
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })
	return got, models
}

func codexOnUniSwitchRequest(from string) *http.Request {
	req := httptest.NewRequest("POST", "/v1/responses",
		strings.NewReader(`{"model":"gpt-6.1-sol","stream":true,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"ping"}]}]}`))
	req.RemoteAddr = from
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "codex_cli_rs/0.164.0 (Mac OS 26.2.0; arm64) Terminal")
	req.Header.Set("Originator", "codex_cli_rs")
	req.Header.Set("Authorization", "Bearer "+Token)
	req.Header.Set("X-Openai-Actor-Authorization", "uni-switch")
	return req
}

// A thread Codex started while uni-switch was its provider keeps sending to
// uni-switch's /v1 after Codex is disconnected or set back to ChatGPT; one of
// Codex's own models picked in it, which no account in uni-switch serves,
// goes to the ChatGPT backend on Codex's own sign-in, uni-switch's key and
// header left behind, where it was answered 404 "uni-switch knows no model
// \"gpt-6.1-sol\"" (iuengei on Discord).
func TestCodexOwnModelOnUniSwitchThreadGoesOnSignIn(t *testing.T) {
	got, models := codexOwnOffUniSwitchSetup(t, false)
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, codexOnUniSwitchRequest("127.0.0.1:51000"))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "pong") {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	if len(*models) != 1 || (*models)[0] != "gpt-6.1-sol" {
		t.Fatalf("backend asked for %v", *models)
	}
	auth := got.Get("Authorization")
	if auth == "" || strings.Contains(auth, Token) {
		t.Fatalf("Authorization %q: want Codex's sign-in, not uni-switch's key", auth)
	}
	if got.Get("Chatgpt-Account-Id") != "acct-1" {
		t.Fatalf("chatgpt-account-id %q, want acct-1", got.Get("Chatgpt-Account-Id"))
	}
	if v := got.Get("X-Openai-Actor-Authorization"); v != "" {
		t.Fatalf("uni-switch's provider header went to OpenAI: %q", v)
	}
}

// Not for a request from another computer, nor a gateway key held to some
// models, nor a model that isn't OpenAI's: those are answered as before,
// nothing sent on Codex's sign-in.
func TestCodexOwnModelOnUniSwitchThreadStaysHere(t *testing.T) {
	t.Run("another computer", func(t *testing.T) {
		_, models := codexOwnOffUniSwitchSetup(t, false)
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, codexOnUniSwitchRequest("192.0.2.7:51000"))
		if len(*models) != 0 {
			t.Fatalf("a request from another computer went on Codex's sign-in: %d %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("held key", func(t *testing.T) {
		_, models := codexOwnOffUniSwitchSetup(t, false)
		req := codexOnUniSwitchRequest("127.0.0.1:51000")
		req = req.WithContext(access.WithIdentity(req.Context(), access.Identity{KeyID: "k1", Models: []string{"fake/*"}}))
		rec := httptest.NewRecorder()
		New().handle(provider.Responses)(rec, req)
		if len(*models) != 0 {
			t.Fatalf("a held key went on Codex's sign-in: %d %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("not OpenAI's", func(t *testing.T) {
		_, models := codexOwnOffUniSwitchSetup(t, false)
		req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"no-such-model-xyz","input":"hi"}`))
		req.RemoteAddr = "127.0.0.1:51000"
		req.Header = codexOnUniSwitchRequest("127.0.0.1:51000").Header
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, req)
		if len(*models) != 0 || rec.Code != 404 {
			t.Fatalf("an unknown model: %d %s, sent %v", rec.Code, rec.Body.String(), *models)
		}
	})
}

// With subscription accounts off, uni-switch reads no sign-in, Codex's
// neither: the request is answered why it can't be served and what to do,
// not with the bare "uni-switch knows no model".
func TestCodexOwnModelOnUniSwitchThreadSaysWhy(t *testing.T) {
	_, models := codexOwnOffUniSwitchSetup(t, true)
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, codexOnUniSwitchRequest("127.0.0.1:51000"))
	body := rec.Body.String()
	if rec.Code != 404 || len(*models) != 0 {
		t.Fatalf("got %d %s, sent %v", rec.Code, body, *models)
	}
	for _, want := range []string{"one of Codex's own models", "started while uni-switch was Codex's model provider", "subscription accounts are off", "Start a new thread"} {
		if !strings.Contains(body, want) {
			t.Errorf("answer lacks %q: %s", want, body)
		}
	}
}

// codexOwnSideCall is one of Codex's hidden side calls (a thread's title
// or description) sent to uni-switch's own /v1 on one of Codex's own models,
// as it is when the thread was started while uni-switch was Codex's provider
// (model_provider = "uni-switch", whose base_url is uni-switch's /v1) and Codex
// asks on its own title model, gpt-5.6-luna.
func codexOwnSideCall(t *testing.T, kind, model string) *httptest.ResponseRecorder {
	t.Helper()
	var text, schema string
	switch kind {
	case "thread_title":
		text = "Generate a concise title. User prompt:\nfix the login bug"
		schema = `{"type":"object","properties":{"title":{"type":"string","minLength":1,"maxLength":36}},"required":["title"],"additionalProperties":false}`
	case "thread_description":
		text = "Describe the thread"
		schema = `{"type":"object","properties":{"description":{"type":"string","minLength":1}},"required":["description"],"additionalProperties":false}`
	}
	body := `{"model":"` + model + `","stream":true,"reasoning":{"effort":"low"},"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":` + strconvQuote(text) + `}]}],` +
		`"text":{"format":{"type":"json_schema","name":"codex_output_schema","strict":true,"schema":` + schema + `}}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:51000"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "codex_cli_rs/0.164.0 (Mac OS 26.2.0; arm64) Terminal")
	req.Header.Set("Originator", "codex_cli_rs")
	req.Header.Set("Authorization", "Bearer "+Token)
	req.Header.Set("session_id", kind+"-thread")
	req.Header.Set("x-codex-turn-metadata", `{"thread_source":"`+kind+`","parent_thread_id":"main-chat"}`)
	New().Handler().ServeHTTP(rec, req)
	return rec
}

// codexOwnSideCallEnv is shruru's machine for #4590: Codex never signed in
// to ChatGPT (no ~/.codex/auth.json, on purpose), so uni-switch can read no
// sign-in of its; its models cache still lists gpt-5.6-luna, and uni-switch
// has no ChatGPT account either. The titles and descriptions settings name
// a model of uni-switch's.
func codexOwnSideCallEnv(t *testing.T) *fake {
	t.Helper()
	fresh(t) // no agent signed in anywhere
	f := &fake{t: t}
	setup(t, provider.Chat, f)
	home := os.Getenv("HOME")
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"),
		[]byte(`{"etag":"e1","models":[{"slug":"gpt-5.6-luna","display_name":"gpt-5.6-luna","priority":1}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	provider.ForgetAccounts()
	t.Cleanup(provider.ForgetAccounts)
	save := func(name, v string) {
		t.Helper()
		st := settings.Load()
		switch name {
		case "titles":
			st.CodexTitles = v
		case "descriptions":
			st.CodexDescriptions = v
		}
		if err := settings.Save(st); err != nil {
			t.Fatal(err)
		}
	}
	save("titles", "fake/m1")
	save("descriptions", "fake/m1")
	return f
}

// A hidden side call of Codex's that lands on /v1 on one of Codex's own
// models — a thread's title or description, the thread started while
// uni-switch was Codex's provider — is uni-switch's to answer (codexTitlesTo and
// codexDescriptionsTo, further down handle), never to put on Codex's
// ChatGPT sign-in: with no sign-in to be had (no ChatGPT account in
// uni-switch, Codex itself never signed in) the request was answered 404 and
// Codex named the thread after its first message, whatever the settings
// said (#4590).
func TestCodexSideCallOnOwnModelNotPutOnSignIn(t *testing.T) {
	t.Run("title", func(t *testing.T) {
		f := codexOwnSideCallEnv(t)
		f.reply = sse(
			`data: {"id":"c1","choices":[{"index":0,"delta":{"content":"Fix login bug"}}]}`,
			`data: {"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":30,"completion_tokens":4}}`,
			`data: [DONE]`)
		rec := codexOwnSideCall(t, "thread_title", "gpt-5.6-luna")
		if text, _ := outputText(t, rec.Body.String()); rec.Code != 200 || text != `{"title":"Fix login bug"}` || f.calls != 1 {
			t.Fatalf("a title on gpt-5.6-luna: %d %q, upstream %d", rec.Code, rec.Body.String(), f.calls)
		}
		var sent struct {
			Model string `json:"model"`
		}
		json.Unmarshal(f.got, &sent)
		if sent.Model != "m1" {
			t.Errorf("the titles model was asked for %q", sent.Model)
		}
	})
	t.Run("description", func(t *testing.T) {
		f := codexOwnSideCallEnv(t)
		f.reply = sse(
			`data: {"id":"c1","choices":[{"index":0,"delta":{"content":"Reviewed the routing change"}}]}`,
			`data: {"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":30,"completion_tokens":8}}`,
			`data: [DONE]`)
		rec := codexOwnSideCall(t, "thread_description", "gpt-5.6-luna")
		if text, _ := outputText(t, rec.Body.String()); rec.Code != 200 || text != `{"description":"Reviewed the routing change"}` || f.calls != 1 {
			t.Fatalf("a description on gpt-5.6-luna: %d %q, upstream %d", rec.Code, rec.Body.String(), f.calls)
		}
	})
}

// With no setting naming where a side call goes, a title asked on one of
// Codex's own models still goes on Codex's sign-in, as every turn of the
// thread does: uni-switch has nothing of its own to answer it with, and
// before #4590 that is where it was answered.
func TestCodexSideCallOnOwnModelUnsetGoesOnSignIn(t *testing.T) {
	_, models := codexOwnOffUniSwitchSetup(t, false)
	for _, kind := range []string{"thread_title", "thread_description", "thread_summary", "composer_predictions"} {
		*models = nil
		req := codexOnUniSwitchRequest("127.0.0.1:51000")
		req.Header.Set("x-codex-turn-metadata", `{"thread_source":"`+kind+`","parent_thread_id":"main-chat"}`)
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, req)
		if len(*models) != 1 || (*models)[0] != "gpt-6.1-sol" {
			t.Errorf("%s with no setting: %d %s, backend asked for %v", kind, rec.Code, rec.Body.String(), *models)
		}
	}
}
