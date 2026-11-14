package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// reasoningLeft says what of a sent body still asks the model to reason.
func reasoningLeft(sent map[string]any) string {
	for _, k := range []string{"reasoning_effort", "reasoning", "thinking", "enable_thinking", "output_config"} {
		if sent[k] != nil {
			return k
		}
	}
	return ""
}

// anthUp is an Anthropic vendor that keeps what it was sent.
type anthUp struct {
	mu   sync.Mutex
	last string
}

func (u *anthUp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	u.mu.Lock()
	u.last = string(b)
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","model":"glm","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":1}}`)
}

// Relayed to an Anthropic vendor as it came, a reasonless model is sent
// neither the agent's thinking nor the thinking:{type:disabled} uni-switch
// adds for a model that isn't Claude: that is a reasoning field too.
func TestReasonlessAnthropicRelay(t *testing.T) {
	fresh(t)
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	up := &anthUp{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	if err := provider.Save(provider.Provider{ID: "anth", Name: "Anth", Key: "k", Models: []string{"glm"}, Anthropic: srv.URL}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelReasonless("anth/glm", true); err != nil {
		t.Fatal(err)
	}
	s := New()
	for _, body := range []string{
		`{"model":"anth/glm","max_tokens":2000,"thinking":{"type":"enabled","budget_tokens":1024},"output_config":{"effort":"high"},"messages":[{"role":"user","content":"hi"}]}`,
		`{"model":"anth/glm","max_tokens":2000,"messages":[{"role":"user","content":"hi"}]}`,
	} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		up.mu.Lock()
		var sent map[string]any
		json.Unmarshal([]byte(up.last), &sent)
		up.mu.Unlock()
		if k := reasoningLeft(sent); k != "" {
			t.Fatalf("%s: sent %s: %v", body, k, sent)
		}
	}
}
