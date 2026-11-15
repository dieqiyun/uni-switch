package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// Each endpoint is tried with a key made for it, and the Anthropic one
// with a model its key sees even when the exposed one is another's.
func TestTestUsesEachEndpointsKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	seen := map[string]string{} // path → key and model it got
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet { // each key sees its own group's models
			if r.Header.Get("Authorization") == "Bearer sk-anthropic" {
				_, _ = rw.Write([]byte(`{"data":[{"id":"claude-opus-5"}]}`))
			} else {
				_, _ = rw.Write([]byte(`{"data":[{"id":"gpt-6-sol"}]}`))
			}
			return
		}
		var in struct{ Model string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		seen[r.URL.Path] = r.Header.Get("Authorization") + " " + in.Model
		_, _ = rw.Write([]byte(`{}`))
	}))
	defer srv.Close()
	p := Provider{ID: "relay", Name: "Relay", Chat: srv.URL + "/v1", Anthropic: srv.URL,
		Key: "sk-chat", KeyProtocol: Chat, Models: []string{"gpt-6-sol"},
		Keys: []KeyAccount{{Key: "sk-anthropic", Protocol: Anthropic}}}
	for _, r := range p.Test(context.Background()) {
		if !r.OK {
			t.Errorf("%s: %+v", r.Protocol, r)
		}
	}
	if got := seen["/v1/chat/completions"]; got != "Bearer sk-chat gpt-6-sol" {
		t.Errorf("chat got %q", got)
	}
	if got := seen["/v1/messages"]; got != "Bearer sk-anthropic claude-opus-5" {
		t.Errorf("anthropic got %q", got)
	}
	ms := p.Available()
	if len(ms) != 2 {
		t.Fatalf("merged list %+v", ms)
	}
	chat, anth := p.first(), p.Keys[0]
	if !p.Serves(chat, "gpt-6-sol") || p.Serves(chat, "claude-opus-5") || !p.Serves(anth, "claude-opus-5") || p.Serves(anth, "gpt-6-sol") {
		t.Error("each key serves only its own list")
	}
	if !p.Serves(anth, "some-unlisted-model") {
		t.Error("a model no list has is anyone's")
	}
}

// Each model is asked on its own, with a key that sees it: one the vendor
// doesn't serve fails while the others answer.
func TestTestModels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	var mu sync.Mutex
	seen := map[string]string{} // model → path and key it came with
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if r.Header.Get("Authorization") == "Bearer sk-anthropic" {
				_, _ = rw.Write([]byte(`{"data":[{"id":"claude-opus-5"}]}`))
			} else {
				_, _ = rw.Write([]byte(`{"data":[{"id":"gpt-6-sol"},{"id":"gone"}]}`))
			}
			return
		}
		var in struct{ Model string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		mu.Lock()
		seen[in.Model] = r.URL.Path + " " + r.Header.Get("Authorization")
		mu.Unlock()
		if in.Model == "gone" {
			rw.WriteHeader(http.StatusNotFound)
			_, _ = rw.Write([]byte(`{"error":{"message":"no channel for model gone"}}`))
			return
		}
		_, _ = rw.Write([]byte(`{}`))
	}))
	defer srv.Close()
	p := Provider{ID: "relay", Name: "Relay", Chat: srv.URL + "/v1", Anthropic: srv.URL,
		Key: "sk-chat", KeyProtocol: Chat,
		Keys: []KeyAccount{{Key: "sk-anthropic", Protocol: Anthropic}}}
	p.Fetch(context.Background())
	rs := p.TestModels(context.Background(), []string{"gpt-6-sol", "claude-opus-5", "gone"})
	if len(rs) != 3 || !rs[0].OK || !rs[1].OK || rs[2].OK || rs[2].Status != 404 || rs[2].Error != "no channel for model gone" {
		t.Fatalf("results %+v", rs)
	}
	if rs[0].Model != "gpt-6-sol" || rs[1].Protocol != Anthropic || rs[2].Model != "gone" {
		t.Fatalf("order %+v", rs)
	}
	if seen["gpt-6-sol"] != "/v1/chat/completions Bearer sk-chat" || seen["claude-opus-5"] != "/v1/messages Bearer sk-anthropic" {
		t.Fatalf("seen %v", seen)
	}
}

