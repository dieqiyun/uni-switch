package gateway

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/claudetui"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// A Claude subscription run whose Claude Code exits after its reply
// began, with no message_stop nor a result, is a failure the agent sees,
// in every protocol a Claude subscription is served on, by -p and by the
// interactive mode. It was a 200 with nothing, or the words so far, as a
// reply that ended (#4822). Exited after its message_start alone, the
// spare account answers, as for any failure before the answer. Exited
// part way through the text, a stream already sent has the protocol's
// error event and no end, and the account isn't asked again; a reply not
// streamed has had nothing sent, and is a failure the spare answers.
func TestClaudeSubscriptionRunExitedMidReplyFails(t *testing.T) {
	start := `    echo '{"type":"stream_event","event":{"type":"message_start","message":{"id":"m1","model":"claude-sonnet-5","usage":{"input_tokens":3,"cache_creation_input_tokens":900}}}}'
`
	text := start + `    echo '{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}'
    echo '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"half an ans"}}}'
`
	asks := []struct{ path, body string }{
		{"/v1/messages", `{"model":"claude/claude-sonnet-5","max_tokens":50,"stream":%v,"messages":[{"role":"user","content":"ping"}]}`},
		{"/v1/chat/completions", `{"model":"claude/claude-sonnet-5","stream":%v,"messages":[{"role":"user","content":"ping"}]}`},
		{"/v1/responses", `{"model":"claude/claude-sonnet-5","stream":%v,"input":[{"role":"user","content":[{"type":"input_text","text":"ping"}]}]}`},
	}
	// what each protocol's stream ends a reply with, and fails one with
	ends := map[string][2]string{
		"/v1/messages":         {"event: message_stop", "event: error"},
		"/v1/chat/completions": {"data: [DONE]", `"error":{`},
		"/v1/responses":        {"response.completed", "response.failed"},
	}
	for _, tui := range []bool{false, true} {
		for _, cut := range []string{"start", "text"} {
			for _, ask := range asks {
				for _, stream := range []bool{true, false} {
					t.Run(fmt.Sprintf("tui=%v/%s/%s/stream=%v", tui, cut, ask.path, stream), func(t *testing.T) {
						first := start
						if cut == "text" {
							first = text
						}
						log := claudeSubscriptionFirstSays(t, first+"    exit 0\n")
						helped := filepath.Join(t.TempDir(), "helped")
						if tui {
							// the interactive mode, its helper a stand-in
							// running the Claude Code it was handed
							claudeTUIRuns = true
							t.Cleanup(func() { claudeTUIRuns = false })
							helper := filepath.Join(t.TempDir(), "uni-switch")
							testenv.Program(t, helper, "#!/bin/sh\n[ \"$1\" = \""+claudetui.Command+"\" ] || exit 9\necho start >> '"+helped+"'\nshift; claude=$1; shift\nexec \"$claude\" \"$@\"\n")
							claudeTUIHelper = helper
							t.Cleanup(func() { claudeTUIHelper = "" })
						}
						s := New()
						t.Cleanup(s.subscription.abortAll)
						code, reply := sendTo(s, ask.path, fmt.Sprintf(ask.body, stream))
						b, _ := os.ReadFile(log)
						runs := strings.Join(strings.Fields(string(b)), ",")
						if tui {
							if n := helperStarts(helped); n != strings.Count(runs, ",")+1 {
								t.Fatalf("the helper started %d times for runs %q", n, runs)
							}
						}
						if cut == "text" && stream {
							// the words so far went out; the reply fails
							// where it was cut, and no other run tells it
							// again under them
							end := ends[ask.path]
							if code != 200 || !strings.Contains(reply, "half an ans") || !strings.Contains(reply, end[1]) ||
								strings.Contains(reply, end[0]) || strings.Contains(reply, "pong") || runs != "own" {
								t.Fatalf("status %d (runs %q):\n%s", code, runs, reply)
							}
							if !strings.Contains(reply, "exited before it ended its reply") {
								t.Fatalf("the error doesn't say why:\n%s", reply)
							}
							return
						}
						// nothing has reached the client: the spare answers
						if code != 200 || !strings.Contains(reply, "pong") || strings.Contains(reply, "half an ans") || runs != "own,tok-spare" {
							t.Fatalf("status %d (runs %q):\n%s", code, runs, reply)
						}
						r := lastRoute(s)
						if len(r.Tries) != 2 || r.Tries[0].Status < 500 || !strings.Contains(r.Tries[0].Error, "exited before it ended its reply") {
							t.Fatalf("tries: %+v", r.Tries)
						}
						// what the cut try billed keeps its row (d2a83de61)
						var handed []usage.Record
						for deadline := time.Now().Add(10 * time.Second); len(handed) == 0 && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
							for _, u := range usage.Load(time.Time{}) {
								if u.RouteID == r.ID && u.HandedOn {
									handed = append(handed, u)
								}
							}
						}
						if len(handed) != 1 || handed[0].ProviderAccount != "me@example.com" || handed[0].CacheWrite != 900 {
							t.Fatalf("the cut try's row: %+v", handed)
						}
					})
				}
			}
		}
	}
}
