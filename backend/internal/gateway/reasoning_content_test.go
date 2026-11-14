package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #2855: a Codex conversation that used OpenAI, then went through uni-switch to
// DeepSeek, then had uni-switch disconnected again, was stuck on HTTP 400
// "Invalid 'input[43].content': array too long. Expected an array with
// maximum length 0, but got an array with length 1 instead." — the remote
// compact with it. The reporter's rollout holds the poison: 25 reasoning
// items the DeepSeek phase handed Codex, each with its text in content
// parts and a 38-character encrypted_content of the vendor's own (not the
// gAAAA its GPT-phase items carry), which Codex stores and replays to
// OpenAI when the thread goes back there. OpenAI takes a reasoning item
// sent back only with its content empty (#1411) and cannot open another
// vendor's seal. So what uni-switch hands a client must be re-sendable to a
// strict Responses upstream: a reasoning item uni-switch makes, its text a
// summary, nothing sealed — the shape its own translations have always
// given, which the first test pins.

// dskItem is such an item, as the reporter's rollout holds it: the text
// where OpenAI wants none, a summary empty, and a seal of the vendor's own.
const dskItem = `{"type":"reasoning","id":"rs_dsk1","summary":[],"content":[{"type":"reasoning_text","text":"counting the lines"}],"encrypted_content":"dskseal0000000000000000000000000000000"}`

// dskRelay is a Responses upstream serving a DeepSeek model as a relay in
// front of one does: its reasoning item carries the model's text in it and
// a seal of its own, beside an answer.
type dskRelay struct{}

func (u *dskRelay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/models") {
		io.WriteString(w, `{"models":[{"slug":"deepseek-flash","visibility":"list"}]}`)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	io.WriteString(w, sse(
		`data: {"type":"response.created","response":{"id":"resp_dsk","model":"deepseek-flash","status":"in_progress"}}`,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_dsk1","summary":[]}}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":`+dskItem+`}`,
		`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"message","id":"msg1","role":"assistant","content":[]}}`,
		`data: {"type":"response.output_text.delta","item_id":"msg1","output_index":1,"delta":"42 lines"}`,
		`data: {"type":"response.output_item.done","output_index":1,"item":{"type":"message","id":"msg1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"42 lines"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_dsk","status":"completed","output":[`+dskItem+`,{"type":"message","id":"msg1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"42 lines"}]}],"usage":{"input_tokens":5,"output_tokens":2}}}`))
}

func saveDSK(t *testing.T, u http.Handler) {
	t.Helper()
	srv := httptest.NewServer(u)
	t.Cleanup(srv.Close)
	if err := provider.Save(provider.Provider{ID: "dsk", Name: "DSK", Key: "k", Models: []string{"deepseek-flash"}, Responses: srv.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
}

// clientReasoning reads a client-side stream back: every reasoning item of
// an output_item.done, and every one response.completed's output carries.
func clientReasoning(t *testing.T, body string) (done, completed []map[string]any) {
	t.Helper()
	for _, ln := range strings.Split(body, "\n") {
		data, ok := strings.CutPrefix(strings.TrimSpace(ln), "data: ")
		if !ok || !strings.Contains(data, `"reasoning"`) {
			continue
		}
		var ev struct {
			Type     string         `json:"type"`
			Item     map[string]any `json:"item"`
			Response struct {
				Output []map[string]any `json:"output"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(data), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "response.output_item.done":
			if ev.Item["type"] == "reasoning" {
				done = append(done, ev.Item)
			}
		case "response.completed", "response.incomplete":
			for _, it := range ev.Response.Output {
				if it["type"] == "reasoning" {
					completed = append(completed, it)
				}
			}
		}
	}
	return done, completed
}

// assertResendable is a reasoning item a client may store and replay where
// OpenAI's rules hold: no content, no seal but OpenAI's own — none here —
// and its reasoning as a summary.
func assertResendable(t *testing.T, it map[string]any, text string) {
	t.Helper()
	if c, ok := it["content"]; ok {
		if parts, _ := c.([]any); len(parts) > 0 {
			t.Errorf("reasoning item carries its text: %v", it)
		}
	}
	if enc, _ := it["encrypted_content"].(string); enc != "" {
		t.Errorf("reasoning item carries a seal that is nobody's here: %q", enc)
	}
	sum, _ := it["summary"].([]any)
	if len(sum) == 0 {
		t.Errorf("reasoning item carries no summary: %v", it)
		return
	}
	p0, _ := sum[0].(map[string]any)
	if s, _ := p0["text"].(string); s != text {
		t.Errorf("summary text %q, want the item's reasoning %q", s, text)
	}
}

// A Chat upstream's reasoning, translated for Codex, is an item uni-switch
// makes: an rs_ id and a summary, no content, nothing sealed. It has been
// so on main; this pins the contract #2855 turns on.
func TestTranslatedChatReasoningCarriesNoContent(t *testing.T) {
	fresh(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			io.WriteString(w, `{"models":[{"slug":"deepseek-v4.1-flash","visibility":"list"}]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"","reasoning_content":"weighing the options"}}]}`,
			`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"42 lines"}}]}`,
			`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			`data: [DONE]`))
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "dsh", Name: "DSH", Key: "k", Chat: up.URL + "/v1", Models: []string{"deepseek-v4.1-flash"}}); err != nil {
		t.Fatal(err)
	}
	body := `{"model":"dsh/deepseek-v4.1-flash","stream":true,"store":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"count the lines"}]}]}`
	rec := codexPostTo(New(), body)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "42 lines") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	done, completed := clientReasoning(t, rec.Body.String())
	if len(done) != 1 || len(completed) != 1 {
		t.Fatalf("%d reasoning items done, %d in the completed reply: %s", len(done), len(completed), rec.Body)
	}
	for _, it := range append(done, completed...) {
		if id, _ := it["id"].(string); !strings.HasPrefix(id, "rs_") {
			t.Errorf("reasoning id %q, want one of uni-switch's own", id)
		}
		assertResendable(t, it, "weighing the options")
	}
}

