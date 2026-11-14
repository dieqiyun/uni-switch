package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/tidwall/gjson"
)

// passthroughRefusalBody is the ChatGPT backend's 400 in xuanwo's
// screenshot (on X), the message as it reached the Codex app after
// uni-switch's "Codex: ".
const passthroughRefusalBody = `{"error":{"code":null,"message":"[ObjectParam] [input[2].internal_chat_message_metadata_passthrough.content_token_count] [unknown_parameter] Unknown parameter: 'input[2].internal_chat_message_metadata_passthrough.content_token_count'.","param":null,"type":"invalid_request_error"}}`

// passthroughTurn is a Codex turn whose items carry the passthrough with
// the member the backend doesn't know, and one item without a passthrough.
const passthroughTurn = `{"model":"%s","stream":false,"input":[` +
	`{"type":"message","role":"developer","content":[{"type":"input_text","text":"be brief"}],"internal_chat_message_metadata_passthrough":{"turn_id":"t1","content_token_count":[3]}},` +
	`{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},` +
	`{"type":"message","role":"user","content":[{"type":"input_text","text":"again"}],"internal_chat_message_metadata_passthrough":{"turn_id":"t2","content_item_kinds":["text"],"content_token_count":[1]}}]}`

func passthroughHas(q []byte, field string) bool {
	for _, it := range gjson.GetBytes(q, "input").Array() {
		if it.Get(passthroughKey + "." + field).Exists() {
			return true
		}
	}
	return false
}

// A newer Codex app's passthrough member the ChatGPT backend doesn't know
// turned every new conversation away (xuanwo on X). The turn is asked
// once more without that member, the rest of the passthrough kept, and
// the next turn goes without it at once.
func TestPassthroughMemberLeftOutWhereRefused(t *testing.T) {
	fresh(t)
	// an OpenAI host, which is sent the passthrough as Codex wrote it
	if err := provider.Save(provider.Provider{ID: "chatgpt", Name: "Codex", Key: "k", Responses: "https://api.openai.com/v1", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var sent [][]byte
	s := New()
	s.client = &http.Client{Transport: countTransport(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		sent = append(sent, b)
		mu.Unlock()
		if passthroughHas(b, "content_token_count") {
			return &http.Response{StatusCode: 400, Header: http.Header{"Content-Type": {"application/json"}}, Request: r,
				Body: io.NopCloser(strings.NewReader(passthroughRefusalBody))}, nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Request: r,
			Body: io.NopCloser(strings.NewReader(`{"id":"r1","object":"response","status":"completed","output":[{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":5,"output_tokens":2}}`))}, nil
	})}
	ask := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		body := strings.Replace(passthroughTurn, "%s", "chatgpt/m1", 1)
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body)))
		return rec
	}
	if rec := ask(); rec.Code != 200 || !strings.Contains(rec.Body.String(), "ok") {
		t.Fatalf("first turn: %d %s (asked %d)", rec.Code, rec.Body, len(sent))
	}
	if len(sent) != 2 || !passthroughHas(sent[0], "content_token_count") || passthroughHas(sent[1], "content_token_count") {
		t.Fatalf("asked %d times: %s", len(sent), sent)
	}
	again := sent[1]
	if gjson.GetBytes(again, "input.0."+passthroughKey+".turn_id").String() != "t1" ||
		gjson.GetBytes(again, "input.2."+passthroughKey+".content_item_kinds.0").String() != "text" ||
		gjson.GetBytes(again, "input.#").Int() != 3 || gjson.GetBytes(again, "input.2.content.0.text").String() != "again" {
		t.Fatalf("asked again with: %s", again)
	}
	if rec := ask(); rec.Code != 200 || len(sent) != 3 || passthroughHas(sent[2], "content_token_count") {
		t.Fatalf("next turn: %d, asked %d times", rec.Code, len(sent))
	}
}

// The refusal is known in each place it can name the passthrough; another
// unknown parameter, or an item's other field, is not taken for it.
func TestPassthroughRefusalWordings(t *testing.T) {
	for body, want := range map[string]string{
		passthroughRefusalBody: "content_token_count",
		"Codex: [ObjectParam] [input[55].internal_chat_message_metadata_passthrough.content_token_count] [unknown_parameter] Unknown parameter: 'input[55].internal_chat_message_metadata_passthrough.content_token_count'.":                     "content_token_count",
		`{"error":{"message":"Unknown parameter: 'input[0].internal_chat_message_metadata_passthrough.content_item_metadata'.","param":"input[0].internal_chat_message_metadata_passthrough.content_item_metadata","code":"unknown_parameter"}}`: "content_item_metadata",
		`{"error":{"message":"Unknown parameter: 'input[4].internal_chat_message_metadata_passthrough'.","code":"unknown_parameter"}}`:                                                                                                           "",
	} {
		got, ok := refusedPassthrough([]byte(body))
		if !ok || got != want {
			t.Errorf("%s: %q %v, want %q", body, got, ok, want)
		}
	}
	for _, body := range []string{
		`{"error":{"message":"Unknown parameter: 'input[0].foo'.","code":"unknown_parameter"}}`,
		`{"error":{"message":"Invalid type for 'input[0].internal_chat_message_metadata_passthrough.turn_id': expected a string.","code":"invalid_type"}}`,
		`{"error":{"message":"Unsupported parameter: 'store'."}}`,
	} {
		if f, ok := refusedPassthrough([]byte(body)); ok {
			t.Errorf("taken for a passthrough refusal (%q): %s", f, body)
		}
	}
}

// Only the member goes, from every item that has it; every other byte
// stays as Codex sent it. For the passthrough as a whole, each item's goes.
func TestWithoutPassthrough(t *testing.T) {
	body := []byte(strings.Replace(passthroughTurn, "%s", "gpt-6", 1))
	b, ok := withoutPassthrough(body, "content_token_count")
	if !ok || passthroughHas(b, "content_token_count") || !json.Valid(b) {
		t.Fatalf("%v %s", ok, b)
	}
	want := strings.NewReplacer(`,"content_token_count":[3]`, "", `,"content_token_count":[1]`, "").Replace(string(body))
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
	if _, ok := withoutPassthrough(b, "content_token_count"); ok {
		t.Fatal("removed it twice")
	}
	all, ok := withoutPassthrough(body, "")
	if !ok || strings.Contains(string(all), passthroughKey) || gjson.GetBytes(all, "input.#").Int() != 3 || gjson.GetBytes(all, "input.2.content.0.text").String() != "again" {
		t.Fatalf("%v %s", ok, all)
	}
	// the signed-in Codex relay asks again the same way
	if b2, ok := withoutUnreadable(body, []byte(passthroughRefusalBody)); !ok || string(b2) != want {
		t.Fatalf("withoutUnreadable: %v %s", ok, b2)
	}
}
