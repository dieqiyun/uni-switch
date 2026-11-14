package gateway

import (
	"encoding/base64"

	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// A gateway key held to some accounts (#905) is shown in /v1/models only
// the models an account or key it may use serves: one every allowed
// account is set not to serve (#474) is another's, not its.
func TestGatewayKeyAccountsListModels(t *testing.T) {
	heads := twoAccounts(t)
	if err := provider.Save(provider.Provider{ID: "codex", Models: []string{"gpt-5.5", "gpt-5.4-mini"}}); err != nil {
		t.Fatal(err)
	}
	s := New()
	keys, secrets := newCaller(t, "Held")
	if _, err := access.Update("accounts-key", access.Change{Key: keys[0].ID, Accounts: []string{"codex/me@example.com"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetAccountModels("codex", "me@example.com", []string{"gpt-5.4-mini"}); err != nil {
		t.Fatal(err)
	}
	list := func(secret string) string {
		t.Helper()
		r := httptest.NewRequest("GET", "/v1/models", nil)
		if secret != "" {
			r.Header.Set("Authorization", "Bearer "+secret)
		} else {
			r.RemoteAddr = "127.0.0.1:5000" // this computer asks without a key
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Body.String()
	}
	if got := list(secrets[0]); strings.Contains(got, `"codex/gpt-5.5"`) || !strings.Contains(got, `"codex/gpt-5.4-mini"`) {
		t.Fatalf("the key's models: %s", got)
	}
	// a provider with no account and no key (a local ollama) is one the
	// key names no account of: not its while it lists no model (Linka on
	// Discord); among the models it lists, its models stay listed, as its
	// requests pass (#905)
	if err := provider.Save(provider.Provider{ID: "local", Name: "Local", Chat: "http://127.0.0.1:11434/v1", Models: []string{"tiny"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("local", "http://127.0.0.1:11434/v1", []catalog.Model{{ID: "tiny"}}); err != nil {
		t.Fatal(err)
	}
	if got := list(secrets[0]); strings.Contains(got, `"local/tiny"`) {
		t.Fatalf("an account-only key lists another provider: %s", got)
	}
	if _, err := access.Update("models-key", access.Change{Key: keys[0].ID, Models: []string{"codex/*", "local/*", "relay/*"}}); err != nil {
		t.Fatal(err)
	}
	if got := list(secrets[0]); !strings.Contains(got, `"local/tiny"`) {
		t.Fatalf("a provider with no account or key: %s", got)
	}
	// an entry kept after its provider lost its last key holds the key
	// closer: the provider it names serves none of its models
	if err := catalog.SaveLive("relay", "http://127.0.0.1:2/v1", []catalog.Model{{ID: "m"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Chat: "http://127.0.0.1:2/v1", Key: "sk-r", Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := access.Update("accounts-key", access.Change{Key: keys[0].ID, Accounts: []string{"codex/me@example.com", "relay/" + provider.KeyID("sk-r")}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Chat: "http://127.0.0.1:2/v1", Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	if got := list(secrets[0]); strings.Contains(got, `"relay/m"`) || !strings.Contains(got, `"local/tiny"`) {
		t.Fatalf("a named provider without a key: %s", got)
	}
	// a keyless request sees both, and the accounts hold no model back
	if got := list(""); !strings.Contains(got, `"codex/gpt-5.5"`) || !strings.Contains(got, `"codex/gpt-5.4-mini"`) {
		t.Fatalf("a keyless list: %s", got)
	}
	if len(*heads) != 0 {
		t.Fatal("a list reached a provider", *heads)
	}
}

// A gateway key held to some accounts (#905) is drawn with by those alone:
// the whole fan-out of a subscription's accounts, not only its first. One
// of the key's whose plan refuses to draw doesn't hand the request to an
// account it may not use; the second in the order, when it is the key's,
// draws what the first may not; a keyless request fans out as before.
func TestGatewayKeyAccountsDrawOnAllowedAccounts(t *testing.T) {
	var drew []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acct := r.Header.Get("chatgpt-account-id")
		drew = append(drew, acct)
		w.Header().Set("Content-Type", "application/json")
		if acct == "acct-1" { // a plan without images, as chatgpt.com refuses
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"detail":{"message":"Forbidden"}}`)
			return
		}
		io.WriteString(w, `{"created":1,"data":[{"b64_json":"eA=="}]}`)
	}))
	t.Cleanup(up.Close)
	codexSignedIn(t, "spare@example.com")
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })
	if err := provider.Save(provider.Provider{ID: "codex", Models: []string{"gpt-image-2"}}); err != nil {
		t.Fatal(err)
	}
	s := New()
	keys, secrets := newCaller(t, "Held")
	setAccounts := func(as ...string) {
		t.Helper()
		if _, err := access.Update("accounts-key", access.Change{Key: keys[0].ID, Accounts: as}); err != nil {
			t.Fatal(err)
		}
	}
	draw := func(secret string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/v1/images/generations", strings.NewReader(`{"model":"codex/gpt-image-2","prompt":"a uni-switch"}`))
		r.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	// the key's first account's plan refuses: the refusal stands, the
	// other account — one the key may not use — is never handed the pen
	setAccounts("codex/me@example.com")
	if w := draw(secrets[0]); w.Code == 200 || strings.Contains(w.Body.String(), "b64_json") {
		t.Fatalf("a refusing account drew: %d %s", w.Code, w.Body.String())
	}
	if !slices.Equal(drew, []string{"acct-1"}) {
		t.Fatalf("the fan-out reached an account the key may not use: %v", drew)
	}
	// the second account, when it is the key's, draws what the first may not
	setAccounts("codex/spare@example.com")
	drew = nil
	if w := draw(secrets[0]); w.Code != 200 || !strings.Contains(w.Body.String(), "b64_json") {
		t.Fatalf("the allowed account didn't draw: %d %s", w.Code, w.Body.String())
	}
	if !slices.Equal(drew, []string{"acct-2"}) {
		t.Fatalf("another account was tried: %v", drew)
	}
	// a keyless request fans out to the next account, as before
	drew = nil
	r := httptest.NewRequest("POST", "/v1/images/generations", strings.NewReader(`{"model":"codex/gpt-image-2","prompt":"a uni-switch"}`))
	r.RemoteAddr = "127.0.0.1:5000"
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !slices.Equal(drew, []string{"acct-1", "acct-2"}) {
		t.Fatalf("a keyless fan-out: %d %s, drew %v", w.Code, w.Body.String(), drew)
	}
}

// A gateway key held to some accounts (#905) counts tokens on those alone:
// the counting candidates — a relay's keys, each an account the key may
// name — are held to the key's accounts as the serving ones are, none but
// the local estimate when it may use none. Gemini's counting, answered by
// the gateway itself, reaches no provider either way.
func TestGatewayKeyAccountsCountTokens(t *testing.T) {
	fresh(t)
	var counted []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counted = append(counted, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"input_tokens":5}`)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Anthropic: up.URL, Key: "sk-0",
		Keys: []provider.KeyAccount{{Key: "sk-A"}, {Key: "sk-B"}}, Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	keys, secrets := newCaller(t, "Held")
	setAccounts := func(as ...string) {
		t.Helper()
		if _, err := access.Update("accounts-key", access.Change{Key: keys[0].ID, Accounts: as}); err != nil {
			t.Fatal(err)
		}
	}
	h := New().Handler()
	count := func(path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+secrets[0])
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	// only the key the key may use counts
	for _, k := range []string{"sk-A", "sk-B"} {
		setAccounts("relay/" + provider.KeyID(k))
		counted = nil
		w := count("/v1/messages/count_tokens", `{"model":"relay/m","messages":[{"role":"user","content":"hi"}]}`)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "input_tokens") {
			t.Fatalf("counted on %s: %d %s", k, w.Code, w.Body.String())
		}
		if !slices.Equal(counted, []string{k}) {
			t.Fatalf("counted on %s by %v", k, counted)
		}
	}
	// the one key it may use, set not to serve the model: the local
	// estimate, no provider asked
	if err := provider.SetAccountModels("relay", provider.KeyID("sk-A"), []string{"other"}); err != nil {
		t.Fatal(err)
	}
	setAccounts("relay/" + provider.KeyID("sk-A"))
	counted = nil
	w := count("/v1/messages/count_tokens", `{"model":"relay/m","messages":[{"role":"user","content":"hi"}]}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "input_tokens") || len(counted) != 0 {
		t.Fatalf("the local estimate: %d %s, asked %v", w.Code, w.Body.String(), counted)
	}
	// Gemini's counting, the model in its URL: the gateway's own estimate
	counted = nil
	w = count("/v1beta/models/relay/m:countTokens", `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "totalTokens") || len(counted) != 0 {
		t.Fatalf("gemini counting: %d %s, asked %v", w.Code, w.Body.String(), counted)
	}
}

// uni-switch's own calls for a held key — a Codex title, of the model Settings
// names — are of the models the user picked, so the models don't hold them,
// but their spend lands on an account: the accounts hold them too (#905),
// and the title is written by an account the key may use alone.
func TestGatewayKeyAccountsHoldASideCall(t *testing.T) {
	var heads []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		heads = append(heads, r.Header.Get("chatgpt-account-id"))
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`data: {"type":"response.completed","response":{"id":"r1","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"title\":\"Fix login bug\"}"}]}],"usage":{"input_tokens":9,"output_tokens":2}}}`))
	}))
	t.Cleanup(up.Close)
	codexSignedIn(t, "spare@example.com")
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })
	if err := provider.Save(provider.Provider{ID: "codex", Models: []string{"gpt-5.5"}}); err != nil {
		t.Fatal(err)
	}
	st := settings.Load()
	st.CodexTitles = "codex/gpt-5.5"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	keys, secrets := newCaller(t, "Held")
	if _, err := access.Update("accounts-key", access.Change{Key: keys[0].ID, Accounts: []string{"codex/spare@example.com"}}); err != nil {
		t.Fatal(err)
	}
	body := `{"model":"gpt-5.6-luna","stream":true,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Generate a concise title. User prompt:\nfix the login bug"}]}],` +
		`"text":{"format":{"type":"json_schema","name":"codex_output_schema","strict":true,"schema":{"type":"object","properties":{"title":{"type":"string"}},"required":["title"],"additionalProperties":false}}}}`
	r := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+secrets[0])
	r.Header.Set("session_id", "title-thread")
	r.Header.Set("x-codex-turn-metadata", `{"thread_source":"thread_title","parent_thread_id":"main-chat"}`)
	w := httptest.NewRecorder()
	New().Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Fix login bug") {
		t.Fatalf("the title: %d %s", w.Code, w.Body.String())
	}
	if !slices.Equal(heads, []string{"acct-2"}) {
		t.Fatalf("the title was written by %v, the key may use acct-2 alone", heads)
	}
}

// System One asks its decision model of an account the calling key may use
// (#905): one of the provider it names and may use serves it; a provider
// the key names no account of, it asks as it always did.
func TestGatewayKeyAccountsSystemOne(t *testing.T) {
	fresh(t)
	var decided int
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decided++
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"answers":{"intent":{"choice":"bug"}},"usage":{"input_tokens":10,"output_tokens":2}}`)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "jev", Name: "Jev", Key: "kj", Decide: up.URL + "/v1", Models: []string{"jev-latest"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "sk-1", Chat: "http://127.0.0.1:1/v1", Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	keys, secrets := newCaller(t, "Held")
	h := New().Handler()
	ask := func(secret string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/v1/systemone", strings.NewReader(`{"model":"jev/jev-latest","state":{"message":"hi"},"questions":{"intent":{"type":"choice"}}}`))
		r.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	// an account of the provider it names: asked
	if _, err := access.Update("accounts-key", access.Change{Key: keys[0].ID, Accounts: []string{"jev/" + provider.KeyID("kj")}}); err != nil {
		t.Fatal(err)
	}
	if w := ask(secrets[0]); w.Code != 200 || decided != 1 {
		t.Fatalf("the key's account: %d %s, decided %d", w.Code, w.Body.String(), decided)
	}
	// a provider the key names no account of, among its models: asked as
	// it always did
	if _, err := access.Update("accounts-key", access.Change{Key: keys[0].ID, Accounts: []string{"relay/" + provider.KeyID("sk-1")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := access.Update("models-key", access.Change{Key: keys[0].ID, Models: []string{"jev/*", "relay/*"}}); err != nil {
		t.Fatal(err)
	}
	if w := ask(secrets[0]); w.Code != 200 || decided != 2 {
		t.Fatalf("an unnamed provider: %d %s, decided %d", w.Code, w.Body.String(), decided)
	}
	// with no model listed, the key may use its accounts' providers' only
	// (Linka on Discord): the decider is not its
	if _, err := access.Update("models-key", access.Change{Key: keys[0].ID}); err != nil {
		t.Fatal(err)
	}
	if w := ask(secrets[0]); w.Code != http.StatusForbidden || decided != 2 {
		t.Fatalf("an account-only key's decider: %d %s, decided %d", w.Code, w.Body.String(), decided)
	}
	// the provider named, its account set not to serve the decision model
	if _, err := access.Update("accounts-key", access.Change{Key: keys[0].ID, Accounts: []string{"jev/" + provider.KeyID("kj")}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetAccountModels("jev", provider.KeyID("kj"), []string{"other"}); err != nil {
		t.Fatal(err)
	}
	w := ask(secrets[0])
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "is not allowed to use the accounts behind") || decided != 2 {
		t.Fatalf("a barred account: %d %s, decided %d", w.Code, w.Body.String(), decided)
	}
}

// A gateway key held to a later key of a decision provider is asked with
// that key alone (#905): the decision was sent on the provider's first
// key whatever the key may use, spending one it may not — here the
// second is the key's, and the first is never asked.
func TestGatewayKeyAccountsSystemOneOnAKey(t *testing.T) {
	fresh(t)
	var asked []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"answers":{"intent":{"choice":"bug"}},"usage":{"input_tokens":10,"output_tokens":2}}`)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "jev", Name: "Jev", Key: "kA", Keys: []provider.KeyAccount{{Key: "kB"}},
		Decide: up.URL + "/v1", Models: []string{"jev-latest"}}); err != nil {
		t.Fatal(err)
	}
	keys, secrets := newCaller(t, "Held")
	if _, err := access.Update("accounts-key", access.Change{Key: keys[0].ID, Accounts: []string{"jev/" + provider.KeyID("kB")}}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/v1/systemone", strings.NewReader(`{"model":"jev/jev-latest","state":{"message":"hi"},"questions":{"intent":{"type":"choice"}}}`))
	r.Header.Set("Authorization", "Bearer "+secrets[0])
	w := httptest.NewRecorder()
	New().Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "bug") {
		t.Fatalf("the decision: %d %s", w.Code, w.Body.String())
	}
	if !slices.Equal(asked, []string{"kB"}) {
		t.Fatalf("asked with %v, the key may use kB alone", asked)
	}
}

