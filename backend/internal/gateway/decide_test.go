package gateway

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"

	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

func TestMixedDecisionGateway(t *testing.T) {
	fresh(t)
	var paths []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/chat/completions":
			var q struct{ Model string }
			json.NewDecoder(r.Body).Decode(&q)
			if q.Model != "deepseek-v4.1-flash" {
				http.Error(w, "wrong conversation model", 400)
				return
			}
			io.WriteString(w, `{"id":"chat","choices":[{"index":0,"message":{"role":"assistant","content":"2"},"finish_reason":"stop"}]}`)
		case "/v1/systemone":
			var q struct{ Model string }
			json.NewDecoder(r.Body).Decode(&q)
			if q.Model != "typesafe/jev" {
				http.Error(w, "wrong decision model", 400)
				return
			}
			io.WriteString(w, `{"model":"typesafe/jev","answers":{"levels":{"type":"noul","noul":0},"intent":{"type":"choice","choice":"bug","confidence":0.9}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer up.Close()
	if err := provider.Save(provider.Provider{ID: "mixed", Name: "Mixed", Key: "k", Chat: up.URL + "/v1", Decide: up.URL + "/v1", Models: []string{"deepseek-v4.1-flash", "typesafe/jev"}}); err != nil {
		t.Fatal(err)
	}
	s := New()
	if code, body := postAs(t, s, "", `{"model":"mixed/deepseek-v4.1-flash","messages":[{"role":"user","content":"hello"}]}`); code != 200 || !strings.Contains(body, `"content":"2"`) {
		t.Fatalf("conversation: %d %s", code, body)
	}
	if code, _ := postAs(t, s, "", `{"model":"mixed/typesafe/jev","messages":[{"role":"user","content":"hello"}]}`); code != 400 {
		t.Fatalf("Jev conversation: %d", code)
	}
	for _, model := range []string{"mixed/deepseek-v4.1-flash", "mixed/typesafe/jev"} {
		if v, err := s.askClassifier(model, []string{"feature", "bug"}, before{}, false, "fix this"); err != nil || v.Intent != "bug" {
			t.Fatalf("classify with %s: %+v %v", model, v, err)
		}
	}
	if strings.Join(paths, ",") != "/v1/chat/completions,/v1/chat/completions,/v1/systemone,/v1/systemone" {
		t.Fatalf("upstream endpoints: %v", paths)
	}
}

// jevUp is a System One API: it answers each question it is asked with
// what choice and score say, and keeps what it was asked.
type jevUp struct {
	mu     sync.Mutex
	choice string
	level  string    // its choice without "none of these"; choice when ""
	levels float64   // whether the intents are levels
	work   []float64 // how much work a request at each level is, 0 to 3
	sure   float64
	score  float64
	asked  []map[string]any
	auth   string
}

func (u *jevUp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var q map[string]any
	json.Unmarshal(b, &q)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.asked, u.auth = append(u.asked, q), r.Header.Get("Authorization")
	if r.URL.Path != "/v1/systemone" {
		http.NotFound(w, r)
		return
	}
	answers := map[string]any{}
	qs, _ := q["questions"].(map[string]any)
	if _, ok := qs["intent"]; ok {
		answers["intent"] = map[string]any{"type": "choice", "choice": u.choice, "confidence": u.sure}
	}
	if _, ok := qs["level"]; ok {
		answers["level"] = map[string]any{"type": "choice", "choice": cmp.Or(u.level, u.choice), "confidence": u.sure}
	}
	if _, ok := qs["levels"]; ok {
		answers["levels"] = map[string]any{"type": "noul", "noul": u.levels}
	}
	for i, w := range u.work {
		if _, ok := qs[fmt.Sprintf("work%d", i)]; ok {
			answers[fmt.Sprintf("work%d", i)] = map[string]any{"type": "score", "score": w, "confidence": 0.9}
		}
	}
	if _, ok := qs["effort"]; ok {
		answers["effort"] = map[string]any{"type": "score", "score": u.score, "confidence": 0.8}
	}
	json.NewEncoder(w).Encode(map[string]any{"model": "jev-1.13.0", "answers": answers, "usage": map[string]int{"input_tokens": 120, "output_tokens": 0}})
}

// turns is what it was asked about messages, not about the intents.
func (u *jevUp) turns() []map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	var out []map[string]any
	for _, q := range u.asked {
		if _, ok := q["state"].(map[string]any)["message"]; ok {
			out = append(out, q)
		}
	}
	return out
}

// uni-switch's own /v1/systemone sends the call to the Jev provider named by
// the model prefix, with that prefix taken off the model. /systemone is
// not served.
func TestSystemOneRoutesByPrefix(t *testing.T) {
	setHome(t, t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	type hit struct {
		path, auth, model, state string
	}
	serve := func() (*httptest.Server, *[]hit, *sync.Mutex, *int, *string, *string) {
		var mu sync.Mutex
		var hits []hit
		status := http.StatusOK
		ctype, failBody := "", ""
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			var q struct {
				Model string            `json:"model"`
				State map[string]string `json:"state"`
			}
			json.Unmarshal(b, &q)
			mu.Lock()
			hits = append(hits, hit{r.URL.Path, r.Header.Get("Authorization"), q.Model, q.State["message"]})
			code, ct, fb := status, ctype, failBody
			mu.Unlock()
			if code != http.StatusOK {
				if ct != "" {
					w.Header().Set("Content-Type", ct)
					w.WriteHeader(code)
					io.WriteString(w, fb)
					return
				}
				http.Error(w, `{"detail":{"message":"nope"}}`, code)
				return
			}
			if r.URL.Path != "/v1/systemone" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"model": q.Model, "answers": map[string]any{"intent": map[string]any{"choice": "bug"}}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 2}})
		}))
		return srv, &hits, &mu, &status, &ctype, &failBody
	}
	a, aHits, aMu, aStatus, aCtype, aBody := serve()
	defer a.Close()
	b, bHits, _, _, _, _ := serve()
	defer b.Close()
	for _, p := range []provider.Provider{
		{ID: "load-a", Name: "A", Key: "ka", Decide: a.URL + "/v1"},
		{ID: "load-b", Name: "B", Key: "kb", Decide: b.URL + "/v1"},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	s := New()
	call := func(path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer uni-switch")
		req.Header.Set(SessionHeader, "decision-session")
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	rec := call("/v1/systemone", `{"model":"load-a/jev-latest","state":{"message":"hi"},"questions":{"intent":{"type":"choice"}}}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"choice":"bug"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if n := len(s.trace.routes); n != 1 {
		t.Fatalf("traced %d", n)
	}
	if r := s.trace.routes[0]; r.Session != "decision-session" || len(r.Usage) != 1 || r.Usage[0].Provider != "load-a" || r.Usage[0].Model != "jev-latest" {
		t.Fatalf("decision accounting: %+v", r)
	}
	if recs := usage.Load(time.Time{}); len(recs) != 1 || recs[0].RouteID == 0 || recs[0].RouteID != s.trace.routes[0].ID {
		t.Fatalf("usage route: %+v", recs)
	}
	if r := s.trace.routes[0]; r.Provider != "load-a" || r.Model != "load-a/jev-latest" || !r.Done || r.Status != 200 || len(r.Tries) != 1 || r.Tries[0].Model != "jev-latest" || r.Tries[0].ID != "load-a" {
		t.Fatalf("route %+v", r)
	}
	aMu.Lock()
	if len(*aHits) != 1 || (*aHits)[0].path != "/v1/systemone" || (*aHits)[0].auth != "Bearer ka" || (*aHits)[0].model != "jev-latest" || (*aHits)[0].state != "hi" {
		t.Fatalf("a %+v", *aHits)
	}
	aMu.Unlock()
	if len(*bHits) != 0 {
		t.Fatalf("b %+v", *bHits)
	}
	if err := catalog.SaveLive("load-a", "", []catalog.Model{{ID: "sys1-mini", Name: "Mini"}, {ID: "jev-latest", Name: "Jev"}, {ID: "jev-preview", Name: "Jev (preview)"}}); err != nil {
		t.Fatal(err)
	}
	rec = call("/v1/systemone", `{"model":"load-a/sys1-mini","state":{"message":"mini"},"questions":{}}`)
	if rec.Code != 200 {
		t.Fatalf("sys1-mini %d %s", rec.Code, rec.Body)
	}
	aMu.Lock()
	if got := (*aHits)[len(*aHits)-1]; got.model != "sys1-mini" || got.state != "mini" {
		t.Fatalf("sys1-mini hit %+v", got)
	}
	aMu.Unlock()
	if err := catalog.SaveLive("load-b", "", []catalog.Model{{ID: "typesafe/jev", Name: "Jev"}, {ID: "jev-latest", Name: "Jev"}}); err != nil {
		t.Fatal(err)
	}
	rec = call("/v1/systemone", `{"model":"load-b/typesafe/jev","state":{"message":"there"},"questions":{}}`)
	if rec.Code != 200 {
		t.Fatalf("b route %d %s", rec.Code, rec.Body)
	}
	if got := (*bHits)[0]; got.auth != "Bearer kb" || got.model != "typesafe/jev" || got.state != "there" {
		t.Fatalf("b hit %+v", got)
	}
	if rec := call("/systemone", `{"model":"load-a/jev-latest"}`); rec.Code != 404 || !strings.Contains(rec.Body.String(), "/v1/systemone") {
		t.Fatalf("/systemone %d %s", rec.Code, rec.Body)
	}
	if rec := call("/v1/systemone", `{"model":"load-a/gemini-3.8-flash","state":{},"questions":{}}`); rec.Code != 400 {
		t.Fatalf("gemini %d %s", rec.Code, rec.Body)
	}
	if rec := call("/v1/systemone", `{"model":"nobody/jev-latest"}`); rec.Code != 404 {
		t.Fatalf("nobody %d %s", rec.Code, rec.Body)
	}
	if rec := call("/v1/systemone", `{"model":"load-a/jev-bogus-9"}`); rec.Code != 400 || !strings.Contains(rec.Body.String(), "is not a model of") {
		t.Fatalf("bogus %d %s", rec.Code, rec.Body)
	}
	if rec := call("/v1/systemone", `{"model":"jev-latest"}`); rec.Code != 400 || !strings.Contains(rec.Body.String(), "more than one") {
		t.Fatalf("ambiguous %d %s", rec.Code, rec.Body)
	}
	if n := len(s.trace.routes); n != 3 {
		t.Fatalf("traced %d after misses", n)
	}
	*aStatus = http.StatusForbidden
	if rec := call("/v1/systemone", `{"model":"load-a/jev-preview","questions":{}}`); rec.Code != 403 || !strings.Contains(rec.Body.String(), "nope") {
		t.Fatalf("upstream %d %s", rec.Code, rec.Body)
	}
	if r := s.trace.routes[len(s.trace.routes)-1]; r.Model != "load-a/jev-preview" || r.Status != 403 || r.Tries[0].Fail == "" {
		t.Fatalf("403 route %+v", r)
	}
	*aStatus, *aCtype, *aBody = http.StatusBadGateway, "text/html; charset=utf-8", "<html>bad gateway</html>"
	if rec := call("/v1/systemone", `{"model":"load-a/jev-preview","questions":{}}`); rec.Code != 502 || rec.Header().Get("Content-Type") != "text/html; charset=utf-8" || !strings.Contains(rec.Body.String(), "<html>") {
		t.Fatalf("html 502 %d %s %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
	big := `{"model":"load-a/jev-latest","pad":"` + strings.Repeat("x", maxSystemOneBody) + `"}`
	if rec := call("/v1/systemone", big); rec.Code != http.StatusRequestEntityTooLarge || strings.Contains(rec.Body.String(), "not a System One request") {
		t.Fatalf("413 %d %s", rec.Code, rec.Body)
	}
}

// A Cloudflare DecideURL error is named once, not "CF: CF: …".
func TestSystemOneDecideURLErrorOnce(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"success":false,"errors":[{"code":9109,"message":"Unauthorized"}]}`, 403)
	}))
	defer up.Close()
	p := provider.Provider{ID: "cf", Name: "CF", Key: "k", Decide: up.URL + "/client/v4"}
	_, err := New().systemOne(context.Background(), p, "typesafe/jev", []byte(`{}`))
	if err == nil || strings.Count(err.Error(), "CF:") != 1 {
		t.Fatal(err)
	}
}

func (u *jevUp) n() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.asked)
}

// withEffort changes the effort in each API's own words, only where the
// request asked for reasoning.
func TestWithEffort(t *testing.T) {
	for _, c := range []struct {
		proto      provider.Protocol
		in, effort string
		want       []string
	}{
		{provider.Chat, `{"reasoning_effort":"low"}`, "high", []string{`"reasoning_effort":"high"`}},
		{provider.Chat, `{"model":"m"}`, "high", []string{`{"model":"m"}`}},
		{provider.Responses, `{"reasoning":{"effort":"medium","summary":"auto"}}`, "xhigh", []string{`"effort":"xhigh"`, `"summary":"auto"`}},
		{provider.Responses, `{"reasoning":{"effort":"none"}}`, "high", []string{`"effort":"none"`}},
		// Codex's Responses Lite (gpt-6.1-sol on a ChatGPT account): the
		// backend turns the request away without context all_turns (#534)
		{provider.Responses, `{"reasoning":{"effort":"high","summary":"auto","context":"all_turns"}}`, "medium", []string{`"effort":"medium"`, `"summary":"auto"`, `"context":"all_turns"`}},
		{provider.Anthropic, `{"max_tokens":32000,"thinking":{"type":"adaptive"}}`, "low", []string{`"output_config":{"effort":"low"}`}},
		{provider.Anthropic, `{"max_tokens":32000,"thinking":{"type":"enabled","budget_tokens":4096}}`, "high", []string{`"budget_tokens":24000`}},
		{provider.Anthropic, `{"max_tokens":8000,"thinking":{"type":"enabled","budget_tokens":4096}}`, "xhigh", []string{`"budget_tokens":7999`}},
		{provider.Anthropic, `{"max_tokens":8000,"thinking":{"type":"disabled"}}`, "high", []string{`"type":"disabled"`}},
	} {
		got := string(withEffort(c.proto, []byte(c.in), c.effort))
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s %s at %s: %s, want %s", c.proto, c.in, c.effort, got, w)
			}
		}
	}
}

// A turn at another effort changes nothing of what the vendor caches (#502):
// for Claude thinking adaptively, output_config's effort alone, its thinking
// as it was; for the Responses API, reasoning's effort alone — not the
// instructions, tools, input or prompt_cache_key.
func TestWithEffortKeepsTheCachedPrefix(t *testing.T) {
	for _, c := range []struct {
		proto provider.Protocol
		in    string
		field string
	}{
		{provider.Anthropic, `{"model":"claude-opus-5-5","max_tokens":32000,"thinking":{"type":"adaptive"},"output_config":{"effort":"max"},"system":[{"type":"text","text":"sys","cache_control":{"type":"ephemeral"}}],"tools":[{"name":"read","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":[{"type":"text","text":"a <b> & c","cache_control":{"type":"ephemeral"}}]}]}`, "output_config"},
		{provider.Responses, `{"model":"gpt-6-astra","instructions":"sys","prompt_cache_key":"thread","reasoning":{"effort":"xhigh","summary":"auto"},"tools":[{"type":"function","name":"read"}],"input":[{"role":"user","content":"a <b> & c"}]}`, "reasoning"},
	} {
		lo, hi := withEffort(c.proto, []byte(c.in), "low"), withEffort(c.proto, []byte(c.in), "high")
		var a, b map[string]json.RawMessage
		if json.Unmarshal(lo, &a) != nil || json.Unmarshal(hi, &b) != nil {
			t.Fatalf("%s: %s / %s", c.proto, lo, hi)
		}
		for k := range a {
			if k != c.field && !bytes.Equal(a[k], b[k]) {
				t.Errorf("%s: %s changed with the effort: %s / %s", c.proto, k, a[k], b[k])
			}
		}
		if bytes.Equal(a[c.field], b[c.field]) {
			t.Errorf("%s: effort not changed: %s", c.proto, a[c.field])
		}
	}
}

// Jev is told what it said of the conversation's turn before, so that
// "go on" keeps that turn's kind and reasoning.
func TestJevIsToldTheTurnBefore(t *testing.T) {
	var got struct {
		State     map[string]string `json:"state"`
		Questions map[string]struct {
			Instructions string `json:"instructions"`
		} `json:"questions"`
	}
	intents := []string{"simple task", "complex task"}
	if err := json.Unmarshal(jevBody("jev-latest", intents, nil, before{Intent: "complex task", Effort: "xhigh"}, true, "go on"), &got); err != nil {
		t.Fatal(err)
	}
	if got.State["previous_message_kind"] != "complex task" || got.State["previous_message_reasoning"] != "xhigh" {
		t.Errorf("state = %v", got.State)
	}
	for _, q := range []string{"intent", "effort"} {
		if !strings.Contains(got.Questions[q].Instructions, "carries on") {
			t.Errorf("%s: %q", q, got.Questions[q].Instructions)
		}
	}
	// a first turn has nothing of the kind
	got.State, got.Questions = nil, nil
	if err := json.Unmarshal(jevBody("jev-latest", intents, nil, before{}, true, "hi"), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.State) != 1 || strings.Contains(got.Questions["intent"].Instructions, "carries on") {
		t.Errorf("first turn: %v %q", got.State, got.Questions["intent"].Instructions)
	}
}

func TestJevConfidence(t *testing.T) {
	for _, c := range []struct {
		ps   map[string]any
		want float64
	}{
		{map[string]any{"a": 0.87, "b": 0.13, "c": 0.0}, 0.805},
		{map[string]any{"0": 0.0, "1": 0.96, "2": 0.04}, 0.94},
		{map[string]any{"a": 0.5, "b": 0.5}, 0},
		{map[string]any{"a": 1.0}, 1},
	} {
		if got := provider.Confidence(c.ps); got < c.want-0.001 || got > c.want+0.001 {
			t.Errorf("%v: %v, want %v", c.ps, got, c.want)
		}
	}
}
