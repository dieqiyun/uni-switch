package gateway

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// oldTurnBody is what a socket's turn wrote before turnBody.
func oldTurnBody(q map[string]json.RawMessage, input []json.RawMessage) ([]byte, error) {
	in, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	q["input"] = in
	return json.Marshal(q)
}

func longSocketTurn(items int) (map[string]json.RawMessage, []json.RawMessage) {
	frame := `{"type":"response.create","model":"fake/m1","stream":true,"instructions":"be <brief> & kind` + "\u2028\u2029" + `","input":[` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}],` +
		`"tools":[ {"type": "function", "name": "a<b", "parameters": {"type":"object"}} ],"prompt_cache_key":null}`
	var q map[string]json.RawMessage
	if err := decodeJSONView([]byte(frame), &q); err != nil {
		panic(err)
	}
	line := strings.Repeat("读这个文件 if a < b && c > d { return \"x\\ty\" }\n", 20)
	var input []json.RawMessage
	for i := range items {
		// <, > and & as Codex sends them: as they are
		var b bytes.Buffer
		e := json.NewEncoder(&b)
		e.SetEscapeHTML(false)
		e.Encode(line + strconv.Itoa(i) + "\u2028\u2029")
		text := bytes.TrimSpace(b.Bytes())
		// pretty-printed, as a reply's item may be
		input = append(input, json.RawMessage(`{ "type": "message",`+"\n\t"+`"role": "user", "content": [ {"type":"input_text", "text": `+string(text)+`} ] }`))
	}
	return q, input
}

// A socket's turn is written as json.Marshal wrote it, byte for byte:
// keys sorted, values compacted, <, >, & and U+2028, U+2029 escaped.
func TestTurnBodyIsJSONMarshal(t *testing.T) {
	for _, items := range []int{0, 1, 40} {
		q, input := longSocketTurn(items)
		want, err := oldTurnBody(q, input)
		if err != nil {
			t.Fatal(err)
		}
		q, input = longSocketTurn(items)
		got, err := turnBody(q, input)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%d items: %v\n got %.300s\nwant %.300s", items, err, got, want)
		}
	}
	q, _ := longSocketTurn(0)
	if got, err := turnBody(q, nil); err != nil || !bytes.Contains(got, []byte(`"input":null`)) {
		t.Errorf("nil input: %s %v", got, err)
	}
	q, input := longSocketTurn(2)
	input[1] = json.RawMessage(`{"bad"`)
	if _, err := turnBody(q, input); err == nil {
		t.Error("a broken item went upstream")
	}
}

// A long conversation on a socket is written once a turn: json.Marshal
// of the list and then of the body around it allocated ~5 copies.
func TestTurnBodyWrittenOnce(t *testing.T) {
	q, input := longSocketTurn(2000)
	body, _ := turnBody(q, input)
	size := uint64(len(body))
	got := allocatedEach(func() { turnBody(q, input) })
	if got > size*5/4 {
		t.Errorf("a %d-byte turn allocated %d bytes (%.1f×)", size, got, float64(got)/float64(size))
	}
	t.Logf("body %d: turnBody %d, json.Marshal %d", size, got, allocatedEach(func() { oldTurnBody(q, input) }))
}
