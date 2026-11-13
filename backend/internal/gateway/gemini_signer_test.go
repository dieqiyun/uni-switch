package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// studioCorrupted is how Gemini's OpenAI-compatible endpoint turns away a
// thought signature it didn't give (mimi001234, 2026-11-05).
const studioCorrupted = `[{"error":{"code":400,"message":"Corrupted thought signature.","status":"INVALID_ARGUMENT"}}]`

// studioSig is the signature studioUp gives its own calls.
const studioSig = "c3R1ZGlvLW1pbnRlZC1zaWduYXR1cmU="

// studioUp is Gemini on Google AI Studio's OpenAI-compatible chat
// completions: a call's signature has to be one it gave, or the value
// Google documents for calls it didn't make.
type studioUp struct {
	mu    sync.Mutex
	sent  [][]string // per request, the signature on each call, in order
	reply string
}

func (u *studioUp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var req struct {
		Stream   bool `json:"stream"`
		Messages []struct {
			ToolCalls []struct {
				Extra json.RawMessage `json:"extra_content"`
			} `json:"tool_calls"`
		} `json:"messages"`
	}
	json.Unmarshal(b, &req)
	var sigs []string
	refused := false
	for _, m := range req.Messages {
		for _, c := range m.ToolCalls {
			sig := googleSignature(c.Extra)
			sigs = append(sigs, sig)
			if sig != "" && sig != skipSignature && sig != studioSig {
				refused = true
			}
		}
	}
	u.mu.Lock()
	u.sent = append(u.sent, sigs)
	reply := u.reply
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if refused {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, studioCorrupted)
		return
	}
	if reply == "" {
		reply = chatOK
	}
	if req.Stream && reply == chatOK {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"id":"ok","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"}}]}`+"\n\n"+
			`data: {"id":"ok","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
		return
	}
	io.WriteString(w, reply)
}

func (u *studioUp) last(t *testing.T) []string {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.sent) == 0 {
		t.Fatal("AI Studio was asked nothing")
	}
	return u.sent[len(u.sent)-1]
}

func (u *studioUp) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.sent)
}

func studioServe(t *testing.T, u *studioUp) {
	t.Helper()
	up := httptest.NewServer(u)
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "google-aistudio-paid", Name: "AI Studio", Key: "k",
		Chat: up.URL + "/v1beta/openai", Models: []string{"gemini-3.8-flash"}}); err != nil {
		t.Fatal(err)
	}
}

func nobodyResting(t *testing.T) {
	t.Helper()
	restingUntil.Lock()
	defer restingUntil.Unlock()
	for id := range restingUntil.m {
		if strings.Contains(id, "aistudio") {
			t.Errorf("%s was rested", id)
		}
	}
}

// piTurn is Pi's next request on the group after the calls with ids
// (openai-completions).
func piTurn(ids []string) string {
	return `{"model":"group/g","stream":false,"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}],"messages":[
		{"role":"user","content":"Weather in Paris and in Tokyo?"},
		{"role":"assistant","content":null,"tool_calls":[
			{"id":"` + ids[0] + `","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}},
			{"id":"` + ids[1] + `","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Tokyo\"}"}}]},
		{"role":"tool","tool_call_id":"` + ids[0] + `","content":"18C, clear"},
		{"role":"tool","tool_call_id":"` + ids[1] + `","content":"21C, rain"}]}`
}

// A signature whose giver isn't known (an id from before signers were
// noted) that AI Studio turns away as corrupted is tried once more with
// the calls as ones it didn't make, and rests nobody.
func TestCorruptedSignatureTriedWithout(t *testing.T) {
	fresh(t)
	u := &studioUp{}
	studioServe(t, u)
	s := New()
	ids := []string{signedID("call_1", "c2lnbmF0dXJlLW9uZQ=="), "call_2"}
	body := strings.Replace(piTurn(ids), "group/g", "google-aistudio-paid/gemini-3.8-flash", 1)
	if code, out := postAs(t, s, "", body); code != http.StatusOK {
		t.Fatalf("%d %s", code, out)
	}
	if u.count() != 2 {
		t.Fatalf("AI Studio was asked %d times, want the one turned away and the one after", u.count())
	}
	if got := u.last(t); len(got) == 0 || got[0] != skipSignature {
		t.Errorf("tried again with %q, want %s", got, skipSignature)
	}
	nobodyResting(t)
}

// A 400 for a signature is about the request's history, not the account:
// it rests nobody and the group goes on to its next member.
func TestCorruptedSignatureNotTheAccount(t *testing.T) {
	if !shapeRefused(http.StatusBadRequest, []byte(studioCorrupted)) {
		t.Error("a corrupted signature isn't taken as the request's shape")
	}
	if !shapeRefused(http.StatusBadRequest, []byte(vertexBadSig)) {
		t.Error("an invalid signature isn't taken as the request's shape")
	}
	fresh(t)
	s := New()
	c := candidate{p: provider.Provider{ID: "google-aistudio-paid", Key: "k"}, model: "gemini-3.8-flash", rest: "google-aistudio-paid"}
	if r := s.restAfter(c, http.StatusBadRequest, http.Header{}, []byte(studioCorrupted)); r.By != "off" || r.Until.After(time.Now()) {
		t.Errorf("a corrupted signature rests the account by %q until %v", r.By, r.Until)
	}
}

// A Gemini client's own history, relayed as it is: a signature another
// provider gave goes as Google's value for calls it didn't make on a call,
// and not at all on text; the provider that gave it gets it as it was.
func TestGeminiBodyKeepsOnlyItsOwn(t *testing.T) {
	fresh(t)
	const vsig, other = "dmVydGV4LXNpZ25lZA==", "dW5rbm93bi1zaWduZWQ="
	noteGeminiSigners([]byte("data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hm\",\"thoughtSignature\":\""+vsig+"\"}]}}]}\r\n\r\n"), "google-vertex")
	if got := signerOf(vsig); got != "google-vertex" {
		t.Fatalf("signer noted as %q", got)
	}
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]},{"role":"model","parts":[
		{"functionCall":{"name":"f","args":{}},"thoughtSignature":"` + vsig + `"},
		{"text":"ok","thoughtSignature":"` + vsig + `"},
		{"functionCall":{"name":"g","args":{}},"thoughtSignature":"` + other + `"}]}]}`)
	if got := geminiBodySignatures(body, "google-vertex", true); string(got) != string(body) {
		t.Errorf("the signer's own body changed:\n%s", got)
	}
	got := geminiBodySignatures(body, "google", true)
	var b struct {
		Contents []struct {
			Parts []map[string]any `json:"parts"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(got, &b); err != nil || len(b.Contents) != 2 || len(b.Contents[1].Parts) != 3 {
		t.Fatalf("%v: %s", err, got)
	}
	parts := b.Contents[1].Parts
	if parts[0]["thoughtSignature"] != skipSignature {
		t.Errorf("the call went with %v", parts[0]["thoughtSignature"])
	}
	if _, ok := parts[1]["thoughtSignature"]; ok {
		t.Errorf("the text went with %v", parts[1]["thoughtSignature"])
	}
	if parts[2]["thoughtSignature"] != other {
		t.Errorf("a signature of no known giver went as %v, want as it was", parts[2]["thoughtSignature"])
	}
	bare := geminiBodySignatures(body, "google-vertex", false)
	if strings.Contains(string(bare), vsig) || strings.Contains(string(bare), other) {
		t.Errorf("asked again without signatures, still sent some: %s", bare)
	}
}
