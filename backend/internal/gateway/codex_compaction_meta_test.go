package gateway

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The reporter's compaction, from Codex Desktop 26.1007 (#2352, yhxwyzm):
// four history items as their Routing page showed them, an MCP tool's call
// with its namespace and list output, then the compaction_trigger, and
// Codex's own client_metadata as sent, x-codex-turn-metadata saying
// "responses_compaction_v2".
func compaction2352(t *testing.T) (body, turn string) {
	t.Helper()
	b, err := os.ReadFile("testdata/codex-26.1007-compaction.json")
	if err != nil {
		t.Fatal(err)
	}
	var q struct {
		ClientMetadata map[string]string `json:"client_metadata"`
	}
	if err := json.Unmarshal(b, &q); err != nil {
		t.Fatal(err)
	}
	turn = q.ClientMetadata["x-codex-turn-metadata"]
	if !strings.Contains(turn, `"implementation":"responses_compaction_v2"`) {
		t.Fatalf("fixture's turn metadata: %s", turn)
	}
	return string(b), turn
}

// A compaction uni-switch summarises itself says so: the request goes with no
// compaction_trigger, and Codex's metadata in the body and the header says
// "responses", as Codex says it of a summary it asks a provider other than
// OpenAI for. It said remote compaction ("responses_compaction_v2") with
// no trigger in the body. The fake refuses that with the relay's words
// (#2352, yhxwyzm; #1171, tcxp001); that this is what the relay checks is
// not confirmed. Nothing else in the metadata changes.
func TestLocalCompactionSaysLocal(t *testing.T) {
	refusal := `{"error":{"code":null,"message":"invalid codex request (request id: 20261015000000000000000000000000)","param":null,"type":"invalid_request_error"}}`
	for _, path := range []string{CodexPath + "/responses", "/v1/responses"} {
		t.Run(path, func(t *testing.T) {
			body, turn := compaction2352(t)
			f := &fake{t: t, reply: sse(
				`data: {"type":"response.created","response":{"id":"r1"}}`,
				`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"SUMMARY"}]}}`,
				`data: {"type":"response.completed","response":{"id":"r1","output":[],"usage":{"input_tokens":5,"output_tokens":2}}}`)}
			remote := []byte(`responses_compaction_v2`)
			f.refuse = func(b []byte) (int, string) {
				if bytes.Contains(b, []byte(`"compaction_trigger"`)) {
					return 0, ""
				}
				if bytes.Contains(b, remote) || strings.Contains(f.head.Get(turnMetadataHeader), string(remote)) {
					return 400, refusal
				}
				return 0, ""
			}
			setup(t, provider.Responses, f)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("POST", path, strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer chatgpt-token")
			req.Header.Set("User-Agent", "Codex Desktop/26.1007 (Mac OS 26.2.0; arm64)")
			req.Header.Set("originator", "Codex Desktop")
			req.Header.Set(turnMetadataHeader, turn)
			New().Handler().ServeHTTP(rec, req)
			if rec.Code != 200 || f.calls == 0 || !strings.Contains(rec.Body.String(), `"type":"compaction"`) {
				t.Fatalf("%d %s\nupstream got %.300s", rec.Code, rec.Body.String(), f.got)
			}
			if bytes.Contains(f.got, []byte(`"compaction_trigger"`)) {
				t.Fatalf("uni-switch's own compaction sent the trigger: %s", f.got)
			}
			var q struct {
				ClientMetadata map[string]string `json:"client_metadata"`
			}
			if err := json.Unmarshal(f.got, &q); err != nil {
				t.Fatal(err)
			}
			want := strings.Replace(turn, `"implementation":"responses_compaction_v2"`, `"implementation":"responses"`, 1)
			if got := q.ClientMetadata["x-codex-turn-metadata"]; got != want {
				t.Errorf("body's turn metadata = %s\nwant %s", got, want)
			}
			if got := f.head.Get(turnMetadataHeader); got != want {
				t.Errorf("header's turn metadata = %s\nwant %s", got, want)
			}
			for _, k := range []string{"mcp_attribution", "guardian_credits_requested", "x-codex-window-id", "turn_id"} {
				if q.ClientMetadata[k] == "" {
					t.Errorf("client_metadata lost %s: %s", k, f.got)
				}
			}
		})
	}
}

// With native compaction on, the trigger goes to the provider as Codex sent
// it, and so does Codex's metadata naming it.
func TestNativeCompactionKeepsItsMetadata(t *testing.T) {
	body, turn := compaction2352(t)
	f := &fake{t: t, reply: sse(
		`data: {"type":"response.created","response":{"id":"r1"}}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"compaction","id":"cmp_1","encrypted_content":"SEALED"}}`,
		`data: {"type":"response.completed","response":{"id":"r1","output":[],"usage":{"input_tokens":5,"output_tokens":2}}}`)}
	setup(t, provider.Responses, f)
	nativeOn(t, "fake")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer chatgpt-token")
	req.Header.Set("User-Agent", "Codex Desktop/26.1007 (Mac OS 26.2.0; arm64)")
	req.Header.Set(turnMetadataHeader, turn)
	New().Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || !bytes.Contains(f.got, []byte(`"compaction_trigger"`)) {
		t.Fatalf("%d %s\nupstream got %s", rec.Code, rec.Body.String(), f.got)
	}
	var q struct {
		ClientMetadata map[string]string `json:"client_metadata"`
	}
	if err := json.Unmarshal(f.got, &q); err != nil {
		t.Fatal(err)
	}
	if got := q.ClientMetadata["x-codex-turn-metadata"]; got != turn {
		t.Errorf("native compaction's metadata changed: %s", got)
	}
	if got := f.head.Get(turnMetadataHeader); got != turn {
		t.Errorf("native compaction's header changed: %s", got)
	}
}

func TestLocalCompactionTurn(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{`{"request_kind":"compaction","compaction":{"trigger":"auto","implementation":"responses_compaction_v2","phase":"pre_turn"},"source":"codex"}`,
			`{"request_kind":"compaction","compaction":{"trigger":"auto","implementation":"responses","phase":"pre_turn"},"source":"codex"}`, true},
		{`{"compaction":{"implementation":"responses"}}`, `{"compaction":{"implementation":"responses"}}`, false},
		{`{"request_kind":"turn","source":"codex"}`, `{"request_kind":"turn","source":"codex"}`, false},
		{`{"compaction":{"trigger":"manual"}}`, `{"compaction":{"trigger":"manual"}}`, false},
		{`{"compaction":`, `{"compaction":`, false},
		{"", "", false},
	} {
		got, ok := localCompactionTurn(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("localCompactionTurn(%s) = %s, %v; want %s, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
