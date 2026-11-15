package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// zenDeprecated is OpenCode Zen's answer to a request for exo-free, as it
// was on 2026-10-10 (HTTP/2 410, content-type: application/json), while
// its /zen/v1/models still listed exo-free (MOMO on Discord).
const zenDeprecated = `{"type":"error","error":{"type":"ModelDeprecated","message":"Model exo-free has been deprecated."},"metadata":{"model":"exo-free"}}`

// zenUnsupported is OpenCode Zen's answer to a request for glm-5-free, a
// free model it had stopped serving, as it was on 2026-10-10 (HTTP 401,
// application/json), with the public key (Jeremy.Zhou on Discord: models
// no longer free should leave the list).
const zenUnsupported = `{"type":"error","error":{"type":"ModelError","message":"Model glm-5-free is not supported"}}`

// zen answers exo-free as OpenCode Zen does, and every other model.
type zen struct{ tried []string }

func (z *zen) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	model := modelOf(body)
	z.tried = append(z.tried, model)
	w.Header().Set("Content-Type", "application/json")
	if model == "glm-5-free" {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, zenUnsupported)
		return
	}
	if model == "exo-free" {
		w.WriteHeader(http.StatusGone)
		io.WriteString(w, zenDeprecated)
		return
	}
	io.WriteString(w, `{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"from zen `+model+`"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`)
}

func listedModels(t *testing.T, s *Server) []string {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models", nil))
	var got struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("/v1/models: %v %s", err, rec.Body.String())
	}
	var ids []string
	for _, m := range got.Data {
		ids = append(ids, m.ID)
	}
	return ids
}

func TestRetiredModelLeavesTheLists(t *testing.T) {
	fresh(t)
	z := &zen{}
	serveOn(t, "opencode-zen-free", "k", []string{"exo-free", "space-bunny-free"}, z)
	s := New()
	if ids := listedModels(t, s); !slices.Contains(ids, "opencode-zen-free/exo-free") {
		t.Fatalf("before: %v", ids)
	}
	code, body := postAs(t, s, "", `{"model":"opencode-zen-free/exo-free","messages":[{"role":"user","content":"hi"}]}`)
	if code != http.StatusGone || !strings.Contains(body, "Model exo-free has been deprecated.") {
		t.Fatalf("the vendor's answer is the agent's: %d %s", code, body)
	}
	if !provider.Retired("opencode-zen-free", "exo-free") {
		t.Fatal("exo-free was not remembered as retired")
	}
	ids := listedModels(t, s)
	if slices.Contains(ids, "opencode-zen-free/exo-free") {
		t.Fatalf("a retired model still listed: %v", ids)
	}
	if !slices.Contains(ids, "opencode-zen-free/space-bunny-free") {
		t.Fatalf("the provider's other models went too: %v", ids)
	}
	// the provider's other models are served as before
	if code, body := postAs(t, s, "", `{"model":"opencode-zen-free/space-bunny-free","messages":[{"role":"user","content":"hi"}]}`); code != http.StatusOK {
		t.Fatalf("space-bunny-free: %d %s", code, body)
	}
	// a provider/model id is still sent: the vendor says what it says, and
	// an answer puts it back
	provider.Unretire("opencode-zen-free", "exo-free")
	if !slices.Contains(listedModels(t, s), "opencode-zen-free/exo-free") {
		t.Fatal("unretired, exo-free is still out of the list")
	}
}

