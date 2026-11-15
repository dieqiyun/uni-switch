package gui

import (
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// A turn's end is said in the page's language: the agent, how long the
// turn took, and the folder's own name when the agent says one.
func TestTurnDoneText(t *testing.T) {
	long := gateway.TurnEnd{Agent: "claude", Session: "s", Folder: "/Users/u/work/uni-switch/", Took: 3*time.Minute + 12*time.Second}
	for _, c := range []struct{ lang, title, body string }{
		{"en", "Claude Code is done", "Took 3m 12s · uni-switch"},
		{"zh", "Claude Code 已完成", "用时 3 分 12 秒 · uni-switch"},
		{"zh-TW", "Claude Code 已完成", "用時 3 分 12 秒 · uni-switch"},
		{"ja", "Claude Code が完了しました", "所要時間 3 分 12 秒 · uni-switch"},
		{"de", "Claude Code ist fertig", "Dauer: 3 Min. 12 Sek. · uni-switch"},
	} {
		if title, body := turnDoneText(c.lang, "Claude Code", long); title != c.title || body != c.body {
			t.Errorf("%s: %q / %q", c.lang, title, body)
		}
	}
	for _, c := range []struct {
		lang string
		e    gateway.TurnEnd
		body string
	}{
		{"en", gateway.TurnEnd{Took: 45 * time.Second}, "Took 45s"},
		{"en", gateway.TurnEnd{Took: 2*time.Hour + 5*time.Minute, Folder: `C:\Users\u\proj`}, "Took 2h 5m · proj"},
		{"zh", gateway.TurnEnd{Took: time.Hour + 30*time.Second}, "用时 1 小时 0 分"},
	} {
		if _, body := turnDoneText(c.lang, "Codex", c.e); body != c.body {
			t.Errorf("%s %+v: %q", c.lang, c.e, body)
		}
	}
}
