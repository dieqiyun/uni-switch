package tui

// The sessions page: the agents' sessions (Claude Code's, Codex's,
// OpenCode's, Pi's), as the app's Sessions view shows them — a range, a
// model and a folder to narrow them to; the sessions themselves, the latest
// first, each one resumed with enter; or, with s, what they spent: the
// totals and a chart of the days.

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/proc"
	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// sessRange is a range the page offers: its name and days (0 for all).
type sessRange struct {
	name string
	days int
}

var sessRanges = []sessRange{{"today", 1}, {"7 days", 7}, {"30 days", 30}, {"90 days", 90}, {"all", 0}}

const allModels, allFolders = "all models", "all folders"

// sessFilterMsg is a model or folder picked, "" for every one.
type sessFilterMsg struct {
	folder bool
	value  string
}

func (m *model) reloadSessions() {
	// the whole set, as the page's count beside it is taken from: the list
	// head says how many there are, and the range filter narrows what is
	// shown of them. sessions.Limit is the window gateway attribution is
	// judged over, not a listing's length, so this asks for sessions.All
	m.slist = sessions.List(sessions.All)
	m.ssel = clamp(m.ssel, len(m.sessShown()))
}

// sessShown is the sessions listed: those active in the range, under the
// model and folder picked.
func (m model) sessShown() []sessions.Session {
	return sessFilter(m.slist, sessRanges[m.srange].days, m.smodel, m.sfolder, time.Now())
}

// sessNothing is what an empty range says: until the user allows an
// agent (RustCats on X), uni-switch reads no agent's sessions, and says how.
func sessNothing() string {
	if !sessions.SettleReads().Asked {
		return "uni-switch reads no agent's session history until you allow it · uni-switch sessions read on all (or on <agent>), or the app's Sessions page"
	}
	return "nothing in this range · Claude Code's, Codex's, OpenCode's and Pi's sessions on this computer show up here"
}

func sessFilter(all []sessions.Session, days int, model, folder string, now time.Time) []sessions.Session {
	var since time.Time
	if days > 0 {
		now = now.In(time.Local)
		since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, 1-days)
	}
	var out []sessions.Session
	for _, s := range all {
		if s.Last.Before(since) || folder != "" && s.Cwd != folder {
			continue
		}
		if model != "" && !slices.ContainsFunc(s.Models, func(u sessions.Model) bool { return u.Model == model }) {
			continue
		}
		out = append(out, s)
	}
	return out
}

func (m model) updateSessions(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(sessRanges)
	{
		shown := m.sessShown()
		switch msg.String() {
		case "j", "down":
			m.ssel = min(m.ssel+1, max(0, len(shown)-1))
			return m, nil
		case "k", "up":
			m.ssel = max(0, m.ssel-1)
			return m, nil
		case "enter":
			if m.ssel < len(shown) {
				return m, resumeSession(shown[m.ssel])
			}
			return m, nil
		}
	}
	switch msg.String() {
	case "l", "right":
		m.srange = (m.srange + 1) % n
	case "h", "left":
		m.srange = (m.srange + n - 1) % n
	case "t":
		m.srange = 0
	case "w":
		m.srange = 1
	case "m":
		m.srange = 2
	case "A":
		m.srange = n - 1
	case "M":
		m.openSessPick(false)
		return m, nil
	case "f":
		m.openSessPick(true)
		return m, nil
	case "x":
		m.smodel, m.sfolder, m.ssel = "", "", 0
		return m, nil
	default:
		return m, nil
	}
	m.ssel = 0
	m.reloadSessions()
	return m, nil
}

// resumeSession runs a session's resume command in this terminal, the
// page back when the agent quits.
func resumeSession(s sessions.Session) tea.Cmd {
	if s.Resume == "" {
		return func() tea.Msg { return flashMsg{text: "this session can't be resumed from here"} }
	}
	// the shell's own "no such directory" is gone with the screen it was on
	if fi, err := os.Stat(s.Cwd); s.Cwd != "" && (err != nil || !fi.IsDir()) {
		return func() tea.Msg { return flashMsg{text: "its folder " + tildePath(s.Cwd) + " isn't there any more"} }
	}
	run := sessions.ResumeNow(s)
	c := proc.Command("sh", "-c", run)
	if runtime.GOOS == "windows" {
		c = proc.Command("powershell", "-NoProfile", "-Command", run)
	}
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return flashMsg{text: "resume: " + err.Error()}
		}
		return flashMsg{text: "back from the session", ok: true}
	})
}

