package provider

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"
)

// OpenRouter's lists as it served them on 2026-11-01, cut to the models
// the test needs: inception/mercury-decide is in the decisions list alone
// (output_modalities decisions, endpoints /api/alpha/decisions), and the
// chat list has free chat models named with the same :free variant.
const (
	orDecisionsList = `{"data":[{"id":"inception/mercury-decide","canonical_slug":"inception/mercury-decide-20260930","name":"Inception: Mercury Decide","context_length":65536,"architecture":{"modality":"text+image->decisions","input_modalities":["text","image"],"output_modalities":["decisions"],"tokenizer":"Other","instruct_type":null},"pricing":{"prompt":"0.00000002","completion":"0","input_cache_read":"0"},"supported_parameters":[],"endpoints":["/api/alpha/decisions","/api/alpha/extractions"]}]}`
	orChatList      = `{"data":[{"id":"liquid/lfm-2.5-2.6b:free","name":"LiquidAI: LFM2.5 2.6B (free)","context_length":32768,"architecture":{"modality":"text->text","input_modalities":["text"],"output_modalities":["text"]},"endpoints":["/api/v1/chat/completions","/api/v1/completions","/api/v1/messages","/api/v1/responses"]},{"id":"openai/gpt-5","context_length":400000,"architecture":{"modality":"text+image->text","input_modalities":["text","image"],"output_modalities":["text"]},"endpoints":["/api/v1/chat/completions","/api/v1/completions","/api/v1/messages","/api/v1/responses"]}]}`
	// /api/v1/systemone's answer to a choice, as Mercury Decide gave it
	orDecideAnswer = `{"model":"inception/mercury-decide-20260930","answers":{"ok":{"type":"noul","noul":0.9}},"usage":{"input_tokens":72,"output_tokens":1,"cost":0.00000144},"id":"gen-dec-1793467694-KCls8tvRNbusIJmB9FhD","provider":"Inception"}`
)

// fakeOpenRouterLists serves orChatList and orDecisionsList for
// openrouter.ai, and an empty image model list (/images/models), and
// records the path and model of every other request.
func fakeOpenRouterLists(t *testing.T) (asked func() []string) {
	t.Helper()
	var mu sync.Mutex
	var got []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/models" {
			if r.URL.Query().Get("output_modalities") == "decisions" {
				w.Write([]byte(orDecisionsList))
			} else {
				w.Write([]byte(orChatList))
			}
			return
		}
		if r.URL.Path == "/api/v1/images/models" {
			w.Write([]byte(`{"data":[]}`))
			return
		}
		var q struct{ Model string }
		json.NewDecoder(r.Body).Decode(&q)
		mu.Lock()
		got = append(got, r.URL.Path+" "+q.Model)
		mu.Unlock()
		if r.URL.Path == "/api/v1/systemone" {
			w.Write([]byte(orDecideAnswer))
			return
		}
		w.WriteHeader(400)
		w.Write([]byte(`{"error":{"message":"` + q.Model + ` is a decisions model and cannot be used with the chat/completions endpoint. Use the /api/alpha/decisions endpoint instead.","code":400}}`))
	}))
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().String()
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, addr)
		},
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = tr
	t.Cleanup(func() { http.DefaultClient.Transport = old; tr.CloseIdleConnections() })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(got)
	}
}