func TestRetiredWords(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
		want   bool
	}{
		{410, zenDeprecated, true},
		{404, `{"error":{"message":"The model text-davinci-003 has been deprecated","type":"invalid_request_error","code":"model_not_found"}}`, true},
		{400, `{"error":{"message":"max_tokens is deprecated, use max_completion_tokens"}}`, false},
		{410, `{"error":{"message":"gone"}}`, false},
		{429, zenDeprecated, false},
		{401, zenUnsupported, true},
		// Zen's own words for a bad key, and for a model served on another API
		{401, `{"type":"error","error":{"type":"AuthError","message":"Invalid API key."}}`, false},
		{401, `{"type":"error","error":{"type":"ModelError","message":"Model grok-4.7 is not supported for format anthropic"}}`, false},
		{403, zenUnsupported, false},
		{401, `Model glm-5-free is not supported`, true},
		// as the gateway holds it, put in its own words
		{401, `{"error":{"code":null,"message":"OpenCode Zen Free: Model glm-5-free is not supported","param":null,"type":"authentication_error"}}`, true},
		{401, `Model grok-4.7 is not supported for format anthropic`, false},
		{401, `Invalid API key.`, false},
		// the safety filter's refusal, whatever its message says of the
		// model, is about the request (#3274)
		{400, `{"error":{"type":"invalid_request_error","code":"cyber_policy","message":"This request was flagged by our safety filter. The model m1 is retired.","param":null}}`, false},
		{400, `{"error":{"code":"content_filter","message":"The model m1 has been deprecated."}}`, false},
		{400, `{"error":{"type":"invalid_request_error","code":"model_deprecated","message":"The model m1 is retired."}}`, true},
	} {
		if got := modelRetired(c.status, []byte(c.body)); got != c.want {
			t.Errorf("modelRetired(%d, %s) = %v", c.status, c.body, got)
		}
	}
}

// A free model Zen serves no more is answered 401 "Model … is not
// supported": that model leaves the provider's list and alone sits out,
// and the account's other free models are served on the next request,
// rather than the whole account resting as though its key were refused.
func TestZenUnsupportedModelLeavesTheListAlone(t *testing.T) {
	fresh(t)
	z := &zen{}
	serveOn(t, "opencode-zen-free", "k", []string{"glm-5-free", "space-bunny-free"}, z)
	s := New()
	code, body := postAs(t, s, "", `{"model":"opencode-zen-free/glm-5-free","messages":[{"role":"user","content":"hi"}]}`)
	if code != http.StatusUnauthorized || !strings.Contains(body, "Model glm-5-free is not supported") {
		t.Fatalf("the vendor's answer is the agent's: %d %s", code, body)
	}
	if !provider.Retired("opencode-zen-free", "glm-5-free") {
		t.Fatalf("glm-5-free was not remembered as gone: %s", body)
	}
	ids := listedModels(t, s)
	if slices.Contains(ids, "opencode-zen-free/glm-5-free") || !slices.Contains(ids, "opencode-zen-free/space-bunny-free") {
		t.Fatalf("lists: %v", ids)
	}
	if code, body := postAs(t, s, "", `{"model":"opencode-zen-free/space-bunny-free","messages":[{"role":"user","content":"hi"}]}`); code != http.StatusOK {
		t.Fatalf("the account's other model sat out with it: %d %s", code, body)
	}
}

// A safety filter's refusal whose message also says the model is retired
// is a refusal of the request: the model stays in the provider's lists
// (#3274, CartethyiaQwQ). A retirement said without one still retires.
func TestRefusalSayingRetiredRetiresNothing(t *testing.T) {
	for _, x := range []struct {
		name, body string
		retire     bool
	}{
		{"refusal", `{"error":{"type":"invalid_request_error","code":"cyber_policy","message":"This content was flagged for possible cybersecurity risk. The model m1 is retired.","param":null}}`, false},
		{"retired alone", `{"error":{"type":"invalid_request_error","code":"model_deprecated","message":"The model m1 is retired."}}`, true},
	} {
		t.Run(x.name, func(t *testing.T) {
			fresh(t)
			t.Cleanup(func() { provider.Unretire("a", "m1") })
			serveOn(t, "a", "k", []string{"m1"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				io.WriteString(w, x.body)
			}))
			code, body := postAs(t, New(), "", `{"model":"a/m1","messages":[{"role":"user","content":"hi"}]}`)
			if got := provider.Retired("a", "m1"); got != x.retire {
				t.Fatalf("retired %v, want %v (%d %s)", got, x.retire, code, body)
			}
		})
	}
}
