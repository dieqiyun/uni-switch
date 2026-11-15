package library

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
	"github.com/tidwall/jsonc"
)

// grokBlocks is the [[hooks.<ev>]] blocks a Grok config.toml has, as raw text.
// It reads the file plainly (no new symbols) so the test that uses it still
// compiles against the code before this change and fails there with a real
// behavior failure, not a compile error.
func grokBlocks(t *testing.T, path, ev string) []string {
	t.Helper()
	var out []string
	lines := strings.Split(read(t, path), "\n")
	header := "[[hooks." + ev + "]]"
	child := "[[hooks." + ev + "."
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != header {
			continue
		}
		// the block runs to the next [[hooks.*]] header of another event,
		// skipping this event's own child tables ([[hooks.<ev>.hooks]])
		j := i + 1
		for j < len(lines) {
			s := strings.TrimSpace(lines[j])
			if strings.HasPrefix(s, "[[hooks.") && !strings.HasPrefix(s, child) {
				break
			}
			j++
		}
		for j-1 > i && strings.TrimSpace(lines[j-1]) == "" {
			j--
		}
		out = append(out, strings.Join(lines[i:j], "\n")+"\n")
		i = j - 1
	}
	return out
}

// Grok Build and Grok App keep hooks as [[hooks.<Event>]] tables in their
// config.toml. A portable hook lands in each one's own event, with the tool
// matcher under Grok's own tool name, beside the user's own blocks, and comes
// out of it alone leaving the rest as it was.
func TestHooksGrokConfigTables(t *testing.T) {
	h := sandbox(t)
	t.Setenv("GROK_APP_HOME", filepath.Join(h, "grok-app-data"))
	build := filepath.Join(h, ".grok/config.toml")
	app := filepath.Join(h, "grok-app-data", "agent-home/config.toml")
	write(t, build, "# my grok config\nmodel = \"grok-4.7\"\n\n[[hooks.Stop]]\n  [[hooks.Stop.hooks]]\n  type = \"command\"\n  command = \"say my own done\"\n")
	write(t, app, "[models]\ndefault = \"grok-4.7\"\n")

	ok(t)(SaveHook("", Hook{Name: "bark", Event: "turn-end", Command: "curl -s https://api.day.app/KEY/done", Timeout: 10,
		Agents: []string{"grok", "grok-app"}}))
	ok(t)(SaveHook("", Hook{Name: "fmt", Event: "after-tool", Matcher: "edit", Command: "prettier --write .",
		Agents: []string{"grok", "grok-app"}}))

	barkBlk := "[[hooks.Stop]]\n  [[hooks.Stop.hooks]]\n  type = \"command\"\n  command = \"curl -s https://api.day.app/KEY/done\"\n  timeout = 10\n"
	// Grok Build starts with the user's own Stop block; Grok App with none
	if got := grokBlocks(t, build, "Stop"); len(got) != 2 || got[1] != barkBlk {
		t.Errorf("grok build Stop: %q", got)
	}
	if got := grokBlocks(t, app, "Stop"); len(got) != 1 || got[0] != barkBlk {
		t.Errorf("grok app Stop: %q", got)
	}
	for _, p := range []string{build, app} {
		if got := grokBlocks(t, p, "PostToolUse"); len(got) != 1 || !strings.Contains(got[0], "matcher = \"search_replace\"") ||
			!strings.Contains(got[0], "command = \"prettier --write .\"") {
			t.Errorf("%s PostToolUse: %q", p, got)
		}
	}
	// the user's own Stop block stayed first, the comment kept
	if s := read(t, build); !strings.HasPrefix(s, "# my grok config\n") || !strings.Contains(s, "say my own done") {
		t.Errorf("grok build config around the hooks:\n%s", s)
	}

	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	var bark HookView
	for _, hh := range v.Hooks {
		if hh.Name == "bark" {
			bark = hh
		}
	}
	if bark.Native["grok"] != "Stop" || bark.Native["grok-app"] != "Stop" {
		t.Errorf("native events: %+v", bark.Native)
	}

	ok(t)(RemoveHooks([]string{"bark", "fmt"}))
	// the user's own Stop block in Grok Build stays; Grok App's is empty again
	if got := grokBlocks(t, build, "Stop"); len(got) != 1 || !strings.Contains(got[0], "say my own done") {
		t.Errorf("grok build after removal, the user's own gone: %q", got)
	}
	if got := grokBlocks(t, app, "Stop"); len(got) != 0 {
		t.Errorf("grok app after removal, left: %q", got)
	}
	for _, p := range []string{build, app} {
		if got := grokBlocks(t, p, "PostToolUse"); len(got) != 0 {
			t.Errorf("%s PostToolUse left: %q", p, got)
		}
	}
	if v, _ := Read(nil); len(v.Hooks) != 0 {
		t.Errorf("left in the library: %+v", v.Hooks)
	}
}

