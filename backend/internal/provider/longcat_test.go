package provider

import (
	"context"
	"net/http"
	"net/http/httptest"

	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// Its list, /openai/v1/models with the key as a Bearer, as the docs'
// example answers it.
func TestLongCatList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openai/v1/models" || r.Header.Get("Authorization") != "Bearer k" {
			rw.WriteHeader(http.StatusUnauthorized)
			rw.Write([]byte(`{"error":{"code":"invalid_api_key","message":"missing_api_key","type":"authentication_error"}}`))
			return
		}
		rw.Write([]byte(`{ "object" : "list" , "data" : [ { "id" : "LongCat-2.5" , "object" : "model" , "owned_by" : "LongCat" } ] }`))
	}))
	defer srv.Close()
	ms, _, err := catalog.FetchAt(context.Background(), srv.URL+"/openai/v1", "k", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].ID != "LongCat-2.5" {
		t.Fatalf("%+v", ms)
	}
}
