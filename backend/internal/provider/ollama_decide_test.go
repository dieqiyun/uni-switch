package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
)

// The bytes below were saved from Ollama 0.40.6 (a local server with
// tev1:0.8b and smollm:135m pulled) and from ollama.com/api/tags on
// 2026-10-19, trimmed to the rows a test needs.
const (
	ollamaLocalTags   = `{"models":[{"name":"smollm:135m","model":"smollm:135m","modified_at":"2026-10-19T23:15:35.885392239+08:00","size":91739413,"digest":"b0b2a46174385c0adcaa77ff245ffeced5fc4a61447b6f221b2beb5c5a760133","details":{"parent_model":"","format":"gguf","family":"llama","families":["llama"],"parameter_size":"134.52M","quantization_level":"Q4_0","context_length":2048,"embedding_length":576,"runner":"ggml"},"capabilities":["completion"]},{"name":"tev1:0.8b","model":"tev1:0.8b","modified_at":"2026-10-19T23:14:31.147707454+08:00","size":1524372929,"digest":"b661d56508169d2b6c4cab5a047409f50bd9694a215bc232469d44291f4bdbf1","details":{"parent_model":"","format":"safetensors","family":"qwen3_5","families":["qwen3_5"],"parameter_size":"752M","quantization_level":"BF16","runner":"mlx"},"capabilities":["decision"]}]}`
	ollamaLocalModels = `{"object":"list","data":[{"id":"smollm:135m","object":"model","created":1792422935,"owned_by":"library"},{"id":"tev1:0.8b","object":"model","created":1792422871,"owned_by":"library"}]}`
	ollamaSystemOne   = `{"model":"tev1:0.8b","answers":{"ok":{"type":"noul","noul":0.2234409293546222}},"usage":{"input_tokens":107,"output_tokens":0},"prompt_eval_cached_count":0}`
	ollamaNoChat      = `{"error":{"message":"\"tev1:0.8b\" does not support chat","type":"invalid_request_error","param":null,"code":null}}`
	ollamaCloudTags   = `{"models":[{"name":"glm-5.3","model":"glm-5.3","modified_at":"2026-08-28T08:00:00-07:00","size":755433728000,"digest":"632dfda18c6d","details":{"parent_model":"","format":"","family":"","families":null,"parameter_size":"","quantization_level":""},"capabilities":["completion","thinking","tools"]},{"name":"clef:27b","model":"clef:27b","modified_at":"2026-10-04T09:00:00-07:00","size":54969731792,"digest":"490a326b58a0","details":{"parent_model":"","format":"","family":"","families":null,"parameter_size":"","quantization_level":""},"capabilities":["decision","vision"]}]}`
	// Ollama before System One (0.6.8) lists no capabilities
	ollamaOldTags = `{"models":[{"name":"qwen3:8b","model":"qwen3:8b","modified_at":"2025-05-01T10:00:00+08:00","size":5225388164,"digest":"500a1f067a9f","details":{"parent_model":"","format":"gguf","family":"qwen3","families":["qwen3"],"parameter_size":"8.2B","quantization_level":"Q4_K_M"}}]}`
)

