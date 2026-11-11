package gui

import (
	"encoding/json"
	"fmt"

	"os"
	"path/filepath"

	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

type codexTestCall struct {
	response, model, effort  string
	at                       time.Time
	input, output, reasoning int
}

func writeCodexAPITestThread(t *testing.T, codex, id, session, parent string, calls []codexTestCall) {
	t.Helper()
	start := calls[0].at
	path := filepath.Join(codex, "sessions", start.Format("2006/01/02"), "rollout-"+start.Format("2006-01-02T15-04-05")+"-"+id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	source, err := json.Marshal("cli")
	if parent != "" {
		source, err = json.Marshal(map[string]any{"subagent": map[string]any{"thread_spawn": map[string]string{"parent_thread_id": parent}}})
	}
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{fmt.Sprintf(`{"timestamp":%q,"type":"session_meta","payload":{"id":%q,"session_id":%q,"source":%s}}`, calls[0].at.Add(-time.Hour).Format(time.RFC3339Nano), id, session, source)}
	for i, call := range calls {
		turn := fmt.Sprintf("turn-%s-%d", id, i)
		lines = append(lines,
			fmt.Sprintf(`{"timestamp":%q,"type":"turn_context","payload":{"turn_id":%q,"model":%q,"effort":%q}}`, call.at.Add(-time.Second).Format(time.RFC3339Nano), turn, call.model, call.effort),
			fmt.Sprintf(`{"timestamp":%q,"type":"token_usage_record","payload":{"response_id":%q,"session_id":%q,"thread_id":%q,"turn_id":%q,"usage":{"input_tokens":%d,"cached_input_tokens":0,"cache_write_input_tokens":0,"output_tokens":%d,"reasoning_output_tokens":%d,"total_tokens":%d}}}`, call.at.Format(time.RFC3339Nano), call.response, session, id, turn, call.input, call.output, call.reasoning, call.input+call.output),
		)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func codexSessionByID(t *testing.T, list []sessions.Session, agent, id string) sessions.Session {
	t.Helper()
	for _, s := range list {
		if s.Agent == agent && s.ID == id {
			return s
		}
	}
	t.Fatalf("session %s:%s missing from %+v", agent, id, list)
	return sessions.Session{}
}

func codexEffortByModel(rows []sessions.ModelEffort, model, effort string) (sessions.ModelEffort, bool) {
	for _, row := range rows {
		if row.Model == model && row.Effort == effort {
			return row, true
		}
	}
	return sessions.ModelEffort{}, false
}