// openSessPick picks a model, or a folder, of those the range has under
// the other filter.
func (m *model) openSessPick(folder bool) {
	all, crumb := allModels, "model"
	if folder {
		all, crumb = allFolders, "folder"
	}
	unique := map[string]bool{}
	filterFolder := m.sfolder
	if folder {
		filterFolder = ""
	}
	for _, s := range sessFilter(m.slist, sessRanges[m.srange].days, "", filterFolder, time.Now()) {
		if folder {
			if m.smodel != "" && !slices.ContainsFunc(s.Models, func(v sessions.Model) bool { return v.Model == m.smodel }) {
				continue
			}
			unique[s.Cwd] = true
		} else {
			for _, v := range s.Models {
				unique[v.Model] = true
			}
		}
	}
	var choices []string
	for v := range unique {
		if v != "" {
			choices = append(choices, v)
		}
	}
	slices.Sort(choices)
	items := []agent.Option{{Value: all}}
	for _, v := range choices {
		items = append(items, agent.Option{Value: v})
	}
	m.pk = picker{crumbs: []string{"sessions", crumb}, input: newInput("filter"), items: items, onPick: func(v string) tea.Cmd {
		if v == all {
			v = ""
		}
		return func() tea.Msg { return sessFilterMsg{folder, v} }
	}}
	m.pk.refilter()
	m.mode = modePick
	m.back = modeList
}

func (m model) viewSessions() string {
	return strings.TrimRight(m.header()+"\n\n"+strings.Join(sessListLines(m.sessShown(), m.agents, m.srange, m.smodel, m.sfolder, m.ssel, m.w-len(pad)-2, m.h, time.Now()), "\n"), "\n")
}

func sessBar(rng int, model, folder string) string {
	var tabs []string
	for i, r := range sessRanges {
		if i == rng {
			tabs = append(tabs, sPill.Render(r.name))
		} else {
			tabs = append(tabs, sMuted.Render(" "+r.name+" "))
		}
	}
	filter := func(v, all string) string {
		if v == "" {
			return sMuted.Render(all)
		}
		return sNameOn.Render(v)
	}
	return strings.Join(tabs, " ") + "    " + filter(model, allModels) + sFaint.Render(" · ") + filter(tildePath(folder), allFolders)

}

// sessListLines is the sessions view below the header: the bar, then the
// sessions in the range, the latest first, as many as fit around the one
// picked.
func sessListLines(list []sessions.Session, agents []*agent.Agent, rng int, model, folder string, sel, width, height int, now time.Time) []string {
	var out []string
	add := func(s string) { out = append(out, pad+"  "+s) }
	add(sessBar(rng, model, folder))
	add("")
	if len(list) == 0 {
		if model != "" || folder != "" {
			add(sMuted.Render("no session matches · x clears the filters"))
		} else {
			add(sMuted.Render(sessNothing()))
		}
		return out
	}
	names := map[string]string{}
	for _, a := range agents {
		names[a.ID] = a.Name
	}
	type row struct{ when, who, where, title string }
	rows := make([]row, len(list))
	var wn, ww, wf int
	for i, s := range list {
		where := filepath.Base(s.Cwd)
		if s.Cwd == "" {
			where = "—"
		}
		title := strings.Join(strings.Fields(s.Title), " ")
		if title == "" {
			title = "(untitled)"
		}
		rows[i] = row{sessAgo(s.Last, now), cmp.Or(names[s.Agent], s.Agent), trunc(where, 24), title}
		wn, ww, wf = max(wn, len(rows[i].when)), max(ww, lipgloss.Width(rows[i].who)), max(wf, lipgloss.Width(rows[i].where))
	}
	visible := max(3, height-11) // the header, the bar, the arrows, the command and the footer around them
	start := 0
	if sel >= visible-1 {
		start = sel - visible + 2
	}
	start = max(0, min(start, len(list)-visible))
	if start > 0 {
		add(sFaint.Render(fmt.Sprintf("↑ %d more", start)))
	}
	for i := start; i < len(list) && i < start+visible; i++ {
		r := rows[i]
		marker, who := "  ", sText.Render(padRight(r.who, ww))
		if i == sel {
			marker, who = sCursor.Render("▸ "), sNameOn.Render(padRight(r.who, ww))
		}
		line := marker + sMuted.Render(padRight(r.when, wn)) + "  " + who + "  " + sText.Render(padRight(r.where, wf))
		if room := width - lipgloss.Width(line) - 2; room > 8 {
			line += "  " + sMuted.Render(trunc(r.title, room))
		}
		out = append(out, pad+line)
	}
	if rest := len(list) - start - visible; rest > 0 {
		add(sFaint.Render(fmt.Sprintf("↓ %d more", rest)))
	}
	if sel < len(list) && list[sel].Resume != "" {
		add("")
		add(sFaint.Render(trunc("↵ runs "+list[sel].Resume, max(20, width-2))))
	}
	return out
}

// sessAgo is how long before now, roughly: 5m ago, 3h ago, 2d ago.
func sessAgo(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return t.In(time.Local).Format("2006-01-02")
}

func tildePath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}
