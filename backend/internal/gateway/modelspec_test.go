package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// specUp is a Chat vendor that keeps each body it was sent.
type specUp struct {
	mu   sync.Mutex
	sent []map[string]any
}

func (u *specUp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var v map[string]any
	json.Unmarshal(b, &v)
	u.mu.Lock()
	u.sent = append(u.sent, v)
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"id":"c1","object":"chat.completion","model":"glm-5:free","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`)
}

func (u *specUp) last(t *testing.T) map[string]any {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.sent) == 0 {
		t.Fatal("the vendor was asked nothing")
	}
	return u.sent[len(u.sent)-1]
}

// Dani Ariyanto on Discord: a relay's models came with nothing known —
// no window, no reply limit, no levels. What uni-switch model spec sets is
// what the gateway lists and asks by: /v1/models has the window, the
// reply limit and the levels, a request asking more than the reply limit
// is lowered to it, and one asking a level the model wasn't given is
// asked the nearest it was. Reset, the request goes as the agent asked.
func TestModelSpecReachesTheGateway(t *testing.T) {
	fresh(t)
	up := &specUp{}
	vendor := httptest.NewServer(up)
	t.Cleanup(vendor.Close)
	if err := provider.Save(provider.Provider{ID: "kenari", Name: "Kenari", Key: "k", Chat: vendor.URL + "/v1", Models: []string{"glm-5:free"}}); err != nil {
		t.Fatal(err)
	}
	n := func(v int) *int { return &v }
	s := func(v string) *string { return &v }
	if err := provider.SetModelSpec("kenari/glm-5:free", provider.SpecChange{Context: n(131072), Output: n(16384), Vision: s("off"), Reasoning: s("low-high")}); err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(New().Handler())
	t.Cleanup(gw.Close)

	res, err := http.Get(gw.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Data []struct {
			ID      string   `json:"id"`
			Context int      `json:"context_window"`
			Output  int      `json:"max_output_tokens"`
			Levels  []string `json:"reasoning_levels"`
			Reasons bool     `json:"reasoning"`
		} `json:"data"`
	}
	json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	i := slices.IndexFunc(list.Data, func(m struct {
		ID      string   `json:"id"`
		Context int      `json:"context_window"`
		Output  int      `json:"max_output_tokens"`
		Levels  []string `json:"reasoning_levels"`
		Reasons bool     `json:"reasoning"`
	}) bool {
		return m.ID == "kenari/glm-5:free"
	})
	if i < 0 {
		t.Fatalf("kenari/glm-5:free not in /v1/models: %+v", list.Data)
	}
	if m := list.Data[i]; m.Context != 131072 || m.Output != 16384 || !slices.Equal(m.Levels, []string{"low", "medium", "high"}) || !m.Reasons {
		t.Fatalf("/v1/models: %+v", m)
	}

	ask := func() map[string]any {
		t.Helper()
		res, err := http.Post(gw.URL+"/v1/chat/completions", "application/json",
			strings.NewReader(`{"model":"kenari/glm-5:free","max_tokens":100000,"reasoning_effort":"xhigh","messages":[{"role":"user","content":"hi"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("%d %s", res.StatusCode, b)
		}
		return up.last(t)
	}
	got := ask()
	if got["max_tokens"] != float64(16384) || got["reasoning_effort"] != "high" {
		t.Fatalf("with the spec the vendor was asked max_tokens %v, reasoning_effort %v", got["max_tokens"], got["reasoning_effort"])
	}

	if _, err := provider.ResetModelSpec("kenari/glm-5:free"); err != nil {
		t.Fatal(err)
	}
	got = ask()
	if got["max_tokens"] != float64(100000) || got["reasoning_effort"] != "xhigh" {
		t.Fatalf("reset, the vendor was asked max_tokens %v, reasoning_effort %v", got["max_tokens"], got["reasoning_effort"])
	}
}

// Levels the user keeps of a model's own (uni-switch model efforts, Names &
// levels, uni-switch model spec --reasoning) only narrow what agents are
// offered (#92, 47e12df41): the vendor is still asked at any level it
// has, so an agent's xhigh goes as xhigh. Only a model with no levels
// known is fitted to the ones the user declared
// (TestModelSpecReachesTheGateway).
func TestNarrowedVendorLevelsAskAsBefore(t *testing.T) {
	fresh(t)
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	data := `{"kenari":{"models":{"sol":{"id":"sol","name":"Sol","reasoning_options":[{"type":"effort","values":["low","medium","high","xhigh","max"]}]}}}}`
	if err := os.WriteFile(catalog.CachePath(), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	up := &specUp{}
	vendor := httptest.NewServer(up)
	t.Cleanup(vendor.Close)
	if err := provider.Save(provider.Provider{ID: "kenari", Name: "Kenari", Catalog: "kenari", Key: "k", Chat: vendor.URL + "/v1", Models: []string{"sol"}}); err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(New().Handler())
	t.Cleanup(gw.Close)
	ask := func(effort string) any {
		t.Helper()
		res, err := http.Post(gw.URL+"/v1/chat/completions", "application/json",
			strings.NewReader(`{"model":"kenari/sol","reasoning_effort":"`+effort+`","messages":[{"role":"user","content":"hi"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		return up.last(t)["reasoning_effort"]
	}
	r := "low-high"
	if err := provider.SetModelSpec("kenari/sol", provider.SpecChange{Reasoning: &r}); err != nil {
		t.Fatal(err)
	}
	if e, _ := provider.EntryOf("kenari/sol"); !slices.Equal(e.Efforts, []string{"low", "medium", "high"}) {
		t.Fatalf("the levels offered are the ones kept: %v", e.Efforts)
	}
	for _, effort := range []string{"xhigh", "max", "low"} {
		if got := ask(effort); got != effort {
			t.Errorf("narrowed to low-high, %s went as %v; the vendor has it", effort, got)
		}
	}
}

// AutoVision, which picks the model that describes an image for one that
// can't see, takes the user's word on which see (uni-switch model spec
// --vision): one marked text-only is passed over, one marked seeing is
// picked.
func TestAutoVisionTakesTheUsersVision(t *testing.T) {
	routerUp(t, "text", "anthropic/claude-haiku-5.5")
	if m := AutoVision(); m != "router/anthropic/claude-haiku-5.5" {
		t.Fatalf("AutoVision = %q", m)
	}
	off, on := "off", "on"
	if err := provider.SetModelSpec("router/anthropic/claude-haiku-5.5", provider.SpecChange{Vision: &off}); err != nil {
		t.Fatal(err)
	}
	if m := AutoVision(); m != "" {
		t.Fatalf("AutoVision = %q, a model the user said is text only", m)
	}
	if err := provider.SetModelSpec("router/text", provider.SpecChange{Vision: &on}); err != nil {
		t.Fatal(err)
	}
	if m := AutoVision(); m != "router/text" {
		t.Fatalf("AutoVision = %q, not the model the user said sees", m)
	}
}
