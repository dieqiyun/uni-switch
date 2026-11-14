package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// predictionSchema is the schema ChatGPT.app's composer predictions ask
// for, as its main bundle builds it ({suggestion: string|null}).
const predictionSchema = `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"suggestion":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["suggestion"],"additionalProperties":false}`

// codexPredictionRequest is a composer prediction as ChatGPT.app's Codex
// sends one through its ChatGPT sign-in (#1970, pluo): a hidden turn on a
// fork of the thread, on the thread's own model, its thread_source
// composer_predictions, its prompt ending in the schema.
func codexPredictionRequest(t *testing.T, s *Server, path, model string) *httptest.ResponseRecorder {
	t.Helper()
	prompt := "Predict the user's next message.\n\nReturn only JSON matching this schema: " + predictionSchema + "."
	in, _ := json.Marshal(prompt)
	body := `{"model":"` + model + `","stream":true,"input":[` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"fix the login bug"}]},` +
		`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Fixed it; the tests pass."}]},` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":` + string(in) + `}]}],` +
		`"text":{"format":{"type":"json_schema","name":"codex_output_schema","strict":true,"schema":` + predictionSchema + `}}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer chatgpt-token")
	req.Header.Set("session_id", "prediction-thread")
	req.Header.Set("x-codex-turn-metadata", `{"thread_source":"composer_predictions","forked_from_thread_id":"main-chat"}`)
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func predictionReply(text string) string {
	b, _ := json.Marshal(text)
	return sse(
		`data: {"id":"c1","choices":[{"index":0,"delta":{"content":`+string(b)+`}}]}`,
		`data: {"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":30,"completion_tokens":8}}`,
		`data: [DONE]`)
}

func saveCodexPredictions(t *testing.T, v string) {
	t.Helper()
	st := settings.Load()
	st.CodexPredictions = v
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
}

// Codex's composer predictions go where Settings says (#1970): as they
// came, to Codex's own sign-in, by default; off, answered here with no
// prediction and sent nowhere; or to a model of uni-switch's, its answer
// handed back as the {"suggestion": …} Codex reads. Each stays a
// prediction in the Usage view.
func TestCodexPredictionsSetting(t *testing.T) {
	f := &fake{t: t, reply: predictionReply(`{"suggestion":"Run the tests again"}`)}
	setup(t, provider.Chat, f)
	chatgptCalls := 0
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		chatgptCalls++
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`data: {"type":"response.completed","response":{"id":"r1","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"suggestion\":\"From ChatGPT\"}"}]}],"usage":{"input_tokens":9,"output_tokens":2}}}`))
	})
	s := New()

	rec := codexPredictionRequest(t, s, CodexPath+"/responses", "gpt-6-astra")
	if text, _ := outputText(t, rec.Body.String()); chatgptCalls != 1 || f.calls != 0 || text != `{"suggestion":"From ChatGPT"}` {
		t.Fatalf("default: chatgpt %d, uni-switch model %d, %q", chatgptCalls, f.calls, text)
	}

	saveCodexPredictions(t, "off")
	rec = codexPredictionRequest(t, s, CodexPath+"/responses", "gpt-6-astra")
	if text, n := outputText(t, rec.Body.String()); rec.Code != 200 || n != 1 || text != `{"suggestion":null}` || chatgptCalls != 1 || f.calls != 0 {
		t.Fatalf("off: %d %q, %d items, chatgpt %d, uni-switch model %d", rec.Code, rec.Body.String(), n, chatgptCalls, f.calls)
	}

	saveCodexPredictions(t, "fake/m1")
	rec = codexPredictionRequest(t, s, CodexPath+"/responses", "gpt-6-astra")
	if text, _ := outputText(t, rec.Body.String()); rec.Code != 200 || text != `{"suggestion":"Run the tests again"}` || f.calls != 1 || chatgptCalls != 1 {
		t.Fatalf("routed: %d %q, uni-switch model %d, chatgpt %d", rec.Code, rec.Body.String(), f.calls, chatgptCalls)
	}
	var sent struct {
		Model string `json:"model"`
	}
	json.Unmarshal(f.got, &sent)
	if sent.Model != "m1" {
		t.Errorf("the uni-switch model was asked for %q", sent.Model)
	}

	// a turn of the conversation is not a prediction: it goes as before
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(`{"model":"gpt-6-astra","stream":true,"input":"hi"}`))
	req.Header.Set("Authorization", "Bearer chatgpt-token")
	s.Handler().ServeHTTP(rec, req)
	if chatgptCalls != 2 || f.calls != 1 {
		t.Errorf("a plain turn: chatgpt %d, uni-switch model %d", chatgptCalls, f.calls)
	}

	var kinds []string
	for _, u := range usage.Load(time.Time{}) {
		kinds = append(kinds, u.Provider+":"+u.Kind)
		if u.Provider == "uni-switch" && (!u.IsRejected() || u.Status != 200) {
			t.Errorf("an answered-here prediction %+v", u)
		}
		// and says where the setting is: Codex's row on the Agents page
		// since f1e4516d, not Settings (#3425)
		if u.Provider == "uni-switch" && !strings.Contains(u.Error, "in Codex's settings on uni-switch's Agents page") {
			t.Errorf("an answered-here prediction says %q", u.Error)
		}
	}
	want := "openai:composer_predictions uni-switch:composer_predictions fake:composer_predictions openai:"
	if got := strings.Join(kinds, " "); got != want {
		t.Errorf("ledger %s\nwant   %s", got, want)
	}
}