// An agent's own event ("grok:Notification") is given to Grok Build and Grok
// App together, since they run the one CLI — and an own event of another agent
// is still refused for them.
func TestHooksGrokOwnEventBoth(t *testing.T) {
	h := sandbox(t)
	t.Setenv("GROK_APP_HOME", filepath.Join(h, "grok-app-data"))
	build := filepath.Join(h, ".grok/config.toml")
	app := filepath.Join(h, "grok-app-data", "agent-home/config.toml")
	write(t, build, "model = \"grok-4.7\"\n")
	write(t, app, "[models]\n")
	res, err := SaveHook("", Hook{Name: "idle", Event: "grok:Notification", Matcher: "idle_prompt", Command: "notify", Agents: []string{"grok", "grok-app"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Problems) != 0 {
		t.Errorf("problems: %+v", res.Problems)
	}
	for _, p := range []string{build, app} {
		got := grokBlocks(t, p, "Notification")
		if len(got) != 1 || !strings.Contains(got[0], "matcher = \"idle_prompt\"") || !strings.Contains(got[0], "command = \"notify\"") {
			t.Errorf("%s Notification: %q", p, got)
		}
	}
	if _, err := SaveHook("", Hook{Name: "bad", Event: "codex:PermissionRequest", Command: "x", Agents: []string{"grok"}}); err == nil {
		t.Error("grok accepted Codex's own event")
	}
	ok(t)(RemoveHooks([]string{"idle"}))
	for _, p := range []string{build, app} {
		if got := grokBlocks(t, p, "Notification"); len(got) != 0 {
			t.Errorf("%s Notification left: %q", p, got)
		}
	}
}

// hooksOf is the list a hooks file has under hooks.<ev>, as JSON.
func hooksOf(t *testing.T, path, ev string) []string {
	t.Helper()
	var out []string
	for _, e := range gjson.GetBytes(jsonc.ToJSON([]byte(read(t, path))), "hooks."+ev).Array() {
		out = append(out, canonical([]byte(e.Raw)))
	}
	return out
}

// The two of #1790: Bark when a Codex turn ends, a formatter after Claude
// Code edits a file. Each goes into its agent's file in its own shape,
// beside the user's own hooks, and comes out of it alone: the user's
// hooks, their comments and their other settings stay as they were.
func TestHooksEachAgentsOwnShape(t *testing.T) {
	h := sandbox(t)
	settings := filepath.Join(h, ".claude/settings.json")
	mine := `{
  // the user's own
  "model": "opus",
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "say done"}]}],
    "PostToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "my-log"}]}]
  }
}
`
	write(t, settings, mine)
	codexHooks := filepath.Join(h, ".codex/hooks.json")

	ok(t)(SaveHook("", Hook{Name: "bark", Event: "turn-end", Command: "curl -s https://api.day.app/KEY/done", Timeout: 10,
		Agents: []string{"codex", "gemini", "cursor"}}))
	ok(t)(SaveHook("", Hook{Name: "fmt", Event: "after-tool", Matcher: "edit", Command: "prettier --write .", Agents: []string{"claude", "codex", "gemini", "cursor"}}))

	if got := hooksOf(t, codexHooks, "Stop"); len(got) != 1 || got[0] != `{"hooks":[{"command":"curl -s https://api.day.app/KEY/done","timeout":10,"type":"command"}]}` {
		t.Errorf("codex Stop: %v", got)
	}
	if got := hooksOf(t, codexHooks, "PostToolUse"); len(got) != 1 || got[0] != `{"hooks":[{"command":"prettier --write .","type":"command"}],"matcher":"apply_patch"}` {
		t.Errorf("codex PostToolUse: %v", got)
	}
	if got := hooksOf(t, settings, "PostToolUse"); len(got) != 2 || got[1] != `{"hooks":[{"command":"prettier --write .","type":"command"}],"matcher":"Edit|Write|MultiEdit"}` {
		t.Errorf("claude PostToolUse, the user's first: %v", got)
	}
	if got := hooksOf(t, settings, "Stop"); len(got) != 1 {
		t.Errorf("claude was given bark, which isn't ticked for it: %v", got)
	}
	gem := filepath.Join(h, ".gemini/settings.json")
	if got := hooksOf(t, gem, "AfterAgent"); len(got) != 1 || !strings.Contains(got[0], `"timeout":10000`) {
		t.Errorf("gemini's timeout is in ms: %v", got)
	}
	if got := hooksOf(t, gem, "AfterTool"); len(got) != 1 || !strings.Contains(got[0], `"matcher":"write_file|replace"`) {
		t.Errorf("gemini AfterTool: %v", got)
	}
	cur := filepath.Join(h, ".cursor/hooks.json")
	if got := hooksOf(t, cur, "stop"); len(got) != 1 || got[0] != `{"command":"curl -s https://api.day.app/KEY/done","timeout":10}` {
		t.Errorf("cursor's entry is the command itself: %v", got)
	}
	if v := gjson.Get(read(t, cur), "version").Int(); v != 1 {
		t.Errorf("cursor's hooks.json has version %d", v)
	}

	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Hooks) != 2 || v.Hooks[0].Name != "bark" || v.Hooks[0].Native["codex"] != "Stop" || v.Hooks[0].Native["gemini"] != "AfterAgent" {
		t.Errorf("view: %+v", v.Hooks)
	}

	ok(t)(RemoveHooks([]string{"bark", "fmt"}))
	// the same settings, its comment kept (a list uni-switch wrote into is
	// spaced the way the editor spaces one)
	if got := read(t, settings); canonical(jsonc.ToJSON([]byte(got))) != canonical(jsonc.ToJSON([]byte(mine))) || !strings.Contains(got, "// the user's own") {
		t.Errorf("claude's settings after, not as the user had them:\n%s", got)
	}
	if got := gjson.Get(read(t, codexHooks), "hooks"); got.Exists() {
		t.Errorf("codex's hooks left: %s", got.Raw)
	}
	if got := gjson.Get(read(t, gem), "hooks"); got.Exists() {
		t.Errorf("gemini's hooks left: %s", got.Raw)
	}
	if v, _ := Read(nil); len(v.Hooks) != 0 {
		t.Errorf("left in the library: %+v", v.Hooks)
	}
}

// An entry the user changed after uni-switch wrote it is theirs: taking the
// hook out of the library, or changing it, leaves their entry be. One the
// user had already, the same as the library's, is neither written twice
// nor taken.
func TestHooksTheUsersEditsAreTheirs(t *testing.T) {
	h := sandbox(t)
	settings := filepath.Join(h, ".claude/settings.json")
	write(t, settings, `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"bark"}]}]}}`)
	ok(t)(SaveHook("", Hook{Name: "bark", Event: "turn-end", Command: "bark", Agents: []string{"claude"}}))
	if got := hooksOf(t, settings, "Stop"); len(got) != 1 {
		t.Fatalf("the user's own was written again: %v", got)
	}
	ok(t)(RemoveHooks([]string{"bark"}))
	if got := hooksOf(t, settings, "Stop"); len(got) != 1 {
		t.Fatalf("the user's own was taken out: %v", got)
	}

	ok(t)(SaveHook("", Hook{Name: "fmt", Event: "after-tool", Matcher: "edit", Command: "gofmt -w .", Agents: []string{"claude"}}))
	s := read(t, settings)
	write(t, settings, strings.Replace(s, "gofmt -w .", "gofmt -w ./cmd", 1))
	ok(t)(HookAgents("fmt", []string{}))
	if got := hooksOf(t, settings, "PostToolUse"); len(got) != 1 || !strings.Contains(got[0], "./cmd") {
		t.Errorf("the user's edit of uni-switch's entry was taken out: %v", got)
	}

	// a changed hook goes out as it was and in as it is
	ok(t)(SaveHook("", Hook{Name: "lint", Event: "before-tool", Matcher: "shell", Command: "lint-a", Agents: []string{"claude"}}))
	ok(t)(SaveHook("lint", Hook{Name: "lint", Event: "before-tool", Matcher: "shell", Command: "lint-b", Agents: []string{"claude"}}))
	if got := hooksOf(t, settings, "PreToolUse"); len(got) != 1 || !strings.Contains(got[0], "lint-b") || !strings.Contains(got[0], `"matcher":"Bash"`) {
		t.Errorf("claude PreToolUse after the change: %v", got)
	}
}

// A file that doesn't parse is unknown, never empty: it is left as it is,
// and the hook said not to be given.
func TestHooksUnreadableFileLeftAlone(t *testing.T) {
	h := sandbox(t)
	codexHooks := filepath.Join(h, ".codex/hooks.json")
	bad := `{"hooks": {"Stop": [ oops`
	write(t, codexHooks, bad)
	res, err := SaveHook("", Hook{Name: "bark", Event: "turn-end", Command: "bark", Agents: []string{"codex"}})
	if err != nil {
		t.Fatal(err)
	}
	if read(t, codexHooks) != bad {
		t.Error("a hooks.json uni-switch can't read was written over")
	}
	if !slices.ContainsFunc(res.Problems, func(p Problem) bool { return p.Agent == "codex" && p.What == "hook:bark" }) {
		t.Errorf("problems: %+v", res.Problems)
	}
	if a := mustLoad(t).Applied["codex"]; a != nil && len(a.Hooks) != 0 {
		t.Errorf("recorded as written: %+v", a.Hooks)
	}
}

// Codex trusts a hook by its place in hooks.json: a group of uni-switch's
// taken out from before the user's own moves the user's trust with it,
// and the record of the group taken out goes. The record's key holds
// hooks.json's path, spelled as Codex spells it (codexKey): on Windows the
// path's backslashes make it a literal string (#2275), and a folder with an
// apostrophe as well, an escaped basic one.
func TestHooksCodexTrustMovesWithTheGroups(t *testing.T) {
	for _, c := range []struct{ name, dir string }{
		{"default", ""},
		{"backslash", `co\dex`},
		{"apostrophe", `o'b\rien`},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := sandbox(t)
			dir := filepath.Join(h, ".codex")
			if c.dir != "" {
				dir = filepath.Join(h, c.dir)
				t.Setenv("CODEX_HOME", dir)
			}
			codexHooks := filepath.Join(dir, "hooks.json")
			cfg := filepath.Join(dir, "config.toml")
			write(t, cfg, "model = \"gpt-5\"\n")
			ok(t)(SaveHook("", Hook{Name: "bark", Event: "turn-end", Command: "bark", Agents: []string{"codex"}}))
			// the user adds one of their own after it and trusts both in /hooks
			raw := read(t, codexHooks)
			var doc map[string]map[string][]json.RawMessage
			if err := json.Unmarshal([]byte(raw), &doc); err != nil {
				t.Fatal(err)
			}
			doc["hooks"]["Stop"] = append(doc["hooks"]["Stop"], json.RawMessage(`{"hooks":[{"type":"command","command":"mine"}]}`))
			b, _ := json.Marshal(doc)
			write(t, codexHooks, string(b))
			key := func(g int) string { return codexKey(codexHooks + ":stop:" + string(rune('0'+g)) + ":0") }
			pre := codexKey(codexHooks + ":pre_tool_use:0:0")
			write(t, cfg, read(t, cfg)+"\n[hooks.state]\n\n[hooks.state."+key(0)+"]\ntrusted_hash = \"sha256:aaa\"\n\n[hooks.state."+key(1)+"]\ntrusted_hash = \"sha256:bbb\"\n\n[hooks.state."+pre+"]\nenabled = false\n")

			ok(t)(RemoveHooks([]string{"bark"}))
			if got := hooksOf(t, codexHooks, "Stop"); len(got) != 1 || !strings.Contains(got[0], "mine") {
				t.Fatalf("codex Stop after: %v", got)
			}
			c := read(t, cfg)
			want := "model = \"gpt-5\"\n\n[hooks.state]\n\n[hooks.state." + key(0) + "]\ntrusted_hash = \"sha256:bbb\"\n\n[hooks.state." + pre + "]\nenabled = false\n"
			if c != want {
				t.Errorf("config.toml after:\n%s\nwant:\n%s", c, want)
			}
		})
	}
}

