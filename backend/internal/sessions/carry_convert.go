package sessions

// Carrying a conversation over to an agent that can't read the session's
// own file (0xPlato on X): Claude Code's is converted into a new Codex
// session, and Codex's into a new Claude Code session, each in that agent's
// own folder and form, so its own resume opens it. The source file is only
// read.
//
// What goes over: what the user and the agent said, a shell command and what
// it printed as the target's own tool call and result (Claude Code's Bash,
// Codex's exec_command), every other tool's call and result as words the
// agent said (the target has no tool of that name), and the user's pasted
// images. What doesn't: reasoning. Claude Code's thinking is checked by its
// signature and Codex's by its encrypted_content, neither of which the other
// vendor can read, and a summary of it is only what the model thought. The
// target picks its model as it does for any resumed session: Codex from its
// config.toml (the rollout names none and keeps no base_instructions, so
// Codex uses its own), Claude Code from its settings; the assistant lines
// say "<synthetic>", Claude Code's own name for a line no model wrote here,
// so uni-switch's usage counts none of them as a call.
//
// The conversion runs when the user asks for it (CarryOver), never on a
// listing, and the one made from a file's bytes is handed back again while
// it is there, so a second click doesn't make a second session.

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/codexsummary"
	"github.com/dieqiyun/uni-switch/backend/internal/proc"
	"github.com/google/uuid"
)

// carryItem is one thing said in a conversation, in neither agent's form.
type carryItem struct {
	Kind   string   // user | assistant | call | result
	Text   string   // what was said; a result's output
	Images []string // a user's images, as data: URLs
	// a call's: its tool and input, the shell line when it ran one, and
	// the folder it ran in
	Name    string
	Input   string
	Shell   string
	Workdir string
	ID      string // a call's, and its result's
	IsError bool
	// a call's result, once paired
	Result string
	Done   bool
	Time   time.Time
}

// carryResultMax is the most of one tool's output carried over: the rest is
// cut, as a model's context would have cut it.
const carryResultMax = 64 << 10

// CarryResult is a session carried over: the new one, and the line that
// opens it in its folder.
type CarryResult struct {
	Agent   string `json:"agent"`
	ID      string `json:"id"`
	Path    string `json:"path"`
	Command string `json:"command"`
}

// convertsTo are the agents a session of an agent can be converted for.
var convertsTo = map[string]string{"claude": "codex", "codex": "claude"}

// agentThere says whether an agent is installed: its command, or its folder.
// A var so tests can say. Answers are kept a little while, as the listing
// asks it of every session on every read.
var agentThere = func(agent string) bool {
	thereMu.Lock()
	defer thereMu.Unlock()
	if t, ok := thereAt[agent]; ok && time.Since(t.at) < 30*time.Second {
		return t.ok
	}
	ok := false
	switch agent {
	case "claude":
		ok = lookOK("claude") || dirOK(ClaudeDir())
	case "codex":
		ok = lookOK("codex") || dirOK(CodexDir())
	}
	thereAt[agent] = there{ok, time.Now()}
	return ok
}

type there struct {
	ok bool
	at time.Time
}

var (
	thereMu sync.Mutex
	thereAt = map[string]there{}
)

func lookOK(name string) bool { _, err := proc.Look(name); return err == nil }
func dirOK(dir string) bool   { st, err := os.Stat(dir); return err == nil && st.IsDir() }

// carried keeps the session made from a file's bytes for a target.
var (
	carriedMu sync.Mutex
	carried   = map[carryKey]CarryResult{}
)

type carryKey struct {
	path, to string
	size     int64
	mod      time.Time
}