// A Responses upstream's reasoning item with its text in it, served
// translated (Codex asked a search the model doesn't run itself), goes to
// Codex as the upstream wrote it today: stored, and poison when the thread
// replays to OpenAI, on uni-switch or off it (#2855). It goes as an item
// uni-switch makes, its text a summary, nothing sealed.
func TestForeignSealedReasoningGivenCodexCarriesNoContent(t *testing.T) {
	fresh(t)
	saveDSK(t, &dskRelay{})
	body := `{"model":"dsk/deepseek-flash","stream":true,"store":false,"tools":[{"type":"web_search"}],"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"count the lines"}]}]}`
	rec := codexPostTo(New(), body)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "42 lines") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	done, completed := clientReasoning(t, rec.Body.String())
	if len(done) != 1 || len(completed) != 1 {
		t.Fatalf("%d reasoning items done, %d in the completed reply: %s", len(done), len(completed), rec.Body)
	}
	for _, it := range append(done, completed...) {
		if id, _ := it["id"].(string); id != "rs_dsk1" {
			t.Errorf("reasoning id %q, want the item's own, its events named by it", id)
		}
		assertResendable(t, it, "counting the lines")
	}
}

// The same item in a reply relayed as it came — a plain turn to a provider
// serving the model on Responses — which is how a DeepSeek phase usually
// reaches Codex: the bytes alone would leave the poison in Codex's rollout.
// They go through the tidy the translated reply's items do.
func TestForeignSealedReasoningRelayedCarriesNoContent(t *testing.T) {
	fresh(t)
	saveDSK(t, &dskRelay{})
	body := `{"model":"dsk/deepseek-flash","stream":true,"store":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"count the lines"}]}]}`
	rec := codexPostTo(New(), body)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "42 lines") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "sequence_number") {
		t.Fatalf("the reply was translated, not relayed: %s", rec.Body)
	}
	done, completed := clientReasoning(t, rec.Body.String())
	if len(done) != 1 || len(completed) != 1 {
		t.Fatalf("%d reasoning items done, %d in the completed reply: %s", len(done), len(completed), rec.Body)
	}
	for _, it := range append(done, completed...) {
		assertResendable(t, it, "counting the lines")
	}
}

// A whole reply relayed as it came goes through the same tidy, whichever
// filter before it held the reply to its end: a plain model's, and a
// glm-5.3-flash replica's, which crArgs holds whole first (#4256). Before,
// what crArgs held went on past the tidy, the item handed over as it came.
func TestForeignSealedReasoningWholeReplyCarriesNoContent(t *testing.T) {
	for _, model := range []string{"deepseek-flash", "glm-5.3-flash"} {
		t.Run(model, func(t *testing.T) {
			fresh(t)
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/models") {
					io.WriteString(w, `{"models":[{"slug":"`+model+`","visibility":"list"}]}`)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"id":"resp_dsk","object":"response","model":"`+model+`","status":"completed","output":[`+dskItem+`,{"type":"message","id":"msg1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"42 lines"}]}],"usage":{"input_tokens":5,"output_tokens":2}}`)
			}))
			t.Cleanup(up.Close)
			if err := provider.Save(provider.Provider{ID: "dsk", Name: "DSK", Key: "k", Models: []string{model}, Responses: up.URL + "/v1"}); err != nil {
				t.Fatal(err)
			}
			body := `{"model":"dsk/` + model + `","stream":false,"store":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"count the lines"}]}]}`
			rec := codexPostTo(New(), body)
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), "42 lines") {
				t.Fatalf("%d %s", rec.Code, rec.Body)
			}
			var res struct {
				Output []map[string]any `json:"output"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
				t.Fatalf("%v: %s", err, rec.Body)
			}
			n := 0
			for _, it := range res.Output {
				if it["type"] == "reasoning" {
					n++
					assertResendable(t, it, "counting the lines")
				}
			}
			if n != 1 {
				t.Fatalf("%d reasoning items in the reply: %s", n, rec.Body)
			}
		})
	}
}

// The made item's summary is the item's text once: a vendor that writes
// its reasoning as both summary and content (#2855's later rollout) is not
// handed back the words twice, and an item with no summary key at all is
// made over, not a panic.
func TestMadeReasoningSaysItsTextOnce(t *testing.T) {
	for name, raw := range map[string]string{
		"both":       `{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"counting the lines"}],"content":[{"type":"reasoning_text","text":"counting the lines"}]}`,
		"no summary": `{"type":"reasoning","id":"rs_1","content":[{"type":"reasoning_text","text":"counting the lines"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			var it map[string]any
			if err := json.Unmarshal(uniSwitchMadeReasoningRaw(json.RawMessage(raw), ""), &it); err != nil {
				t.Fatal(err)
			}
			assertResendable(t, it, "counting the lines")
		})
	}
}