// A model that talks instead of answering with the object gives Codex no
// prediction, never its words as the user's next message, and the call
// is in the Usage view as the failure it is.
func TestNoPrediction(t *testing.T) {
	f := &fake{t: t, reply: predictionReply("Sure! The user will probably ask you to run the tests again.")}
	setup(t, provider.Chat, f)
	saveCodexPredictions(t, "fake/m1")
	s := New()
	rec := codexPredictionRequest(t, s, CodexPath+"/responses", "gpt-6-astra")
	if text, n := outputText(t, rec.Body.String()); rec.Code != 200 || n != 1 || text != `{"suggestion":null}` {
		t.Fatalf("talking reply: %d %q", rec.Code, rec.Body.String())
	}
	var last usage.Record
	for _, u := range usage.Load(time.Time{}) {
		last = u
	}
	if last.Kind != "composer_predictions" || !strings.HasPrefix(last.Error, "prediction: ") || !last.Failed() {
		t.Fatalf("unusable reply was not flagged: %+v", last)
	}
}

// Codex on a uni-switch model (an "OpenAI" provider table pointed at uni-switch,
// #1301), with nothing set: the model asked for writes the prediction,
// its answer wrapped as Codex reads it.
func TestCodexPredictionOnUniSwitch(t *testing.T) {
	for _, path := range []string{"/v1/responses", "/backend-api/codex/responses"} {
		t.Run(path, func(t *testing.T) {
			f := &fake{t: t, reply: predictionReply("```json\n{\"suggestion\": \"  Commit it  \"}\n```")}
			setup(t, provider.Chat, f)
			s := New()
			rec := codexPredictionRequest(t, s, path, "fake/m1")
			if text, _ := outputText(t, rec.Body.String()); rec.Code != 200 || text != `{"suggestion":"Commit it"}` || f.calls != 1 {
				t.Fatalf("on uni-switch: %d %q, calls %d", rec.Code, rec.Body.String(), f.calls)
			}
		})
	}
}

func TestPredictionJSON(t *testing.T) {
	for in, want := range map[string]struct {
		out string
		ok  bool
	}{
		`{"suggestion":"Run it"}`:                         {`{"suggestion":"Run it"}`, true},
		`{"suggestion":null}`:                             {`{"suggestion":null}`, true},
		`{"suggestion":"  "}`:                             {`{"suggestion":null}`, true},
		"<think>hm</think>\n{\"suggestion\":\"Ship it\"}": {`{"suggestion":"Ship it"}`, true},
		"Run the tests":                                   {`{"suggestion":null}`, false},
		`{"description":"x"}`:                             {`{"suggestion":null}`, false},
		`{"suggestion":3}`:                                {`{"suggestion":null}`, false},
		"":                                                {`{"suggestion":null}`, false},
	} {
		if got, ok := predictionJSON(in); got != want.out || ok != want.ok {
			t.Errorf("predictionJSON(%q) = %s %v, want %s %v", in, got, ok, want.out, want.ok)
		}
	}
}