// A gateway key held to a later key of a remote uni-switch makes videos with
// that key alone (#905): the videos API sent on the provider's first key,
// refusing a key held to a later one though it may use it.
func TestGatewayKeyAccountsVideoOnAnAllowedKey(t *testing.T) {
	fresh(t)
	var made []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		made = append(made, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"vid-1"}`)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "office", Name: "Office",
		Chat: up.URL + "/v1", Key: "kA", Keys: []provider.KeyAccount{{Key: "kB"}}, Models: []string{"grok-imagine-video"}}); err != nil {
		t.Fatal(err)
	}
	keys, secrets := newCaller(t, "Held")
	if _, err := access.Update("accounts-key", access.Change{Key: keys[0].ID, Accounts: []string{"office/" + provider.KeyID("kB")}}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/v1/videos", strings.NewReader(`{"model":"office/grok-imagine-video","prompt":"a uni-switch"}`))
	r.Header.Set("Authorization", "Bearer "+secrets[0])
	w := httptest.NewRecorder()
	New().Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("the video: %d %s", w.Code, w.Body.String())
	}
	if !slices.Equal(made, []string{"kB"}) {
		t.Fatalf("made with %v, the key may use kB alone", made)
	}
}

// A gateway key held to a later key of a keyed provider draws with that
// key alone (#905): the images API's keyed path refused a key held to a
// later one though it may use it.
func TestGatewayKeyAccountsDrawOnAnAllowedKey(t *testing.T) {
	fresh(t)
	var drew []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		drew = append(drew, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		w.Header().Set("Content-Type", "application/json")
		b64 := base64.StdEncoding.EncodeToString(pngBytes)
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"Here it is.","images":[{"type":"image_url","image_url":{"url":"data:image/png;base64,`+b64+`"}}]}}],"usage":{"prompt_tokens":3,"completion_tokens":50}}`)
			return
		}
		io.WriteString(w, `{"created":1,"data":[{"b64_json":"`+b64+`"}]}`)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "art", Name: "Art", Chat: up.URL + "/v1", Key: "kA",
		Keys: []provider.KeyAccount{{Key: "kB"}}, Models: []string{"gpt-image-1"}}); err != nil {
		t.Fatal(err)
	}
	keys, secrets := newCaller(t, "Held")
	if _, err := access.Update("accounts-key", access.Change{Key: keys[0].ID, Accounts: []string{"art/" + provider.KeyID("kB")}}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/v1/images/generations", strings.NewReader(`{"model":"art/gpt-image-1","prompt":"a uni-switch"}`))
	r.Header.Set("Authorization", "Bearer "+secrets[0])
	w := httptest.NewRecorder()
	New().Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "b64_json") {
		t.Fatalf("the drawing: %d %s", w.Code, w.Body.String())
	}
	if !slices.Equal(drew, []string{"kB"}) {
		t.Fatalf("drew with %v, the key may use kB alone", drew)
	}
}