// An image model is tested as the gateway draws with it, on the images
// API with one small picture — a chat request it would turn away — while a
// chat model is still asked in chat. The vendor-wide Test picks the chat one.
func TestTestModelsDraws(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	var mu sync.Mutex
	seen := map[string]string{} // model → path it was asked on
	bodies := map[string]map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			rw.WriteHeader(http.StatusNotFound)
			return
		}
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		model, _ := in["model"].(string)
		mu.Lock()
		seen[model] = r.URL.Path
		bodies[model] = in
		mu.Unlock()
		draws := strings.Contains(model, "image") || strings.Contains(model, "flux")
		switch {
		case r.URL.Path == "/v1/images/generations" && draws:
			_, _ = rw.Write([]byte(`{"data":[{"url":"https://img.example/1.png"}]}`))
		case r.URL.Path == "/v1/chat/completions" && !draws:
			_, _ = rw.Write([]byte(`{"choices":[{"message":{"content":"hi"}}]}`))
		default:
			rw.WriteHeader(http.StatusBadRequest)
			_, _ = rw.Write([]byte(`{"error":{"message":"wrong endpoint for ` + model + `"}}`))
		}
	}))
	defer srv.Close()
	p := Provider{ID: "custom", Name: "Custom", Chat: srv.URL + "/v1", Key: "sk-x",
		Models: []string{"gpt-image-1", "flux-kontext-pro", "gpt-6-sol"}}
	rs := p.TestModels(context.Background(), []string{"gpt-image-1", "flux-kontext-pro", "gpt-6-sol"})
	for _, r := range rs {
		if !r.OK {
			t.Fatalf("results %+v", rs)
		}
	}
	if seen["gpt-image-1"] != "/v1/images/generations" || seen["flux-kontext-pro"] != "/v1/images/generations" || seen["gpt-6-sol"] != "/v1/chat/completions" {
		t.Fatalf("seen %v", seen)
	}
	if b := bodies["gpt-image-1"]; b["n"] != float64(1) || b["quality"] != "low" || b["prompt"] == "" {
		t.Fatalf("gpt-image body %v", b)
	}
	if b := bodies["flux-kontext-pro"]; b["quality"] != nil || b["messages"] != nil {
		t.Fatalf("flux body %v", b)
	}
	// the vendor-wide Test asks the chat model, though an image one is first
	mu.Lock()
	clear(seen)
	mu.Unlock()
	if rs := p.Test(context.Background()); len(rs) != 1 || !rs[0].OK || rs[0].Model != "gpt-6-sol" {
		t.Fatalf("Test %+v", rs)
	}
}

// #1837: an image model whose name doesn't place it on the images API
// (agnes-image-2.5-flash reads like gemini's -flash image models, which
// chat) is turned away in chat with the vendor naming the images API; the
// gateway draws with it there, so its test passes there too, and a chat
// model the vendor turns away the same way stays failed.
func TestTestModelsGoesWhereTheVendorPointsIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			rw.WriteHeader(http.StatusNotFound)
			return
		}
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		model, _ := in["model"].(string)
		mu.Lock()
		paths = append(paths, model+" "+r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/v1/images/generations" && model == "agnes-image-2.5-flash" {
			_, _ = rw.Write([]byte(`{"created":1,"data":[{"url":"https://img.example/1.png"}]}`))
			return
		}
		rw.WriteHeader(http.StatusBadRequest)
		_, _ = rw.Write([]byte(`{"error":{"message":"Model ` + model + ` is an image model. Use /v1/images/generations","type":"invalid_request_error"}}`))
	}))
	defer srv.Close()
	p := Provider{ID: "agnes", Name: "Agnes", Chat: srv.URL + "/v1", Key: "sk-x",
		Models: []string{"agnes-image-2.5-flash", "agnes-chat"}}
	rs := p.TestModels(context.Background(), []string{"agnes-image-2.5-flash", "agnes-chat"})
	if len(rs) != 2 || !rs[0].OK || rs[1].OK {
		t.Fatalf("results %+v", rs)
	}
	mu.Lock()
	defer mu.Unlock()
	slices.Sort(paths)
	want := []string{"agnes-chat /v1/chat/completions", "agnes-image-2.5-flash /v1/chat/completions", "agnes-image-2.5-flash /v1/images/generations"}
	if !slices.Equal(paths, want) {
		t.Fatalf("asked %v, want %v", paths, want)
	}
}