// CarryOver converts a session for the agent to (one carries offers it
// with Convert), and says how to open the new one.
func CarryOver(s Session, to string) (CarryResult, error) {
	if !Reads(s.Agent) {
		return CarryResult{}, ErrNotRead
	}
	ok := false
	for _, c := range carries(s) {
		if c.Agent == to && c.Convert {
			ok = true
		}
	}
	if !ok {
		return CarryResult{}, fmt.Errorf("a %s session can't be carried over to %s", s.Agent, to)
	}
	st, err := os.Stat(s.Path)
	if err != nil {
		return CarryResult{}, err
	}
	key := carryKey{s.Path, to, st.Size(), st.ModTime()}
	carriedMu.Lock()
	defer carriedMu.Unlock()
	if r, ok := carried[key]; ok {
		if _, err := os.Stat(r.Path); err == nil {
			return r, nil
		}
	}
	var items []carryItem
	switch s.Agent {
	case "claude":
		items, err = claudeCarryItems(s.Path)
	case "codex":
		items, err = codexCarryItems(s.Path)
	}
	if err != nil {
		return CarryResult{}, err
	}
	items = pairCarry(items)
	if !saysSomething(items) {
		return CarryResult{}, errors.New("nothing was said in the session")
	}
	var r CarryResult
	switch to {
	case "claude":
		r, err = writeClaudeCarry(s.Cwd, items, time.Now())
	case "codex":
		r, err = writeCodexCarry(CodexDir(), s.Cwd, items, time.Now())
	}
	if err != nil {
		return CarryResult{}, err
	}
	r.Command = resumeCommand("", to, r.ID, s.Cwd, r.Path)
	carried[key] = r
	return r, nil
}

func saysSomething(items []carryItem) bool {
	for _, it := range items {
		if it.Kind == "user" {
			return true
		}
	}
	return false
}

// pairCarry puts each result on its call, in the call's place.
func pairCarry(items []carryItem) []carryItem {
	at := map[string]int{}
	out := items[:0:0]
	for _, it := range items {
		switch it.Kind {
		case "call":
			if it.ID != "" {
				at[it.ID] = len(out)
			}
			out = append(out, it)
		case "result":
			if i, ok := at[it.ID]; ok && !out[i].Done {
				out[i].Result, out[i].IsError, out[i].Done = cutResult(it.Text), it.IsError, true
			}
		default:
			out = append(out, it)
		}
	}
	return out
}

func cutResult(s string) string {
	if len(s) <= carryResultMax {
		return s
	}
	cut := carryResultMax
	for cut > 0 && cut < len(s) && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut] + "\n[…]"
}

// note is a call the target has no tool for, as words the agent said.
func (it carryItem) note() string {
	var b strings.Builder
	b.WriteString("[" + it.Name + "]\n")
	if strings.TrimSpace(it.Input) != "" {
		b.WriteString(fence(it.Input))
	}
	if it.Done {
		if it.IsError {
			b.WriteString("\n[error]\n")
		} else {
			b.WriteString("\n[result]\n")
		}
		b.WriteString(fence(it.Result))
	}
	return b.String()
}

// fence is text in a code block no line of it closes.
func fence(s string) string {
	f := "```"
	for strings.Contains(s, f) {
		f += "`"
	}
	return f + "\n" + strings.TrimRight(s, "\n") + "\n" + f + "\n"
}

// ---- reading Claude Code

