package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// fakeMLXServe answers as MLX-Serve (github.com/ddalcu/mlx-serve) does:
// /v1/models lists loaded and discovered models with their capabilities and
// input modalities; decision models have capabilities: ["decisions"];
// /v1/systemone answers System One questions;
// /v1/chat/completions answers chat requests but rejects typed-decision models with 400.
func fakeMLXServe(t *testing.T) (base string, asked func() []string) {
	t.Helper()
	var mu sync.Mutex
	var models []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"object":"list","data":[` +
				`{"id":"clef-flash-4bit","object":"model","created":1791860041,"owned_by":"mlx-serve","loaded":true,"state":"ready","bytes_resident":6193757576,"bytes_on_disk":5950219560,"context_length":4096,"max_model_len":4096,"batched_decode":true,"capabilities":["decisions"],"input_modalities":["text"],"meta":{"architecture":"clef","engine":"mlx","context_length":4096,"model_max_tokens":4096}},` +
				`{"id":"Qwen3.6-35B-A3B-MLX-4bit","object":"model","created":0,"owned_by":"mlx-serve","loaded":false,"state":"unloaded","bytes_resident":0,"bytes_on_disk":20402204271,"context_length":262144,"max_model_len":262144,"capabilities":["chat","tool_use","streaming","json_schema","vision"],"input_modalities":["text","image","video"],"meta":{"architecture":"qwen3_5_moe","engine":"mlx","context_length":262144,"model_max_tokens":262144}},` +
				`{"id":"gemma-4-26B-A4B-it-qat-q4_0-gguf","object":"model","created":0,"owned_by":"mlx-serve","loaded":false,"state":"unloaded","bytes_resident":0,"bytes_on_disk":16000000000,"capabilities":["chat","tool_use","streaming","json_schema"],"input_modalities":["text"],"meta":{"architecture":"gguf","engine":"gguf"}}` +
				`]}`))
		case "/v1/systemone":
			var q struct{ Model string }
			json.NewDecoder(r.Body).Decode(&q)
			mu.Lock()
			models = append(models, q.Model)
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"model":"` + q.Model + `","answers":{"ok":{"type":"noul","noul":0.5322}},"usage":{"input_tokens":146,"output_tokens":0}}`))
		case "/v1/chat/completions":
			var q struct{ Model string }
			json.NewDecoder(r.Body).Decode(&q)
			w.Header().Set("Content-Type", "application/json")
			if q.Model == "clef-flash-4bit" {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":{"message":"This is a typed-decision model; it cannot serve chat/text requests. Use POST /v1/decisions instead.","type":"invalid_request_error","param":null,"code":400}}`))
				return
			}
			w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","model":"` + q.Model + `","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`))
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

// MLX-Serve's decision models: an MLX-Serve provider serves decision models
// (Clef, Clef-Flash, Kev, Laya, D1) at POST /v1/systemone, and its decision
// models are the ones /v1/models marks with capabilities: ["decisions"]. They
// classify routing groups, are never offered as chat models to agents, and
// provider test runs them via decide.
func TestMlxServeDecisionModels(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	base, asked := fakeMLXServe(t)

	// an MLX-Serve provider saved before its preset had System One, at a port
	// of the user's own
	if err := Save(Provider{ID: "mlx-serve", Name: "MLX-Serve", Preset: MLXServePreset,
		Chat: base + "/v1", Responses: base + "/v1", Anthropic: base}); err != nil {
		t.Fatal(err)
	}
	p, err := Find("mlx-serve")
	if err != nil || !p.Decides() || p.DecideOnly() || p.Decide != base+"/v1" {
		t.Fatalf("MLX-Serve has no decision API at its own server: %+v", p)
	}
	if _, err := p.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, _ = Find("mlx-serve")

	if !p.DecidesModel("clef-flash-4bit") || p.DecidesModel("Qwen3.6-35B-A3B-MLX-4bit") || p.DecidesModel("gemma-4-26B-A4B-it-qat-q4_0-gguf") {
		t.Fatal("decision models are not the ones MLX-Serve capabilities say are")
	}
	var deciders []string
	for _, e := range Deciders() {
		deciders = append(deciders, e.ID)
	}
	if !slices.Equal(deciders, []string{"mlx-serve/clef-flash-4bit"}) {
		t.Fatalf("deciders: %v", deciders)
	}
	var agents []string
	for _, e := range providerEntries() {
		agents = append(agents, e.Model)
	}
	if !slices.Equal(agents, []string{"Qwen3.6-35B-A3B-MLX-4bit", "gemma-4-26B-A4B-it-qat-q4_0-gguf"}) {
		t.Fatalf("agents' models: %v", agents)
	}
	for _, m := range p.DecisionModels() {
		if m.ID == "clef-flash-4bit" && (ListedWindow(m) != 4096 || m.ImageInput == nil || *m.ImageInput) {
			t.Fatalf("Clef-Flash: %+v", m)
		}
	}

	q, m, err := RouteDecider("mlx-serve/clef-flash-4bit")
	if err != nil || q.ID != "mlx-serve" || m != "clef-flash-4bit" {
		t.Fatalf("routed to %s %s: %v", q.ID, m, err)
	}
	if err := q.AskSystemOne(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if _, m, _ := RouteDecider("mlx-serve"); m != "clef-flash-4bit" {
		t.Fatalf("MLX-Serve's default decision model: %q", m)
	}
	if got := asked(); !slices.Equal(got, []string{"clef-flash-4bit"}) {
		t.Fatalf("asked: %v", got)
	}

	// provider test mlx-serve clef-flash-4bit tests it via decide
	results := p.TestModels(context.Background(), []string{"clef-flash-4bit"})
	if len(results) != 1 || !results[0].OK || results[0].Protocol != "decide" {
		t.Fatalf("TestModels clef-flash-4bit: %+v", results)
	}

	// chat models are still tested via chat
	chatResults := p.TestModels(context.Background(), []string{"Qwen3.6-35B-A3B-MLX-4bit"})
	if len(chatResults) != 1 || !chatResults[0].OK || chatResults[0].Protocol != Chat {
		t.Fatalf("TestModels chat model: %+v", chatResults)
	}

	// default provider probe tests chat endpoints with chat models, and decide with clef-flash-4bit
	defResults := p.Test(context.Background())
	for _, r := range defResults {
		if r.Protocol != "decide" && r.Model == "clef-flash-4bit" {
			t.Fatalf("default provider test selected decision model for %s: %+v", r.Protocol, r)
		}
	}
}

// Table tests for listedMLXServeDecide using real and edge-case responses.
func TestListedMLXServeDecideTable(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    []catalog.Model
		wantErr bool
	}{
		{
			name: "real captured response with decision and chat models",
			body: `{"object":"list","data":[` +
				`{"id":"clef-flash-4bit","context_length":4096,"max_model_len":4096,"capabilities":["decisions"],"input_modalities":["text"]},` +
				`{"id":"Qwen3.6-35B-A3B-MLX-4bit","context_length":262144,"capabilities":["chat","tool_use"],"input_modalities":["text","image"]}` +
				`]}`,
			want: []catalog.Model{
				{
					ID:              "clef-flash-4bit",
					Name:            "clef-flash-4bit",
					Context:         4096,
					Images:          false,
					InputModalities: []string{"text"},
				},
			},
		},
		{
			name: "decision model with vision input modality",
			body: `{"object":"list","data":[` +
				`{"id":"clef-vl","context_length":65536,"capabilities":["decisions"],"input_modalities":["text","image"]}` +
				`]}`,
			want: []catalog.Model{
				{
					ID:              "clef-vl",
					Name:            "clef-vl",
					Context:         65536,
					Images:          true,
					InputModalities: []string{"text", "image"},
				},
			},
		},
		{
			name: "models field fallback and omitted input modalities",
			body: `{"models":[` +
				`{"id":"clef-base","context_length":4096,"capabilities":["decisions"]}` +
				`]}`,
			want: []catalog.Model{
				{
					ID:      "clef-base",
					Name:    "clef-base",
					Context: 4096,
				},
			},
		},
		{
			name: "server with chat models only",
			body: `{"object":"list","data":[` +
				`{"id":"qwen-chat","capabilities":["chat","tools"],"input_modalities":["text"]}` +
				`]}`,
			want: []catalog.Model{},
		},
		{
			name: "empty models list",
			body: `{"object":"list","data":[]}`,
			want: []catalog.Model{},
		},
		{
			name:    "invalid json",
			body:    `{"detail":"Not Found"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := listedMLXServeDecide([]byte(tt.body))
			if (err != nil) != tt.wantErr {
				t.Fatalf("listedMLXServeDecide() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d models, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i].ID != tt.want[i].ID || got[i].Context != tt.want[i].Context || got[i].Images != tt.want[i].Images {
					t.Errorf("model[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
				if len(tt.want[i].InputModalities) > 0 {
					if got[i].ImageInput == nil || *got[i].ImageInput != tt.want[i].Images {
						t.Errorf("model[%d] ImageInput want %v, got %v", i, tt.want[i].Images, got[i].ImageInput)
					}
				} else if got[i].ImageInput != nil {
					t.Errorf("model[%d] ImageInput want nil, got %v", i, *got[i].ImageInput)
				}
			}
		})
	}
}

// An MLX-Serve server with no decision model has none: no Jev is made up,
// and its chat models stay chat models.
func TestMLXServeWithoutDecisionModels(t *testing.T) {
	ms, err := listedMLXServeDecide([]byte(`{"data":[{"id":"Qwen3-8B-4bit","capabilities":["chat"],"input_modalities":["text"]}]}`))
	if err != nil || len(ms) != 0 {
		t.Fatalf("%v %v", ms, err)
	}
	if _, err := listedMLXServeDecide([]byte(`{"detail":"Not Found"}`)); err == nil {
		t.Fatal("a 404's body read as a list")
	}
	p := Provider{ID: "mlx-serve-x", Preset: MLXServePreset, Chat: "http://127.0.0.1:1/v1", Decide: "http://127.0.0.1:1/v1"}
	if got := p.decideListed(); len(got) != 0 {
		t.Fatalf("made up: %v", got)
	}
	if p.Jev() != "" || p.DecidesModel("Qwen3-8B-4bit") {
		t.Fatalf("Jev: %q", p.Jev())
	}
}