// A key that lists models and some accounts of one provider uses another
// provider's models it lists on every account (#905): the accounts hold
// it to some accounts of the providers they name, not to those
// providers. (One that lists accounts and no model may use only their
// providers' models: TestGatewayKeyAccountsOnlyHoldTheModels.)
func TestGatewayKeyAccountsOfANamedProvider(t *testing.T) {
	fresh(t)
	plan := &fake{t: t, ctype: "application/json", reply: `{"id":"from-plan","choices":[]}`}
	spare := &fake{t: t, ctype: "application/json", reply: `{"id":"from-spare","choices":[]}`}
	twoProviders(t, plan, spare)
	keys, secrets := newCaller(t, "Held")
	if _, err := access.Update("accounts-key", access.Change{Key: keys[0].ID, Accounts: []string{"plan/" + provider.KeyID("k")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := access.Update("models-key", access.Change{Key: keys[0].ID, Models: []string{"plan/*", "spare/*"}}); err != nil {
		t.Fatal(err)
	}
	h := New().Handler()
	// plan is named: only its key may serve; spare, named nowhere, serves
	// as it always did
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"spare/m2","messages":[{"role":"user","content":"hi"}]}`))
	r.Header.Set("Authorization", "Bearer "+secrets[0])
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "from-spare") {
		t.Fatalf("a provider the key names no account of: %d %s", w.Code, w.Body.String())
	}
	if spare.calls != 1 || plan.calls != 0 {
		t.Fatalf("spare %d, plan %d", spare.calls, plan.calls)
	}
}