// #3766 (char1eslu): AgentRouter serves gpt-5.6-sol on /v1/responses and
// turns it away on /v1/chat/completions, in Chinese. Test models asked
// chat only, so the model stayed red while the gateway answered it on
// Responses. On that word the test now asks the next API the provider has,
// with that API's key, as the gateway would; another refusal stays as it
// was, and so does the word when no other API is left or every API says
// it, the first API's answer then.
func TestTestModelsMovesToTheAPIServingIt(t *testing.T) {
	oneHome(t)
	said := "模型 gpt-5.6-sol 不支持 /v1/chat/completions 接口，请改用 /v1/responses（OpenAI Responses API）。"
	// the report has the message only: the JSON around it is a guess, and
	// so is how it is encoded (escaped as Python's json.dumps writes it)
	refusal := `{"error":{"message":"` + said + `","type":"invalid_request_error"}}`
	escaped := `{"error":{"message":` + strconv.QuoteToASCII(said) + `,"type":"invalid_request_error"}}`
	var mu sync.Mutex
	var asked []string
	chatSays := refusal
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var in struct{ Model string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		mu.Lock()
		asked = append(asked, in.Model+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		chat := chatSays
		mu.Unlock()
		switch {
		case in.Model == "gone":
			// new-api's words for a model no channel serves, written from
			// memory, not from the report
			rw.WriteHeader(http.StatusBadRequest)
			_, _ = rw.Write([]byte(`{"error":{"message":"当前分组 default 下对于模型 gone 无可用渠道"}}`))
		case r.URL.Path == "/v1/chat/completions":
			rw.WriteHeader(http.StatusBadRequest)
			_, _ = rw.Write([]byte(chat))
		case in.Model == "nowhere":
			rw.WriteHeader(http.StatusBadRequest)
			_, _ = rw.Write([]byte(`{"error":{"message":"This model is not supported in the v1/responses endpoint."}}`))
		default:
			_, _ = rw.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	took := func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := asked
		asked = nil
		return out
	}
	p := Provider{ID: "agentrouter", Name: "AgentRouter", Chat: srv.URL + "/v1", Responses: srv.URL + "/v1",
		Key: "sk-chat", KeyProtocol: Chat, Keys: []KeyAccount{{Key: "sk-responses", Protocol: Responses}}}
	for _, chat := range []string{refusal, escaped} {
		mu.Lock()
		chatSays = chat
		mu.Unlock()
		rs := p.TestModels(context.Background(), []string{"gpt-5.6-sol"})
		if len(rs) != 1 || !rs[0].OK || rs[0].Protocol != Responses {
			t.Fatalf("%s: results %+v", chat, rs)
		}
		want := []string{"gpt-5.6-sol /v1/chat/completions Bearer sk-chat", "gpt-5.6-sol /v1/responses Bearer sk-responses"}
		if got := took(); !slices.Equal(got, want) {
			t.Fatalf("%s: asked %v, want %v", chat, got, want)
		}
	}
	mu.Lock()
	chatSays = refusal
	mu.Unlock()
	// a refusal about the model is its answer, on the first API alone
	rs := p.TestModels(context.Background(), []string{"gone"})
	if len(rs) != 1 || rs[0].OK || rs[0].Protocol != Chat || rs[0].Status != 400 {
		t.Fatalf("gone: results %+v", rs)
	}
	if got := took(); len(got) != 1 {
		t.Fatalf("gone: asked %v, want chat once", got)
	}
	// with chat alone the vendor's word is the answer
	chatOnly := Provider{ID: "agentrouter", Name: "AgentRouter", Chat: srv.URL + "/v1", Key: "sk-chat"}
	rs = chatOnly.TestModels(context.Background(), []string{"gpt-5.6-sol"})
	if len(rs) != 1 || rs[0].OK || rs[0].Protocol != Chat || rs[0].Status != 400 || !strings.Contains(rs[0].Error, "请改用 /v1/responses") {
		t.Fatalf("chat only: results %+v", rs)
	}
	if got := took(); len(got) != 1 {
		t.Fatalf("chat only: asked %v, want chat once", got)
	}
	// every API says it: each is asked, and the first one's answer stays
	rs = p.TestModels(context.Background(), []string{"nowhere"})
	if len(rs) != 1 || rs[0].OK || rs[0].Protocol != Chat || !strings.Contains(rs[0].Error, "请改用 /v1/responses") {
		t.Fatalf("nowhere: results %+v", rs)
	}
	if got := took(); len(got) != 2 {
		t.Fatalf("nowhere: asked %v, want chat and responses", got)
	}
}

// The API the user picked for a model, or the only one the vendor's list
// names, keeps its test there, as usable keeps the gateway's request:
// chat turning it away is the answer, not a reason to ask Responses
// (#3766).
func TestTestModelsKeepsAModelOnItsAPI(t *testing.T) {
	oneHome(t)
	const refusal = `{"error":{"message":"模型 gpt-5.6-sol 不支持 /v1/chat/completions 接口，请改用 /v1/responses（OpenAI Responses API）。","type":"invalid_request_error"}}`
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/v1/chat/completions" {
			rw.WriteHeader(http.StatusBadRequest)
			_, _ = rw.Write([]byte(refusal))
			return
		}
		_, _ = rw.Write([]byte(`{}`))
	}))
	defer srv.Close()
	p := Provider{ID: "agentrouter", Name: "AgentRouter", Key: "k", Chat: srv.URL + "/v1", Responses: srv.URL + "/v1", Models: []string{"picked", "listed"}}
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive(p.ID, p.Chat, []catalog.Model{{ID: "picked"}, {ID: "listed", APIs: []string{"chat"}}}); err != nil {
		t.Fatal(err)
	}
	if err := SetModelAPI("agentrouter/picked", "chat"); err != nil {
		t.Fatal(err)
	}
	q, err := Find(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"picked", "listed"} {
		mu.Lock()
		asked = nil
		mu.Unlock()
		rs := q.TestModels(context.Background(), []string{m})
		mu.Lock()
		n := len(asked)
		mu.Unlock()
		if len(rs) != 1 || rs[0].OK || rs[0].Protocol != Chat || n != 1 {
			t.Errorf("%s: results %+v after %d asks, want red on chat after one", m, rs, n)
		}
	}
}

