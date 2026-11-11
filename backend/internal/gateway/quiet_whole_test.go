package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"

	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// saidIn reads a stream as its agent would, with the protocol's decoder:
// its reasoning, signatures, text, tool calls, stop and errors, in order.
func saidIn(proto provider.Protocol, stream string) string {
	dec := decoder(proto)
	var said []string
	var kind EventKind = -1
	add := func(k EventKind, s string) {
		if k == kind && k != KToolStart && len(said) > 0 {
			said[len(said)-1] += s
			return
		}
		kind = k
		said = append(said, s)
	}
	readSSE(strings.NewReader(stream), func(_, data string) error {
		if data == "[DONE]" {
			return nil
		}
		return dec(data, func(ev Event) {
			switch ev.Kind {
			case KThink:
				add(KThink, "think:"+ev.Text)
			case KSig:
				add(KSig, "sig:"+ev.Text)
			case KText:
				add(KText, "text:"+ev.Text)
			case KToolStart:
				add(KToolStart, "tool:"+ev.Name+" ")
				kind = KToolArgs
			case KToolArgs:
				said[len(said)-1] += ev.Text
			case KStop:
				add(KStop, "stop:"+ev.Stop)
			case KError:
				add(KError, "error:"+ev.Text)
			}
		})
	})
	return strings.Join(said, "|")
}

func chatChunk(delta, finish string) string {
	return `data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":` + delta + `,"finish_reason":` + finish + `}]}` + "\n\n"
}

