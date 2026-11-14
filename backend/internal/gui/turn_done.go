package gui

import (
	"fmt"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// turnDoneText is the notification that an agent's turn is done (itieniu
// on X), in the language (en, zh, zh-TW, ja or de): the agent by its name,
// how long the turn took, and the folder it works in when it says one.
func turnDoneText(lang, agent string, t gateway.TurnEnd) (title, body string) {
	took := turnTook(lang, t.Took)
	dir := gateway.TurnFolder(t.Folder)
	switch lang {
	case "zh":
		title, body = agent+" 已完成", "用时 "+took
		if dir != "" {
			body += " · " + dir
		}
	case "zh-TW":
		title, body = agent+" 已完成", "用時 "+took
		if dir != "" {
			body += " · " + dir
		}
	case "ja":
		title, body = agent+" が完了しました", "所要時間 "+took
		if dir != "" {
			body += " · " + dir
		}
	case "de":
		title, body = agent+" ist fertig", "Dauer: "+took
		if dir != "" {
			body += " · " + dir
		}
	default:
		title, body = agent+" is done", "Took "+took
		if dir != "" {
			body += " · " + dir
		}
	}
	return title, body
}

// turnTook is a turn's length in minutes and seconds, or hours and minutes.
func turnTook(lang string, d time.Duration) string {
	s := int(d.Round(time.Second) / time.Second)
	h, m, sec := s/3600, s/60%60, s%60
	type units struct{ h, m, s string }
	u := map[string]units{
		"zh": {" 小时 ", " 分 ", " 秒"}, "zh-TW": {" 小時 ", " 分 ", " 秒"},
		"ja": {" 時間 ", " 分 ", " 秒"}, "de": {" Std. ", " Min. ", " Sek."},
	}[lang]
	if u.h == "" {
		u = units{"h ", "m ", "s"}
	}
	trim := func(x string) string {
		if len(x) > 0 && x[len(x)-1] == ' ' {
			return x[:len(x)-1]
		}
		return x
	}
	switch {
	case h > 0:
		return fmt.Sprintf("%d%s%d%s", h, u.h, m, trim(u.m))
	case m > 0:
		return fmt.Sprintf("%d%s%d%s", m, u.m, sec, u.s)
	}
	return fmt.Sprintf("%d%s", sec, u.s)
}

// agentName is an agent's name as uni-switch shows it, its id for one it
// doesn't know.
func agentName(id string) string {
	for _, a := range agent.All() {
		if a.ID == id {
			return a.Name
		}
	}
	return id
}
