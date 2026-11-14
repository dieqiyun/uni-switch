package provider

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// selfSignedVendor is an OpenAI-compatible server behind a certificate
// nothing trusts, as fangzhengjin's vLLM on the LAN is (#2383).
func selfSignedVendor(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			io.WriteString(w, `{"id":"c1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
			return
		}
		io.WriteString(w, `{"data":[{"id":"qwen3-32b"}]}`)
	}))
	s.Config.ErrorLog = log.New(io.Discard, "", 0) // the refused handshakes
	s.StartTLS()
	t.Cleanup(s.Close)
	return s
}

func certError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "certificate")
}

// A provider the user added lists its models from a self-signed host only
// when asked not to check its certificate; another provider of the user's
// on the same machine, through the same transport, is still checked.
func TestInsecureTLSOwnProviderOnly(t *testing.T) {
	azureHome(t)
	dialsOut(t)
	srv := selfSignedVendor(t)
	ctx := context.Background()

	if err := Save(Provider{ID: "vllm", Name: "vLLM", Chat: srv.URL + "/v1", Models: []string{"qwen3-32b"}}); err != nil {
		t.Fatal(err)
	}
	p, _ := Find("vllm")
	if _, err := p.List(ctx); !certError(err) {
		t.Fatalf("without InsecureTLS: want a certificate error, got %v", err)
	}
	if rs := p.Test(ctx); len(rs) == 0 || rs[0].OK || !strings.Contains(rs[0].Error, "certificate") {
		t.Fatalf("its test without InsecureTLS: want a certificate error, got %+v", rs)
	}
	p.InsecureTLS = true
	if err := Save(*p); err != nil {
		t.Fatal(err)
	}
	p, _ = Find("vllm")
	if !p.InsecureTLS {
		t.Fatal("InsecureTLS not kept")
	}
	ms, err := p.List(ctx)
	if err != nil || len(ms) != 1 || ms[0].ID != "qwen3-32b" {
		t.Fatalf("with InsecureTLS: %v %v", ms, err)
	}
	if rs := p.Test(ctx); len(rs) == 0 || !rs[0].OK {
		t.Fatalf("its test with InsecureTLS: %+v", rs)
	}

	if err := Save(Provider{ID: "other", Name: "Other", Chat: srv.URL + "/v1", NoModels: true}); err != nil {
		t.Fatal(err)
	}
	o, _ := Find("other")
	if _, err := o.List(ctx); !certError(err) {
		t.Fatalf("another provider on the same host: want a certificate error, got %v", err)
	}
	// its models URL on another host is checked
	p.ModelsURL = strings.Replace(srv.URL, "127.0.0.1", "localhost", 1) + "/v1/models"
	if _, err := p.List(ctx); !certError(err) {
		t.Fatalf("a models URL on another host: want a certificate error, got %v", err)
	}
}

// A preset's provider, a subscription's account and a plugin's never skip
// the check: Save refuses it, and one written into providers.json by hand
// is checked all the same.
func TestInsecureTLSNotForPresetsOrSubscriptions(t *testing.T) {
	azureHome(t)
	dialsOut(t)
	srv := selfSignedVendor(t)
	ctx := context.Background()

	if err := Save(Provider{ID: "ollama", Name: "Ollama", Preset: "ollama", Chat: srv.URL + "/v1", InsecureTLS: true}); err == nil {
		t.Fatal("a preset's provider saved with InsecureTLS")
	}
	if err := Save(Provider{ID: "codex", Name: "Codex", Chat: srv.URL + "/v1", Key: "k", InsecureTLS: true}); err == nil {
		t.Fatal("a subscription's id saved with InsecureTLS")
	}
	for _, p := range []Provider{
		{ID: "ollama", Preset: "ollama", Chat: srv.URL + "/v1", InsecureTLS: true},
		{ID: "codex", Chat: srv.URL + "/v1", InsecureTLS: true},
		{ID: "relay", Chat: srv.URL + "/v1", InsecureTLS: true, Account: &Account{User: "a@b.c"}},
	} {
		if p.TakesInsecureTLS() {
			t.Errorf("%s takes InsecureTLS", p.ID)
		}
		if _, err := p.List(ctx); !certError(err) {
			t.Errorf("%s with InsecureTLS by hand: want a certificate error, got %v", p.ID, err)
		}
	}
}
