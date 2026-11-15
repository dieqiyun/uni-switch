package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/tidwall/gjson"
)

// thinkUp is a vendor answering the routing classifier "1" on Chat,
// Anthropic's Messages and Gemini's API, keeping each body it was sent;
// refuse, when it returns a message, answers 400 with it.
type thinkUp struct {
	mu     sync.Mutex
	bodies []string
	refuse func(body string) string
}

func (u *thinkUp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	u.mu.Lock()
	u.bodies = append(u.bodies, string(b))
	refuse := u.refuse
	u.mu.Unlock()
	if refuse != nil {
		if msg := refuse(string(b)); msg != "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			io.WriteString(w, msg)
			return
		}
	}
	stream := gjson.GetBytes(b, "stream").Bool()
	switch p := r.URL.Path; {
	case strings.HasSuffix(p, "/messages") && stream:
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"m\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"+
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"+
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"1\"}}\n\n"+
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"+
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n"+
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	case strings.HasSuffix(p, "/messages"):
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"1"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	case strings.Contains(p, ":streamGenerateContent"):
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"1"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`))
	case strings.Contains(p, ":generateContent"):
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"1"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`)
	case stream:
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`data: {"id":"c","choices":[{"index":0,"delta":{"role":"assistant","content":"1"}}]}`,
			`data: {"id":"c","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`, `data: [DONE]`))
	default:
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"1"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1}}`)
	}
}

func (u *thinkUp) last() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.bodies) == 0 {
		return ""
	}
	return u.bodies[len(u.bodies)-1]
}

func (u *thinkUp) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.bodies)
}

// thinkVendor saves p, served by a new thinkUp, with the levels given
// for model on its list (none when levels is nil).
func thinkVendor(t *testing.T, p provider.Provider, model string, levels []string) *thinkUp {
	t.Helper()
	u := &thinkUp{}
	srv := httptest.NewServer(u)
	t.Cleanup(srv.Close)
	base := srv.URL
	switch {
	case p.Anthropic == "x":
		p.Anthropic = base
	case p.Gemini == "x":
		p.Gemini, base = base+"/v1beta", base+"/v1beta"
	default:
		p.Chat, base = base+"/v1", base+"/v1"
	}
	p.Name, p.Key, p.Models = strings.ToUpper(p.ID), "k", []string{model}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive(p.ID, base, []catalog.Model{{ID: model, Context: 32000, Efforts: levels}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := provider.Find(p.ID); len(got.Efforts(model)) != len(levels) {
		t.Fatalf("%s levels %v, want %v", model, got.Efforts(model), levels)
	}
	return u
}

// classifyOnce asks s's routing classifier, model, which of two kinds a
// message is, and fails unless it answers the first.
func classifyOnce(t *testing.T, s *Server, model string) {
	t.Helper()
	v, err := s.askClassifier(model, []string{"refactoring", "tests"}, before{}, false, "rename this package")
	if err != nil || v.Intent != "refactoring" {
		t.Fatalf("%s: %+v %v", model, v, err)
	}
}

// tangtang969 on X: "classifier的模型调用可以加上不带思考的参数么？提速效果
// 比较好". The routing classifier asks its model to reason not at all
// where its wire can say so: Anthropic's API thinking disabled where the
// model's lowest level asked it to think at low, Gemini 2.5 Flash its off
// budget, 0, where it went at low's 4096, and a Chat model whose levels
// aren't known effort none, where it was left to think at its vendor's
// default.
func TestClassifierAsksThinkingOff(t *testing.T) {
	for _, c := range []struct {
		name   string
		p      provider.Provider
		model  string
		levels []string
		check  func(body string) bool
	}{
		{"anthropic", provider.Provider{ID: "an", Anthropic: "x"}, "claude-sonnet-4-5", []string{"low", "medium", "high"},
			func(b string) bool {
				return gjson.Get(b, "thinking").Raw == `{"type":"disabled"}` && !gjson.Get(b, "output_config").Exists()
			}},
		{"gemini 2.5 flash", provider.Provider{ID: "gm", Gemini: "x"}, "gemini-2.5-flash", []string{"low", "medium", "high"},
			func(b string) bool {
				return gjson.Get(b, "generationConfig.thinkingConfig").Raw == `{"thinkingBudget":0}`
			}},
		{"chat, levels unknown", provider.Provider{ID: "rl"}, "gpt6.1-sol", nil,
			func(b string) bool { return gjson.Get(b, "reasoning_effort").String() == "none" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			fresh(t)
			u := thinkVendor(t, c.p, c.model, c.levels)
			classifyOnce(t, New(), c.p.ID+"/"+c.model)
			if !c.check(u.last()) {
				t.Fatalf("sent %s", u.last())
			}
		})
	}
}

// What each vendor takes is unchanged: a Chat model with levels and no
// none is asked its lowest, one with none none, a model the user said
// takes no reasoning ask is sent none at all, and Gemini 2.5 Pro, which
// can't stop thinking, its least budget. A relay that turns the effort
// away still classifies, asked again without it.
func TestClassifierThinkingAsEachVendorTakes(t *testing.T) {
	for _, c := range []struct {
		name       string
		p          provider.Provider
		model      string
		levels     []string
		reasonless bool
		check      func(body string) bool
	}{
		{"chat lowest", provider.Provider{ID: "lo"}, "cls", []string{"low", "high", "max"}, false,
			func(b string) bool { return gjson.Get(b, "reasoning_effort").String() == "low" }},
		{"chat minimal", provider.Provider{ID: "mi"}, "cls", []string{"minimal", "medium"}, false,
			func(b string) bool { return gjson.Get(b, "reasoning_effort").String() == "minimal" }},
		{"chat none", provider.Provider{ID: "no"}, "cls", []string{"none", "low"}, false,
			func(b string) bool { return gjson.Get(b, "reasoning_effort").String() == "none" }},
		{"reasonless", provider.Provider{ID: "rs"}, "cls", nil, true,
			func(b string) bool {
				return !gjson.Get(b, "reasoning_effort").Exists() && !gjson.Get(b, "reasoning").Exists()
			}},
		{"gemini 2.5 pro", provider.Provider{ID: "gp", Gemini: "x"}, "gemini-2.5-pro", []string{"low", "medium", "high"}, false,
			func(b string) bool {
				return gjson.Get(b, "generationConfig.thinkingConfig").Raw == `{"thinkingBudget":128}`
			}},
	} {
		t.Run(c.name, func(t *testing.T) {
			fresh(t)
			u := thinkVendor(t, c.p, c.model, c.levels)
			if c.reasonless {
				if err := provider.SetModelReasonless(c.p.ID+"/"+c.model, true); err != nil {
					t.Fatal(err)
				}
			}
			classifyOnce(t, New(), c.p.ID+"/"+c.model)
			if !c.check(u.last()) {
				t.Fatalf("sent %s", u.last())
			}
		})
	}

	t.Run("effort turned away", func(t *testing.T) {
		fresh(t)
		u := thinkVendor(t, provider.Provider{ID: "rf"}, "gpt6.1-sol", nil)
		u.refuse = func(b string) string {
			if gjson.Get(b, "reasoning_effort").Exists() {
				return `{"error":{"message":"Unrecognized request argument supplied: reasoning_effort","type":"invalid_request_error"}}`
			}
			return ""
		}
		s := New()
		for i := range 2 {
			n := u.count()
			classifyOnce(t, s, "rf/gpt6.1-sol")
			if gjson.Get(u.last(), "reasoning_effort").Exists() {
				t.Fatalf("ask %d answered with %s", i, u.last())
			}
			if i == 1 && u.count()-n != 1 {
				t.Errorf("the next ask took %d requests, want 1", u.count()-n)
			}
		}
	})
}

// The Image recognition model's own level is left as it was: the least
// of its levels, none asked of one whose levels aren't known.
func TestVisionEffortUnchangedByClassifier(t *testing.T) {
	fresh(t)
	thinkVendor(t, provider.Provider{ID: "vu"}, "seer", nil)
	thinkVendor(t, provider.Provider{ID: "vl"}, "seer", []string{"low", "high"})
	if e := visionEffort("vu/seer"); e != "" {
		t.Errorf("unknown levels: %q", e)
	}
	if e := visionEffort("vl/seer"); e != "low" {
		t.Errorf("low, high: %q", e)
	}
}