// What a client reads off a stream that it can't off the whole reply
// itself: Anthropic's SDKs take a stop's details from message_delta, null
// or not, over message_start's; and a Responses item with no id can be
// named by no event of its own, which openai-node refuses.
func TestWholeAsStreamClientReads(t *testing.T) {
	rec := httptest.NewRecorder()
	if !wholeAsStream(rec, provider.Anthropic, []byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"Partial."}],"stop_reason":"refusal","stop_sequence":null,"stop_details":{"type":"refusal","category":"cyber","explanation":"x"},"usage":{"input_tokens":3,"output_tokens":2}}`)) {
		t.Fatal("not streamed")
	}
	var delta map[string]any
	for _, ev := range events(rec.Body.String()) {
		if ev["type"] == "message_delta" {
			delta, _ = ev["delta"].(map[string]any)
		}
	}
	if d, _ := delta["stop_details"].(map[string]any); d["category"] != "cyber" {
		t.Fatalf("message_delta: %v", delta)
	}
	rec = httptest.NewRecorder()
	if !wholeAsStream(rec, provider.Responses, []byte(`{"id":"resp_1","object":"response","created_at":1,"status":"completed","model":"m","output":[{"type":"function_call","call_id":"call_1","name":"f","arguments":"{\"a\":1}","status":"completed"}]}`)) {
		t.Fatal("not streamed")
	}
	if got := rec.Body.String(); strings.Contains(got, `"item_id":null`) || !strings.Contains(got, `"arguments":"{\"a\":1}"`) || saidIn(provider.Responses, got) != "tool:f {\"a\":1}|stop:tool" {
		t.Fatalf("%s\nsays %q", got, saidIn(provider.Responses, got))
	}
}

// A Responses text's citations go as their own events too, before its
// text is done: ai-sdk (opencode) makes its sources of these alone.
func TestWholeAsStreamResponsesCitations(t *testing.T) {
	rec := httptest.NewRecorder()
	cite := `{"type":"url_citation","start_index":0,"end_index":4,"url":"https://example.com/w","title":"W"}`
	if !wholeAsStream(rec, provider.Responses, []byte(`{"id":"resp_1","object":"response","created_at":1,"status":"completed","model":"m","output":[{"type":"message","id":"msg_1","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Sun.","annotations":[`+cite+`]}]}]}`)) {
		t.Fatal("not streamed")
	}
	var types []string
	var added map[string]any
	for _, ev := range events(rec.Body.String()) {
		types = append(types, ev["type"].(string))
		if ev["type"] == "response.output_text.annotation.added" {
			added = ev
		}
	}
	a, _ := added["annotation"].(map[string]any)
	if added["item_id"] != "msg_1" || added["annotation_index"] != float64(0) || a["url"] != "https://example.com/w" {
		t.Fatalf("annotation.added: %v", added)
	}
	if got := strings.Join(types, " "); !strings.Contains(got, "response.output_text.delta response.output_text.annotation.added response.output_text.done") {
		t.Fatalf("events: %s", got)
	}
}

// A citation another vendor's whole reply carries without the offsets
// OpenAI's schema wants on every annotation (Volcengine Ark's
// url_citation, #4921) goes to no place a client reads one: not in the
// annotation events, nor in the parts, items and reply the client keeps,
// for the client stores them and replays them, and where that schema
// holds the replay is refused whole. The text and a citation that carries
// its offsets go as the vendor wrote them.
func TestWholeAsStreamDropsCitationsWithoutOffsets(t *testing.T) {
	rec := httptest.NewRecorder()
	if !wholeAsStream(rec, provider.Responses, []byte(`{"id":"resp_1","object":"response","created_at":1,"status":"completed","model":"m","output":[{"type":"message","id":"msg_1","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Sun.","annotations":[`+
		`{"type":"url_citation","url":"https://example.com/ark","title":"Ark","site_name":"example.com"},`+
		`{"type":"url_citation","start_index":0,"end_index":3,"url":"https://example.com/w","title":"W"}]}]}]}`)) {
		t.Fatal("not streamed")
	}
	got := rec.Body.String()
	if strings.Contains(got, "example.com/ark") {
		t.Fatalf("the offsetless citation reached the client:\n%s", got)
	}
	// the citation with its offsets is in every place a client reads one:
	// its annotation event, the part, the item and the reply it keeps
	if cited := strings.Count(got, "example.com/w"); cited != 4 {
		t.Fatalf("the citation with its offsets is in %d places, want 4:\n%s", cited, got)
	}
	if said := saidIn(provider.Responses, got); said != "text:Sun.|stop:stop" {
		t.Fatalf("the stream says %q:\n%s", said, got)
	}
}

// A held try's reply under a type that says neither JSON nor a stream,
// once the agent has a stream's headers: its first bytes decide, however
// they are split. A stream is held as a stream from there, read event by
// event like one that says so: its frames stay held, a failure among them
// is the try's (#1418: it went straight through before, and an error
// after its frames reached the agent), and the first word of the reply
// lets it through, the bytes held of its start too.
func TestHeldUnsureStreamPasses(t *testing.T) {
	for _, c := range []struct {
		name   string
		writes []string
		stream bool
	}{
		{"a stream split in its first word", []string{"\n da", "ta: {\"x\":1}\n\n"}, true},
		{"a comment", []string{": hi\n\n"}, true},
		{"an id first", []string{"id: 1\n", "data: {\"x\":1}\n\n"}, true},
		{"a retry first, split", []string{"ret", "ry: 1000\ndata: {}\n\n"}, true},
		{"a whole reply", []string{"{\"id\":", "1}"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h := newHoldWriter(rec, true)
			h.alive = &keptAlive{proto: provider.Chat, sent: true}
			h.Header().Set("Content-Type", "application/octet-stream")
			h.WriteHeader(http.StatusOK)
			all := strings.Join(c.writes, "")
			for _, w := range c.writes {
				if n, err := h.Write([]byte(w)); n != len(w) || err != nil {
					t.Fatalf("wrote %d of %q: %v", n, w, err)
				}
			}
			if c.stream {
				if h.passing || h.whole || !h.stream || h.held.String() != all || rec.Body.Len() != 0 {
					t.Fatalf("frames only: passing %v, whole %v, stream %v, held %q, agent has %q", h.passing, h.whole, h.stream, h.held.String(), rec.Body.String())
				}
				said := "data: " + chatOK + "\n\n"
				h.Write([]byte(said))
				if !h.passing || rec.Body.String() != all+said {
					t.Fatalf("said: passing %v, agent has %q", h.passing, rec.Body.String())
				}
			}
			if !c.stream && (h.passing || !h.whole || h.held.String() != all || rec.Body.Len() != 0) {
				t.Fatalf("passing %v, whole %v, held %q, agent has %q", h.passing, h.whole, h.held.String(), rec.Body.String())
			}
		})
	}
}

// A held try's reply under a type that says neither JSON nor a stream,
// its first bytes slow to come: the agent, sent nothing of it yet, is
// kept alive meanwhile, as it was before such a reply was held.
func TestHeldUnsureKeptAlive(t *testing.T) {
	quietFast(t)
	rec := httptest.NewRecorder()
	h := newHoldWriter(rec, true)
	h.alive = &keptAlive{proto: provider.Chat, sent: true}
	h.atLine = true
	h.Header().Set("Content-Type", "application/octet-stream")
	h.WriteHeader(http.StatusOK)
	time.Sleep(2 * keepaliveEvery)
	h.mu.Lock()
	h.keepQuiet()
	h.mu.Unlock()
	if got := rec.Body.String(); got != ": keepalive\n" {
		t.Fatalf("the agent has %q", got)
	}
	h.Write([]byte("data: {}\n\n"))
	if got := rec.Body.String(); got != ": keepalive\n" {
		t.Fatalf("a frame went: the agent has %q", got)
	}
	h.Write([]byte("data: " + chatOK + "\n\n"))
	if got := rec.Body.String(); got != ": keepalive\ndata: {}\n\ndata: "+chatOK+"\n\n" {
		t.Fatalf("the agent has %q", got)
	}
}

// A held try's reply under a type that says neither JSON nor a stream,
// which begins as a stream, frames only, then fails: the failure is the
// try's, for the next candidate to answer, and nothing of it went to the
// agent (#1418, Aliceonly: the sseStart branch passed such a stream
// through at once).
func TestHeldUnsureStreamFailsHeld(t *testing.T) {
	rec := httptest.NewRecorder()
	h := newHoldWriter(rec, true)
	h.alive = &keptAlive{proto: provider.Responses, sent: true}
	h.Header().Set("Content-Type", "text/plain; charset=utf-8")
	h.WriteHeader(http.StatusOK)
	h.Write([]byte(strings.Join(overloadedLead, "\n\n") + "\n\n"))
	h.Write([]byte("event: response.rate_limits.updated\ndata: {\"type\":\"response.rate_limits.updated\"}\n\n"))
	h.Write([]byte(overloadedFailed + "\n\n"))
	if h.passing || !h.failed() || rec.Body.Len() != 0 {
		t.Fatalf("passing %v, failed %v, agent has %q", h.passing, h.failed(), rec.Body.String())
	}
}
