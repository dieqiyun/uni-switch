package gui

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func TestUniSwitchTrayQuit(t *testing.T) {
	sandboxHome(t)
	previous := quitAgents
	t.Cleanup(func() { quitAgents = previous })
	quitAgents = func() []quitAgentJSON { return []quitAgentJSON{{ID: "codex", Name: "Codex"}} }
	var messages bytes.Buffer
	quits := 0
	host := &uniSwitchHost{encode: json.NewEncoder(&messages), quit: func() { quits++ }}
	host.AskQuit()
	var prompt struct{ Type, Method string }
	if err := json.NewDecoder(&messages).Decode(&prompt); err != nil || prompt.Type != "host" || prompt.Method != "askQuit" || quits != 0 {
		t.Fatalf("first quit: prompt=%+v err=%v quits=%d", prompt, err, quits)
	}
	host.AskQuit()
	if quits != 1 || messages.Len() != 0 {
		t.Fatalf("repeat quit with an unavailable window: quits=%d messages=%s", quits, &messages)
	}
	host.quitAt.Store(0)
	if err := settings.SetQuitNoAsk(true); err != nil {
		t.Fatal(err)
	}
	host.AskQuit()
	if quits != 2 || messages.Len() != 0 {
		t.Fatalf("quit with confirmation disabled: quits=%d messages=%s", quits, &messages)
	}
}
