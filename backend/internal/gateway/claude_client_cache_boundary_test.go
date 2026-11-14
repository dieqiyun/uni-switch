package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// classifierBody is the shape of Claude Desktop's auto mode classifier
// (#3835, from Claude Code 2.1.308): a system prompt asking for a severity,
// a user message of project instructions, and a user message whose text
// blocks hold the transcript, the operation to classify, and the trailing
// ask. Round one marks the transcript's end; round two's transcript has
// grown, so its mark moved to the newest entry. No account is read and no
// model is asked: the body is synthetic.
func classifierBody(t *testing.T, grown bool) []byte {
	t.Helper()
	mark := map[string]string{"type": "ephemeral", "ttl": "1h"}
	block := func(s string, marked bool) map[string]any {
		b := map[string]any{"type": "text", "text": s}
		if marked {
			b["cache_control"] = mark
		}
		return b
	}
	content := []map[string]any{
		block("<transcript>\n", false),
		block(strings.Repeat("earlier transcript entry\n", 20000), !grown),
	}
	if grown {
		// the previous operation has become history; the old block stands
		// unchanged but its mark moved on to the newest entry
		content = append(content, block("previous screenshot action\n", false), block("new small entry\n", true))
	}
	content = append(content,
		block("current operation\n", true),
		block("</transcript>\n", false),
		block("Return <severity>N</severity> only.", false),
	)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	// Desktop sends < and > as themselves; Marshal would escape them and
	// miss the shapes the gateway reads in the raw bytes
	enc.SetEscapeHTML(false)
	if err := enc.Encode(map[string]any{
		"model": "claude-sonnet-5", "max_tokens": 64,
		"system": []map[string]any{block("Classify actions with <severity>N</severity>.\n", true)},
		"messages": []map[string]any{
			{"role": "user", "content": []map[string]any{block("<user_claude_md>project rules</user_claude_md>", true)}},
			{"role": "user", "content": content},
		},
	}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// classifierPrefixes serializes each rendered block's content (cache_control
// is metadata, no part of the content a cache prefix covers) and reports
// every block's ending prefix, and which endings a mark wrote.
func classifierPrefixes(t *testing.T, blocks []map[string]any) (ends []string, writes map[string]bool) {
	t.Helper()
	var prefix strings.Builder
	writes = map[string]bool{}
	for _, b := range blocks {
		content := map[string]any{}
		for k, v := range b {
			if k != "cache_control" {
				content[k] = v
			}
		}
		data, err := json.Marshal(content)
		if err != nil {
			t.Fatal(err)
		}
		prefix.Write(data)
		prefix.WriteByte('\n')
		ends = append(ends, prefix.String())
		if b["cache_control"] != nil {
			writes[prefix.String()] = true
		}
	}
	return ends, writes
}

// reusablePrefix is the longest of round two's block endings that round one
// wrote a mark at: a position Anthropic's prefix check may find within its
// lookback of block boundaries. Serialized bytes are not tokens.
func reusablePrefix(t *testing.T, first, second []map[string]any) int {
	t.Helper()
	_, writes := classifierPrefixes(t, first)
	ends, _ := classifierPrefixes(t, second)
	longest := 0
	for _, p := range ends {
		if writes[p] && len(p) > longest {
			longest = len(p)
		}
	}
	return longest
}

// TestClaudeClientCacheKeepsEarlierTranscriptBoundary renders two rounds of
// the classifier and asks that the prefix round one wrote at its transcript
// mark still ends a block of round two, whose marks moved on with the
// growth: Anthropic's check looks back over block boundaries, not into
// merged blocks, so a boundary merged away is a cache written for nothing
// (#3835). The default path is a control: it keeps no transcript at all.
func TestClaudeClientCacheKeepsEarlierTranscriptBoundary(t *testing.T) {
	for _, client := range []bool{false, true} {
		name := "default"
		if client {
			name = "client_marks"
		}
		t.Run(name, func(t *testing.T) {
			var rounds [2][]map[string]any
			for i, grown := range []bool{false, true} {
				r, err := parseAnthropicRequest(classifierBody(t, grown), client)
				if err != nil {
					t.Fatal(err)
				}
				if !autoModeClassifier(r) || len(r.Messages) != 2 {
					t.Fatal("fixture does not match the observed two-message classifier shape")
				}
				rounds[i], err = renderClaudePrompt(r)
				if err != nil {
					t.Fatal(err)
				}
			}
			longest := reusablePrefix(t, rounds[0], rounds[1])
			t.Logf("before blocks=%d, after blocks=%d, reusable serialized prefix bytes=%d", len(rounds[0]), len(rounds[1]), longest)
			if !client && longest > 1000 {
				t.Fatal("default reproduction unexpectedly preserves the transcript")
			}
			if client && longest < 400000 {
				t.Fatal("client marks survived, but the earlier transcript block boundary was merged away")
			}
		})
	}
}

// TestClaudeClientCacheClassifierWithoutHeader serves the classifier through
// the subscription path as Desktop sends it: native cache_control, no
// private header. Its marks must be kept like an opted-in client's (#3835),
// and the transcript boundary must survive the round's growth.
func TestClaudeClientCacheClassifierWithoutHeader(t *testing.T) {
	s := New()
	t.Cleanup(s.subscription.abortAll)
	var rounds [2]*Request
	for i, grown := range []bool{false, true} {
		body := classifierBody(t, grown)
		r := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
		w := httptest.NewRecorder()
		s.serveSubscription(w, r, provider.Anthropic, "Claude Code", "claude-sonnet-5", "test", body, &Usage{},
			func(_ context.Context, req *Request) (*subscriptionRun, <-chan Event, error) {
				rounds[i] = req
				return nil, nil, fmt.Errorf("stop before a run starts")
			})
	}
	if rounds[0] == nil || rounds[1] == nil {
		t.Fatal("a request never reached run startup")
	}
	if rounds[0].clientCache == nil || rounds[1].clientCache == nil {
		t.Fatal("classifier marks dropped without the private header")
	}
	first, err := renderClaudePrompt(rounds[0])
	if err != nil {
		t.Fatal(err)
	}
	second, err := renderClaudePrompt(rounds[1])
	if err != nil {
		t.Fatal(err)
	}
	if longest := reusablePrefix(t, first, second); longest < 400000 {
		t.Fatalf("marks kept, boundary merged away: reusable serialized prefix bytes=%d", longest)
	}
}

// A classifier whose marks the bridge can't keep (a 1h mark after a 5m
// one, which Anthropic refuses) never asked for them through uni-switch's
// header, so it is served the default way, as before #3835, not refused.
func TestClaudeClientCacheClassifierBadMarksServedAsBefore(t *testing.T) {
	body := classifierBody(t, false)
	body = bytes.Replace(body, []byte(`"cache_control":{"ttl":"1h","type":"ephemeral"}`), []byte(`"cache_control":{"ttl":"5m","type":"ephemeral"}`), 1)
	if !bytes.Contains(body, []byte(`"ttl":"5m"`)) {
		t.Fatal("fixture: no mark turned to 5m")
	}
	s := New()
	t.Cleanup(s.subscription.abortAll)
	var got *Request
	r := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.serveSubscription(w, r, provider.Anthropic, "Claude Code", "claude-sonnet-5", "test", body, &Usage{},
		func(_ context.Context, req *Request) (*subscriptionRun, <-chan Event, error) {
			got = req
			return nil, nil, fmt.Errorf("stop before a run starts")
		})
	if got == nil {
		t.Fatalf("refused before run startup: %d %s", w.Code, w.Body)
	}
	if got.clientCache != nil {
		t.Error("marks the bridge can't keep were kept")
	}
}
