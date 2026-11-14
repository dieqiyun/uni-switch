package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
)

// A saved Voyage provider's Test asks an embedding model at its
// /embeddings, as the gateway does, and a reranker's at /rerank, never
// /chat/completions. A custom endpoint may first probe its model list.
func TestVoyageTestAsksItsRetrievalAPIs(t *testing.T) {
	detectHome(t)
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		mu.Lock()
		asked = append(asked, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer pa-voyage" {
			w.WriteHeader(401)
			io.WriteString(w, `{"detail":"Provided API key is invalid."}`)
			return
		}
		switch r.URL.Path {
		case "/v1/embeddings":
			io.WriteString(w, `{"object":"list","data":[{"object":"embedding","embedding":[0.1,0.2],"index":0}],"model":"voyage-4-large","usage":{"total_tokens":1}}`)
		case "/v1/rerank":
			io.WriteString(w, `{"object":"list","data":[{"relevance_score":0.5,"index":0}],"model":"rerank-3","usage":{"total_tokens":2}}`)
		default:
			w.WriteHeader(404)
			io.WriteString(w, `{"detail":"Not Found"}`)
		}
	}))
	defer srv.Close()
	p, _ := legacyVendor("voyage")
	p.Chat, p.Key = srv.URL+"/v1", "pa-voyage"
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	saved, err := Find("voyage")
	if err != nil {
		t.Fatal(err)
	}
	p = *saved
	if rs := p.Test(context.Background()); len(rs) != 1 || !rs[0].OK || rs[0].Model != "voyage-4-large" {
		t.Fatalf("Test: %+v", rs)
	}
	if rs := p.TestModels(context.Background(), []string{"rerank-3", "voyage-code-3"}); len(rs) != 2 || !rs[0].OK || !rs[1].OK {
		t.Fatalf("TestModels: %+v", rs)
	}
	mu.Lock()
	defer mu.Unlock()
	slices.Sort(asked)
	if !slices.Equal(asked, []string{"GET /v1/models", "POST /v1/embeddings", "POST /v1/embeddings", "POST /v1/rerank"}) {
		t.Fatalf("asked %v", asked)
	}
}
