package gui

import (
	"encoding/json"

	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func codexDetailsAPILine(t *testing.T, at time.Time, typ string, payload any) string {
	t.Helper()
	b, err := json.Marshal(struct {
		Timestamp string `json:"timestamp"`
		Type      string `json:"type"`
		Payload   any    `json:"payload"`
	}{at.Format(time.RFC3339Nano), typ, payload})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeCodexDetailsAPIRollout(t *testing.T, codex, id string, at time.Time, lines ...string) {
	t.Helper()
	path := filepath.Join(codex, "sessions", at.Format("2006/01/02"), "rollout-"+at.Format("2006-01-02T15-04-05")+"-"+id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