// fakeOllama answers as Ollama 0.40.6 does: /api/tags and /v1/models list
// a chat model and a decision model, /v1/systemone answers System One, and
// chat completions turn the decision model away.
func fakeOllama(t *testing.T) (base string, asked func() []string) {
	t.Helper()
	var mu sync.Mutex
	var models []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(ollamaLocalTags))
		case "/v1/models":
			w.Write([]byte(ollamaLocalModels))
		case "/v1/systemone":
			var q struct{ Model string }
			json.NewDecoder(r.Body).Decode(&q)
			mu.Lock()
			models = append(models, q.Model)
			mu.Unlock()
			w.Write([]byte(ollamaSystemOne))
		case "/v1/chat/completions":
			var q struct{ Model string }
			json.NewDecoder(r.Body).Decode(&q)
			if q.Model == "tev1:0.8b" {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(ollamaNoChat))
				return
			}
			w.Write([]byte(`{"id":"chatcmpl-382","object":"chat.completion","created":1792422957,"model":"smollm:135m","system_fingerprint":"fp_ollama","choices":[{"index":0,"message":{"role":"assistant","content":"Hello! How can I"},"finish_reason":"length"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(models)
	}
}

// An Ollama provider saved before its preset had System One (Nanka on
// Discord): the models its /api/tags marks "decision" classify routing
// groups through /v1/systemone, and agents no longer see them as chat
// models.
func TestOllamaDecisionModels(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	base, asked := fakeOllama(t)

	if err := Save(Provider{ID: "ollama", Name: "Ollama", Preset: "ollama", Chat: base + "/v1", Anthropic: base}); err != nil {
		t.Fatal(err)
	}
	p, err := Find("ollama")
	if err != nil || !p.Decides() || p.DecideOnly() || p.Decide != base+"/v1" {
		t.Fatalf("Ollama has no decision API at its own server: %+v", p)
	}
	if _, _, _, err := p.refetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, _ = Find("ollama")

	if !p.DecidesModel("tev1:0.8b") || p.DecidesModel("smollm:135m") {
		t.Fatal("decision models are not the ones Ollama's capabilities say are")
	}
	var deciders []string
	for _, e := range Deciders() {
		deciders = append(deciders, e.ID)
	}
	if !slices.Equal(deciders, []string{"ollama/tev1:0.8b"}) {
		t.Fatalf("deciders: %v", deciders)
	}
	var agents []string
	for _, e := range providerEntries() {
		agents = append(agents, e.Model)
	}
	if !slices.Equal(agents, []string{"smollm:135m"}) {
		t.Fatalf("agents' models: %v", agents)
	}

	q, m, err := RouteDecider("ollama/tev1:0.8b")
	if err != nil || q.ID != "ollama" || m != "tev1:0.8b" {
		t.Fatalf("routed to %s %s: %v", q.ID, m, err)
	}
	if err := q.AskSystemOne(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if _, m, _ := RouteDecider("ollama"); m != "tev1:0.8b" {
		t.Fatalf("Ollama's default decision model: %q", m)
	}
	if got := asked(); !slices.Equal(got, []string{"tev1:0.8b"}) {
		t.Fatalf("asked: %v", got)
	}
	results := p.TestModels(context.Background(), []string{"tev1:0.8b"})
	if len(results) != 1 || !results[0].OK || results[0].Protocol != "decide" {
		t.Fatalf("TestModels tev1:0.8b: %+v", results)
	}
}

// Ollama Cloud's list marks Clef a decision model that reads images, with
// the cloud's window; a chat model, and every model of an Ollama from
// before System One, is none.
func TestListedOllamaDecide(t *testing.T) {
	ms, err := listedOllamaDecide("https://ollama.com/v1", []byte(ollamaCloudTags))
	if err != nil || len(ms) != 1 || ms[0].ID != "clef:27b" || ms[0].Context != 65_536 || ms[0].ImageInput == nil || !*ms[0].ImageInput {
		t.Fatalf("cloud: %+v %v", ms, err)
	}
	ms, err = listedOllamaDecide("http://localhost:11434/v1", []byte(ollamaLocalTags))
	if err != nil || len(ms) != 1 || ms[0].ID != "tev1:0.8b" || ms[0].Context != 0 || ms[0].ImageInput == nil || *ms[0].ImageInput {
		t.Fatalf("local: %+v %v", ms, err)
	}
	if ms, err := listedOllamaDecide("http://localhost:11434/v1", []byte(ollamaOldTags)); err != nil || len(ms) != 0 {
		t.Fatalf("old Ollama: %+v %v", ms, err)
	}
	if _, err := listedOllamaDecide("http://localhost:11434/v1", []byte(`404 page not found`)); err == nil {
		t.Fatal("a 404's body read as a list")
	}
	if got := ollamaDecisions("https://ollama.com/v1"); got != "https://ollama.com/api/tags" {
		t.Fatalf("list at %s", got)
	}
	for _, id := range []string{"ollama"} {
		pr := Preset(id)
		if pr == nil || pr.Decide == "" || pr.Decide != pr.Chat {
			t.Fatalf("%s's preset has no System One beside its chat: %+v", id, pr)
		}
	}
	p := Provider{ID: "ollama-x", Preset: "ollama-cloud", Chat: "http://127.0.0.1:1/v1", Decide: "http://127.0.0.1:1/v1"}
	if got := p.decideListed(); len(got) != 0 || p.Jev() != "" {
		t.Fatalf("made up: %v %q", got, p.Jev())
	}
}