// Asked as Codex, a model the vendor turns away on Responses with its word
// that Chat serves it is asked there with Codex's headers, as the gateway
// sends them on both; asked as Claude Code it stays red on Messages, where
// alone the gateway sends Claude Code's (#3766).
func TestTestModelsAsAClientMovesAsTheGatewayDoes(t *testing.T) {
	oneHome(t)
	SawClient(ClientClaudeCode, http.Header{"User-Agent": {"claude-cli/2.1.0 (external, cli)"}, "X-App": {"cli"}})
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path+" "+r.Header.Get("User-Agent"))
		mu.Unlock()
		switch r.URL.Path {
		case "/v1/responses":
			rw.WriteHeader(http.StatusBadRequest)
			_, _ = rw.Write([]byte(`{"error":{"message":"This model is not supported in the v1/responses endpoint. Use v1/chat/completions."}}`))
		case "/v1/messages":
			rw.WriteHeader(http.StatusBadRequest)
			_, _ = rw.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"Model gpt-x is not supported for format anthropic"}}`))
		default:
			rw.Header().Set("Content-Type", "text/event-stream")
			_, _ = rw.Write([]byte("data: {}\n\n"))
		}
	}))
	defer srv.Close()
	p := Provider{ID: "relay", Name: "Relay", Key: "k", Chat: srv.URL + "/v1", Responses: srv.URL + "/v1", Anthropic: srv.URL}
	took := func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := asked
		asked = nil
		return out
	}
	rs := p.TestModels(TestAs(context.Background(), ClientCodex), []string{"gpt-x"})
	got := took()
	if len(rs) != 1 || !rs[0].OK || rs[0].Protocol != Chat || len(got) != 2 || !strings.HasPrefix(got[0], "/v1/responses ") ||
		!strings.HasPrefix(got[1], "/v1/chat/completions codex_cli_rs/") {
		t.Errorf("as Codex: results %+v, asked %q", rs, got)
	}
	rs = p.TestModels(TestAs(context.Background(), ClientClaudeCode), []string{"gpt-x"})
	got = took()
	if len(rs) != 1 || rs[0].OK || rs[0].Protocol != Anthropic || len(got) != 1 || got[0] != "/v1/messages claude-cli/2.1.0 (external, cli)" {
		t.Errorf("as Claude Code: results %+v, asked %q", rs, got)
	}
}