// codexKey is a [hooks.state] key as Codex 0.162 writes it (toml_writer's
// TomlKeyBuilder::as_default, seen from `codex app-server`'s
// config/batchWrite): a basic string when nothing in it needs escaping, a
// literal one when a backslash or a double quote does and it has no
// apostrophe, else a basic string with its escapes.
func codexKey(k string) string {
	if !strings.ContainsAny(k, "\\\"") {
		return `"` + k + `"`
	}
	if !strings.Contains(k, "'") {
		return "'" + k + "'"
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(k) + `"`
}

// An agent's own event goes to that agent alone, and an agent uni-switch can't
// give hooks to is said to be one, on the page, before any change.
func TestHooksNativeEventsAndUnavailableAgents(t *testing.T) {
	h := sandbox(t)
	if _, err := SaveHook("", Hook{Name: "ask", Event: "codex:PermissionRequest", Command: "x", Agents: []string{"claude"}}); err == nil {
		t.Error("codex's own event was given to claude")
	}
	if _, err := SaveHook("", Hook{Name: "bad", Event: "turn-end", Matcher: "shell", Command: "x"}); err == nil {
		t.Error("turn-end was narrowed to the shell")
	}
	if _, err := SaveHook("", Hook{Name: "bad", Event: "after-tool", Command: " "}); err == nil {
		t.Error("a hook with no command was kept")
	}
	ok(t)(SaveHook("", Hook{Name: "ask", Event: "codex:PermissionRequest", Matcher: "Bash", Command: "notify", Agents: []string{"codex"}}))
	ok(t)(SaveHook("", Hook{Name: "bark", Event: "turn-end", Command: "bark"}))
	ok(t)(SomeHooksAgents([]string{"ask", "bark"}, []string{"claude", "opencode"}, true))
	l := mustLoad(t)
	if a := l.hook("ask").Agents; !slices.Equal(a, []string{"codex"}) {
		t.Errorf("ask's agents: %v", a)
	}
	// many at once go to the agents that can take them; one ticked by
	// itself that can't is said why
	if a := l.hook("bark").Agents; !slices.Equal(a, []string{"claude"}) {
		t.Errorf("bark's agents: %v", a)
	}
	if _, err := HookAgents("bark", []string{"claude", "opencode"}); err != nil {
		t.Fatal(err)
	}
	if got := hooksOf(t, filepath.Join(h, ".codex/hooks.json"), "PermissionRequest"); len(got) != 1 || !strings.Contains(got[0], `"matcher":"Bash"`) {
		t.Errorf("codex PermissionRequest: %v", got)
	}
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(v.Hooks, func(x HookView) bool { return x.Name == "bark" })
	if i < 0 || v.Hooks[i].Problems["opencode"] == "" || v.Hooks[i].Problems["claude"] != "" {
		t.Errorf("bark's problems: %+v", v.Hooks)
	}
	j := slices.IndexFunc(v.HookAgents, func(a HookAgentView) bool { return a.ID == "codex" })
	if j < 0 || !slices.Contains(v.HookAgents[j].Events, "PermissionRequest") || v.HookAgents[j].File == "" {
		t.Errorf("codex's view: %+v", v.HookAgents)
	}
}

// Hooks turned off in the agent are said: Claude Code's disableAllHooks.
func TestHooksOffSaid(t *testing.T) {
	h := sandbox(t)
	write(t, filepath.Join(h, ".claude/settings.json"), `{"disableAllHooks": true}`)
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	j := slices.IndexFunc(v.HookAgents, func(a HookAgentView) bool { return a.ID == "claude" })
	if j < 0 || !v.HookAgents[j].Off {
		t.Errorf("claude's view: %+v", v.HookAgents)
	}
}

// #2011 (emo172): a hook's name is letters, digits, - and _, so a hook
// says what it is for in a description, in any language. The library
// keeps it through a save, a rename and a change of agents, the page
// reads it, and no agent's file is given it.
func TestHookDescription(t *testing.T) {
	h := sandbox(t)
	ok(t)(SaveHook("", Hook{Name: "auto-format-queue", Description: "  编辑后\n格式化 ", Event: "after-tool", Matcher: "edit", Command: "fmtq", Agents: []string{"claude"}}))
	if d := mustLoad(t).hook("auto-format-queue").Description; d != "编辑后 格式化" {
		t.Errorf("kept %q, want the words on one line", d)
	}
	ok(t)(HookAgents("auto-format-queue", []string{"claude", "codex"}))
	ok(t)(SaveHook("auto-format-queue", Hook{Name: "fmtq", Description: "编辑后格式化", Event: "after-tool", Matcher: "edit", Command: "fmtq", Agents: []string{"claude", "codex"}}))
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Hooks) != 1 || v.Hooks[0].Name != "fmtq" || v.Hooks[0].Description != "编辑后格式化" {
		t.Errorf("the page's hooks: %+v", v.Hooks)
	}
	for _, f := range []string{".claude/settings.json", ".codex/hooks.json"} {
		if s := read(t, filepath.Join(h, f)); strings.Contains(s, "格式化") || strings.Contains(s, "description") {
			t.Errorf("%s was given the description: %s", f, s)
		}
	}
	// a line, not a document
	if _, err := SaveHook("fmtq", Hook{Name: "fmtq", Description: strings.Repeat("格", 201), Event: "after-tool", Command: "fmtq"}); err == nil {
		t.Error("a 201-character description was kept")
	}
	ok(t)(SaveHook("fmtq", Hook{Name: "fmtq", Description: strings.Repeat("格", 200), Event: "after-tool", Command: "fmtq"}))
}

// Claude Code's own events are all of its HOOK_EVENTS (claude 2.1.296),
// those added after uni-switch's hooks first came among them: one goes to
// settings.json under its own name, and the page offers every one (#2020,
// emo172).
func TestHooksClaudeEventsAll(t *testing.T) {
	h := sandbox(t)
	for _, ev := range []string{"StopFailure", "PostCompact", "FileChanged", "MessageDisplay", "PreModelSwitch", "PostToolBatch"} {
		ok(t)(SaveHook("", Hook{Name: "on-" + ev, Event: "claude:" + ev, Command: "x", Agents: []string{"claude"}}))
		if got := hooksOf(t, filepath.Join(h, ".claude/settings.json"), ev); len(got) != 1 {
			t.Errorf("settings.json %s: %v", ev, got)
		}
	}
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	j := slices.IndexFunc(v.HookAgents, func(a HookAgentView) bool { return a.ID == "claude" })
	if j < 0 || len(v.HookAgents[j].Events) != 33 {
		t.Fatalf("claude's events: %+v", v.HookAgents)
	}
}