type ccCarryLine struct {
	Type        string `json:"type"`
	UUID        string `json:"uuid"`
	ParentUUID  string `json:"parentUuid"`
	IsSidechain bool   `json:"isSidechain"`
	IsMeta      bool   `json:"isMeta"`
	Timestamp   string `json:"timestamp"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type ccCarryBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
	Source    struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	} `json:"source"`
}

// claudeCarryItems reads the branch a Claude Code session is on: each line
// names its parent, and the last thing said is where it stands. A
// compaction's boundary has no parent, so what came before it is left to
// the summary after it, as Claude Code does.
func claudeCarryItems(path string) ([]carryItem, error) {
	type node struct {
		parent string
		line   []byte // a user's or the assistant's, else nil
	}
	nodes := map[string]node{}
	leaf := ""
	err := eachLine(path, func(b []byte) {
		var l ccCarryLine
		if json.Unmarshal(b, &l) != nil || l.UUID == "" {
			return
		}
		n := node{parent: l.ParentUUID}
		if (l.Type == "user" || l.Type == "assistant") && !l.IsSidechain {
			n.line = append([]byte(nil), b...)
			leaf = l.UUID
		}
		nodes[l.UUID] = n
	})
	if err != nil {
		return nil, err
	}
	var chain [][]byte
	seen := map[string]bool{}
	for id := leaf; id != "" && !seen[id]; id = nodes[id].parent {
		seen[id] = true
		if n := nodes[id]; n.line != nil {
			chain = append(chain, n.line)
		}
	}
	var items []carryItem
	for i := len(chain) - 1; i >= 0; i-- {
		var l ccCarryLine
		if json.Unmarshal(chain[i], &l) != nil || l.IsMeta {
			continue
		}
		at, _ := time.Parse(time.RFC3339Nano, l.Timestamp)
		items = append(items, claudeLineItems(l, at)...)
	}
	return items, nil
}

func claudeLineItems(l ccCarryLine, at time.Time) []carryItem {
	var text string
	if json.Unmarshal(l.Message.Content, &text) == nil {
		if l.Type == "user" && claudeOwnWords(text) {
			return nil
		}
		return []carryItem{{Kind: l.Type, Text: text, Time: at}}
	}
	var blocks []ccCarryBlock
	if json.Unmarshal(l.Message.Content, &blocks) != nil {
		return nil
	}
	var items []carryItem
	user := carryItem{Kind: "user", Time: at}
	var words []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if l.Type == "user" {
				if !claudeOwnWords(b.Text) {
					words = append(words, b.Text)
				}
			} else if strings.TrimSpace(b.Text) != "" {
				items = append(items, carryItem{Kind: "assistant", Text: b.Text, Time: at})
			}
		case "image":
			if l.Type == "user" && b.Source.Type == "base64" && b.Source.Data != "" {
				user.Images = append(user.Images, "data:"+b.Source.MediaType+";base64,"+b.Source.Data)
			}
		case "tool_use":
			it := carryItem{Kind: "call", Name: b.Name, ID: b.ID, Input: pretty(b.Input), Time: at}
			if b.Name == "Bash" {
				var in struct {
					Command string `json:"command"`
				}
				if json.Unmarshal(b.Input, &in) == nil {
					it.Shell = in.Command
				}
			}
			items = append(items, it)
		case "tool_result":
			items = append(items, carryItem{Kind: "result", ID: b.ToolUseID, Text: resultText(b.Content), IsError: b.IsError, Time: at})
		}
		// thinking and redacted_thinking stay behind: see the top
	}
	if l.Type == "user" && (len(words) > 0 || len(user.Images) > 0) {
		user.Text = strings.Join(words, "\n\n")
		items = append([]carryItem{user}, items...)
	}
	return items
}

// claudeOwnWords is a user line Claude Code wrote itself: a slash
// command's name or its output, a reminder, the caveat over them.
func claudeOwnWords(text string) bool {
	t := strings.TrimSpace(text)
	for _, p := range []string{"<command-name>", "<command-message>", "<command-args>", "<local-command-", "<system-reminder>", "Caveat: The messages below were generated"} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return t == ""
}

// ---- reading Codex

type cxCarryPayload struct {
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
	Input     string          `json:"input"`
	CallID    string          `json:"call_id"`
	Output    json.RawMessage `json:"output"`
	Content   []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL string `json:"image_url"`
	} `json:"content"`
	Action struct {
		Command          []string `json:"command"`
		WorkingDirectory string   `json:"working_directory"`
	} `json:"action"`
	EncryptedContent   string            `json:"encrypted_content"`
	Message            string            `json:"message"`
	ReplacementHistory []json.RawMessage `json:"replacement_history"`
}

// codexCarryItems reads a Codex rollout's items in order. A compaction
// stands for what came before it: the history it kept, or the user's
// words before it and its summary, as Codex rebuilds it.
func codexCarryItems(path string) ([]carryItem, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(rolloutTwin(path)); err == nil {
			path = rolloutTwin(path)
		}
	}
	var items []carryItem
	err := eachLine(path, func(b []byte) {
		var l struct {
			Type      string          `json:"type"`
			Timestamp string          `json:"timestamp"`
			Payload   json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(b, &l) != nil {
			return
		}
		at, _ := time.Parse(time.RFC3339Nano, l.Timestamp)
		switch l.Type {
		case "response_item":
			var p cxCarryPayload
			if json.Unmarshal(l.Payload, &p) == nil {
				items = append(items, codexItem(p, at)...)
			}
		case "compacted":
			var p cxCarryPayload
			if json.Unmarshal(l.Payload, &p) != nil {
				return
			}
			if p.ReplacementHistory != nil {
				items = nil
				for _, raw := range p.ReplacementHistory {
					var q cxCarryPayload
					if json.Unmarshal(raw, &q) == nil {
						items = append(items, codexItem(q, at)...)
					}
				}
			} else if p.Message != "" {
				var kept []carryItem
				for _, it := range items {
					if it.Kind == "user" {
						kept = append(kept, it)
					}
				}
				items = append(kept, carryItem{Kind: "user", Text: codexsummary.Prefix + "\n" + p.Message, Time: at})
			}
		}
	})
	return items, err
}

func codexItem(p cxCarryPayload, at time.Time) []carryItem {
	switch p.Type {
	case "message":
		var words, images []string
		for _, c := range p.Content {
			switch c.Type {
			case "input_text", "output_text", "text":
				words = append(words, c.Text)
			case "input_image":
				if strings.HasPrefix(c.ImageURL, "data:") {
					images = append(images, c.ImageURL)
				}
			}
		}
		text := strings.Join(words, "\n")
		switch p.Role {
		case "user":
			if codexOwnWords(text) && len(images) == 0 {
				return nil
			}
			if codexOwnWords(text) {
				text = ""
			}
			return []carryItem{{Kind: "user", Text: text, Images: images, Time: at}}
		case "assistant":
			if strings.TrimSpace(text) == "" {
				return nil
			}
			return []carryItem{{Kind: "assistant", Text: text, Time: at}}
		}
	case "function_call":
		it := carryItem{Kind: "call", Name: p.Name, ID: p.CallID, Input: pretty(json.RawMessage(p.Arguments)), Time: at}
		var a struct {
			Cmd     string          `json:"cmd"`
			Command json.RawMessage `json:"command"`
			Workdir string          `json:"workdir"`
		}
		if json.Unmarshal([]byte(p.Arguments), &a) == nil {
			switch p.Name {
			case "exec_command":
				it.Shell = a.Cmd
			case "shell", "container.exec":
				var argv []string
				if json.Unmarshal(a.Command, &argv) == nil {
					it.Shell = shellLine(argv)
				}
			case "shell_command":
				json.Unmarshal(a.Command, &it.Shell)
			}
			it.Workdir = a.Workdir
		}
		return []carryItem{it}
	case "custom_tool_call":
		return []carryItem{{Kind: "call", Name: p.Name, ID: p.CallID, Input: p.Input, Time: at}}
	case "local_shell_call":
		return []carryItem{{Kind: "call", Name: "shell", ID: p.CallID, Input: strings.Join(p.Action.Command, " "),
			Shell: shellLine(p.Action.Command), Workdir: p.Action.WorkingDirectory, Time: at}}
	case "function_call_output", "custom_tool_call_output":
		return []carryItem{{Kind: "result", ID: p.CallID, Text: codexOutput(p.Output), Time: at}}
	case "compaction", "compaction_summary":
		// uni-switch's own summary is read; OpenAI's is encrypted for OpenAI only
		if msg, ours := codexsummary.Message(p.EncryptedContent); ours && msg != nil {
			c, _ := msg["content"].([]map[string]any)
			if len(c) > 0 {
				if t, _ := c[0]["text"].(string); t != "" {
					return []carryItem{{Kind: "user", Text: t, Time: at}}
				}
			}
		}
	}
	// reasoning stays behind: see the top
	return nil
}

// codexOwnWords is a user message Codex wrote itself: its context (the
// folder, AGENTS.md, a turn's interruption), not the user's words.
func codexOwnWords(text string) bool {
	t := strings.TrimSpace(text)
	return t == "" || strings.HasPrefix(t, "<") || strings.HasPrefix(t, "# AGENTS.md")
}

// shellLine is a command's argv as the line a shell would run: the script
// of `bash -lc <script>`, else the words quoted.
func shellLine(argv []string) string {
	if len(argv) == 3 && (argv[1] == "-lc" || argv[1] == "-c") {
		return argv[2]
	}
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = a
		if a == "" || strings.ContainsAny(a, " \t\n'\"\\$`*?[]{}()<>|&;#~!") {
			q[i] = shellQuote(a)
		}
	}
	return strings.Join(q, " ")
}

// eachLine tells fn each line of a session file, a packed one unpacked.
func eachLine(path string, fn func([]byte)) error {
	f, err := openLines(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<16)
	for {
		b, err := r.ReadBytes('\n')
		if len(b) > 0 {
			fn(b)
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// ---- writing

func carryStamp(at, now time.Time) string {
	if at.IsZero() {
		at = now
	}
	return at.UTC().Format("2006-01-02T15:04:05.000Z")
}

// writeNew writes a new file through a temp file renamed into place.
func writeNew(path string, lines [][]byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("%s is there already", path)
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	w := bufio.NewWriter(f)
	for _, l := range lines {
		w.Write(l)
		w.WriteByte('\n')
	}
	err = w.Flush()
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// jsonLine is v as one line of JSON, <, > and & as they are, as the
// agents write them.
func jsonLine(v any) []byte {
	b, _ := marshalPlain(v)
	return b
}

func marshalPlain(v any) ([]byte, error) {
	var buf bytes.Buffer
	e := json.NewEncoder(&buf)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// agentVersion is the version the agent wrote its newest listed session
// with, which a file written for it says too ("" when none says).
func agentVersion(agent string) string {
	for _, s := range listSessions(0) {
		if s.Agent != agent || s.Path == "" || s.WSL != "" {
			continue
		}
		v := ""
		n := 0
		eachLine(s.Path, func(b []byte) {
			if v != "" || n > 40 {
				return
			}
			n++
			var l struct {
				Version string `json:"version"`
				Payload struct {
					CLIVersion string `json:"cli_version"`
				} `json:"payload"`
			}
			if json.Unmarshal(b, &l) == nil {
				v = l.Version
				if agent == "codex" {
					v = l.Payload.CLIVersion
				}
			}
		})
		if v != "" {
			return v
		}
	}
	return ""
}

// writeClaudeCarry writes items as a new Claude Code session of the folder
// cwd: <claude dir>/projects/<cwd as Claude names it>/<uuid>.jsonl, each
// line naming the one before it as its parent, its fields in Claude Code's
// order.
func writeClaudeCarry(cwd string, items []carryItem, now time.Time) (CarryResult, error) {
	name, err := claudeProjectName(cwd)
	if err != nil {
		return CarryResult{}, err
	}
	id := uuid.NewString()
	path := filepath.Join(ClaudeDir(), "projects", name, id+".jsonl")
	version := agentVersion("claude")
	if version == "" {
		version = "2.0.0"
	}
	var lines [][]byte
	parent := ""
	msgID := "" // the assistant message lines are part of, while it goes on
	line := func(typ string, message ord, at time.Time, extra ...any) string {
		u := uuid.NewString()
		l := o("parentUuid", nilIfEmpty(parent), "isSidechain", false, "type", typ, "message", message)
		l = append(l, o(extra...)...)
		l = append(l, o("uuid", u, "timestamp", carryStamp(at, now), "userType", "external",
			"entrypoint", "cli", "cwd", cwd, "sessionId", id, "version", version)...)
		lines = append(lines, jsonLine(l))
		parent = u
		return u
	}
	assistant := func(block ord, stop string, at time.Time) string {
		if msgID == "" {
			msgID = "msg_" + randHex(12)
		}
		return line("assistant", o("id", msgID, "type", "message", "role", "assistant", "model", "<synthetic>",
			"content", []any{block}, "stop_reason", stop, "stop_sequence", nil,
			"usage", o("input_tokens", 0, "cache_creation_input_tokens", 0, "cache_read_input_tokens", 0, "output_tokens", 0)), at)
	}
	for _, it := range items {
		switch it.Kind {
		case "user":
			msgID = ""
			var content any = it.Text
			if len(it.Images) > 0 {
				var blocks []any
				if it.Text != "" {
					blocks = append(blocks, o("type", "text", "text", it.Text))
				}
				for _, img := range it.Images {
					if mt, data, ok := splitDataURL(img); ok {
						blocks = append(blocks, o("type", "image", "source", o("type", "base64", "media_type", mt, "data", data)))
					}
				}
				content = blocks
			}
			line("user", o("role", "user", "content", content), it.Time)
		case "assistant":
			assistant(o("type", "text", "text", it.Text), "end_turn", it.Time)
		case "call":
			if it.Shell == "" {
				assistant(o("type", "text", "text", it.note()), "end_turn", it.Time)
				continue
			}
			command := it.Shell
			if it.Workdir != "" && it.Workdir != cwd {
				command = "cd " + shellQuote(it.Workdir) + " && " + command
			}
			callID := "toolu_" + randHex(12)
			by := assistant(o("type", "tool_use", "id", callID, "name", "Bash", "input", o("command", command)), "tool_use", it.Time)
			msgID = ""
			out := it.Result
			if !it.Done {
				out = "[no output was recorded]"
			}
			line("user", o("role", "user", "content", []any{o("tool_use_id", callID, "type", "tool_result", "content", out, "is_error", it.IsError)}), it.Time,
				"toolUseResult", o("stdout", out, "stderr", "", "interrupted", false, "isImage", false),
				"sourceToolAssistantUUID", by)
		}
	}
	if err := writeNew(path, lines); err != nil {
		return CarryResult{}, err
	}
	return CarryResult{Agent: "claude", ID: id, Path: path}, nil
}

// ord is a JSON object whose fields are written in the order given, as the
// agent's own files have them (some readers look for a line's type near
// its start).
type ord []kv

type kv struct {
	k string
	v any
}

// o is an ord of key, value pairs.
func o(pairs ...any) ord {
	out := make(ord, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, kv{pairs[i].(string), pairs[i+1]})
	}
	return out
}

func (m ord) MarshalJSON() ([]byte, error) {
	b := []byte{'{'}
	for i, f := range m {
		if i > 0 {
			b = append(b, ',')
		}
		k, _ := marshalPlain(f.k)
		v, err := marshalPlain(f.v)
		if err != nil {
			return nil, err
		}
		b = append(append(append(b, k...), ':'), v...)
	}
	return append(b, '}'), nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// splitDataURL is a data: URL's media type and base64 data.
func splitDataURL(u string) (string, string, bool) {
	rest, ok := strings.CutPrefix(u, "data:")
	if !ok {
		return "", "", false
	}
	head, data, ok := strings.Cut(rest, ",")
	mt, b64 := strings.CutSuffix(head, ";base64")
	if !ok || !b64 || mt == "" {
		return "", "", false
	}
	return mt, data, true
}

// writeCodexCarry writes items as a new Codex session of the folder cwd:
// <codex home>/sessions/YYYY/MM/DD/rollout-<local time>-<uuid v7>.jsonl.
// Its session_meta says what a rollout's must (the provider is the one
// Codex uses now, so its history lists it) and no base_instructions: Codex
// gives its own. Each user message and answer is written as Codex's own
// event too, which its TUI shows a resumed session's history from. Codex
// adds the thread to its state database itself when it opens it.
func writeCodexCarry(home, cwd string, items []carryItem, now time.Time) (CarryResult, error) {
	u, err := uuid.NewV7()
	if err != nil {
		return CarryResult{}, err
	}
	id := u.String()
	local := now.Local()
	path := filepath.Join(home, "sessions", local.Format("2006"), local.Format("01"), local.Format("02"),
		"rollout-"+local.Format("2006-01-02T15-04-05")+"-"+id+".jsonl")
	version := agentVersion("codex")
	if version == "" {
		version = "0.0.0"
	}
	stamp := carryStamp(now, now)
	lines := [][]byte{jsonLine(o("timestamp", stamp, "type", "session_meta", "payload", o(
		"session_id", id, "id", id, "timestamp", stamp, "cwd", cwd, "originator", "codex_cli_rs",
		"cli_version", version, "source", "cli", "model_provider", CodexProviderIn(home))))}
	add := func(at time.Time, typ string, payload ord) {
		lines = append(lines, jsonLine(o("timestamp", carryStamp(at, now), "type", typ, "payload", payload)))
	}
	say := func(at time.Time, text string) {
		add(at, "response_item", o("type", "message", "role", "assistant",
			"content", []any{o("type", "output_text", "text", text)}))
		add(at, "event_msg", o("type", "agent_message", "message", text))
	}
	for _, it := range items {
		switch it.Kind {
		case "user":
			content := []any{}
			if it.Text != "" {
				content = append(content, o("type", "input_text", "text", it.Text))
			}
			images := []string{}
			for _, img := range it.Images {
				if _, _, ok := splitDataURL(img); ok {
					content = append(content, o("type", "input_image", "image_url", img))
					images = append(images, img)
				}
			}
			add(it.Time, "response_item", o("type", "message", "role", "user", "content", content))
			add(it.Time, "event_msg", o("type", "user_message", "message", it.Text,
				"images", images, "local_images", []any{}, "text_elements", []any{}))
		case "assistant":
			say(it.Time, it.Text)
		case "call":
			if it.Shell == "" {
				say(it.Time, it.note())
				continue
			}
			callID := "call_" + randHex(12)
			workdir := it.Workdir
			if workdir == "" {
				workdir = cwd
			}
			add(it.Time, "response_item", o("type", "function_call", "name", "exec_command",
				"arguments", string(jsonLine(o("cmd", it.Shell, "workdir", workdir))), "call_id", callID))
			out := it.Result
			if !it.Done {
				out = "[no output was recorded]"
			}
			add(it.Time, "response_item", o("type", "function_call_output", "call_id", callID, "output", out))
		}
	}
	if err := writeNew(path, lines); err != nil {
		return CarryResult{}, err
	}
	return CarryResult{Agent: "codex", ID: id, Path: path}, nil
}
