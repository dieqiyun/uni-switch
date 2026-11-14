package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// staleOnWindows is why the tests of copies told from the process list
// don't run on Windows: StaleCopies lists no processes there, only the
// daemon as last checked (TestCodexStaleOnWindowsIsTheDaemonsLastCheck).
const staleOnWindows = "Windows lists no processes for StaleCopies; see TestCodexStaleOnWindowsIsTheDaemonsLastCheck"

// Codex writes config.toml itself (a project's trust, a notice seen), so a
// copy reopened after uni-switch's change and then writing there was still
// told to reopen; an app-server daemon left running for another
// CODEX_HOME was counted too (#729). Only uni-switch's own change counts, for
// copies of this home's Codex.
func TestCodexStaleOnlyForUniSwitchsChange(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip(staleOnWindows)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CODEX_HOME", "")
	dir := filepath.Join(home, ".codex")
	os.MkdirAll(dir, 0o755)
	cfg := filepath.Join(dir, "config.toml")
	list := filepath.Join(dir, "uni-switch-models.json")
	conf := "model = \"uni-switch/gpt-6-luna\"\nmodel_provider = \"uni-switch\"\nmodel_catalog_json = \"" + tomlString(list) + "\"\n\n" +
		"[model_providers.uni-switch]\nname = \"uni-switch\"\nbase_url = \"http://127.0.0.1:3425/v1\"\n"
	os.WriteFile(cfg, []byte(conf), 0o600)
	os.WriteFile(list, []byte(`{"models":[{"slug":"uni-switch/gpt-6-luna"}]}`), 0o644)
	then := time.Now().Add(-2 * time.Hour)
	os.Chtimes(cfg, then, then)
	os.Chtimes(list, then, then)

	was := running
	t.Cleanup(func() { running = was })
	running = func(string) []runningProc {
		return []runningProc{
			// the ChatGPT app's Codex, reopened an hour ago
			{time.Hour, "/Applications/ChatGPT.app/Contents/Resources/codex app-server --listen stdio://"},
			// a daemon another CODEX_HOME's Codex left running
			{3 * time.Hour, "/tmp/sb/.codex/packages/app-server-daemon/releases/0.160.0/bin/codex app-server --listen unix:// --managed-daemon"},
		}
	}
	a := codex(home)
	if n := a.Stale(); n != 0 {
		t.Fatalf("reopened after uni-switch's change, other home's daemon: %d stale, want 0", n)
	}

	// Codex trusts a project: config.toml written now, uni-switch's part as it was
	f, _ := os.OpenFile(cfg, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("\n[projects.\"/Users/x/work\"]\ntrust_level = \"trusted\"\n")
	f.Close()
	if n := a.Stale(); n != 0 {
		t.Fatalf("Codex's own write: %d stale, want 0", n)
	}

	// uni-switch's list changes: the copy running from before has the old one
	os.WriteFile(list, []byte(`{"models":[{"slug":"uni-switch/gpt-6-luna"},{"slug":"uni-switch/new"}]}`), 0o644)
	if n := a.Stale(); n != 1 {
		t.Fatalf("uni-switch's list changed: %d stale, want 1", n)
	}
	// and stays so while nothing else changes, uni-switch restarted or not
	if n := codex(home).Stale(); n != 1 {
		t.Fatalf("asked again: %d stale, want 1", n)
	}
}

// miaopasi on Discord: the Codex row stayed "已接入 · 重开 Codex 后生效"
// after Codex was restarted. On macOS closing the Codex app's window
// leaves the app running, its app-server with it; an editor's Codex and
// the CLI's managed daemon run on through a reopened app too. Each copy
// left is told apart, so the row can say which one and how it is reopened,
// and one app's processes are one copy, not one per process.
func TestCodexStaleCopiesSayWhichIsLeft(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip(staleOnWindows)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CODEX_HOME", "")
	dir := filepath.Join(home, ".codex")
	os.MkdirAll(dir, 0o755)
	cfg := filepath.Join(dir, "config.toml")
	os.WriteFile(cfg, []byte("model = \"gpt-6.1-sol\"\nopenai_base_url = \"http://127.0.0.1:3425/backend-api/codex\"\n"), 0o600)
	then := time.Now().Add(-2 * time.Hour)
	os.Chtimes(cfg, then, then)

	was := running
	t.Cleanup(func() { running = was })
	app := "/Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex"
	running = func(string) []runningProc {
		return []runningProc{
			// the Codex app, its window closed and opened again: the app
			// and its processes from three days ago
			{72 * time.Hour, app + " -c features.code_mode_host=true app-server --analytics-default-enabled"},
			{72*time.Hour - 10*time.Second, app + " exec-server --remote https://codex-cloud-environments.chatgpt.com/api"},
			// VS Code's Codex
			{5 * time.Hour, home + "/.vscode/extensions/openai.chatgpt-26.1003.0-darwin-arm64/bin/macos-aarch64/codex app-server --analytics-default-enabled"},
			// the CLI's daemon for this home
			{4 * time.Hour, dir + "/packages/app-server-daemon/releases/0.160.0-aarch64-apple-darwin/bin/codex app-server --listen unix:// --managed-daemon"},
			// another app's own Codex (miaopasi: Agents Anywhere's
			// connector), its app-server talked to over stdio
			{20 * time.Hour, home + "/Library/Application Support/Agents Anywhere/connector/.venv/lib/python3.12/site-packages/codex_cli_bin/bin/codex app-server --listen stdio://"},
			// a codex fnm installed, run in a terminal: no app's
			{20 * time.Hour, home + "/Library/Application Support/fnm/node-versions/v24.1.0/installation/bin/codex"},
			// a codex in a terminal from before, and one opened since
			{3 * time.Hour, "/opt/homebrew/bin/codex"},
			{time.Minute, "/opt/homebrew/bin/codex"},
		}
	}
	a := codex(home)
	got := a.StaleCopies()
	var kinds []string
	for _, c := range got {
		kinds = append(kinds, c.Kind)
	}
	if want := []string{"app", "ide", "daemon", "embedded", "cli", "cli"}; strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("copies: %v, want %v", kinds, want)
	}
	if d := time.Since(got[0].Since); d < 72*time.Hour-time.Minute || d > 72*time.Hour+time.Minute {
		t.Fatalf("the app's copy started %v ago, want the app-server's 72h", d)
	}
	if got[3].App != "Agents Anywhere" {
		t.Fatalf("the embedded copy's app: %q, want Agents Anywhere", got[3].App)
	}
	if n := a.Stale(); n != 6 {
		t.Fatalf("Stale: %d, want 6 (one app, not one per process)", n)
	}
}

// lychjck's #1461: the managed daemon keeps a long-lived helper next to it
// (codex app-server daemon pid-update-loop) that a daemon restart doesn't
// end and that reads no list. It matches the codex startsWith pattern and
// no --managed-daemon, so it was counted as a "cli" copy from before
// uni-switch's change: "A Codex in a terminal" that doesn't exist, forever,
// and only killing the process cleared the row. A daemon helper is no
// copy to reopen.
func TestCodexDaemonHelperIsNoStaleCopy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip(staleOnWindows)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CODEX_HOME", "")
	dir := filepath.Join(home, ".codex")
	os.MkdirAll(dir, 0o755)
	cfg := filepath.Join(dir, "config.toml")
	os.WriteFile(cfg, []byte("model = \"gpt-6.1-sol\"\nopenai_base_url = \"http://127.0.0.1:3425/backend-api/codex\"\n"), 0o600)
	then := time.Now().Add(-2 * time.Hour)
	os.Chtimes(cfg, then, then)

	was := running
	t.Cleanup(func() { running = was })
	running = func(string) []runningProc {
		return []runningProc{
			// the helper, from before uni-switch's change
			{3 * time.Hour, "/opt/homebrew/bin/codex app-server daemon pid-update-loop"},
		}
	}
	if got := codex(home).StaleCopies(); len(got) != 0 {
		t.Fatalf("the daemon's helper counted a stale copy: %+v, want none", got)
	}
	// a terminal Codex from before is still one
	running = func(string) []runningProc {
		return []runningProc{
			{3 * time.Hour, "/opt/homebrew/bin/codex"},
		}
	}
	if n := codex(home).Stale(); n != 1 {
		t.Fatalf("a terminal codex: %d stale, want 1", n)
	}
}

// On Windows, where listing processes is a PowerShell run, the Codex row's
// stale copy is the daemon as KeepCodexDaemonCurrent last found it (#1388):
// behind uni-switch's change, until it is restarted, while the check is
// recent. Elsewhere the same last check is kept for the row, beside the
// process list.
func TestCodexStaleOnWindowsIsTheDaemonsLastCheck(t *testing.T) {
	home, _ := codexHome(t, "", "")
	dir := filepath.Join(home, ".codex")
	cfg, list := filepath.Join(dir, "config.toml"), filepath.Join(dir, "uni-switch-models.json")
	os.WriteFile(cfg, []byte("model_provider = \"uni-switch\"\nmodel_catalog_json = \""+tomlString(list)+"\"\n"), 0o600)
	os.WriteFile(list, []byte(`{"models":[{"slug":"uni-switch/a"}]}`), 0o644)
	wasKeep, wasRunning := keepCodexDaemon, running
	t.Cleanup(func() {
		keepCodexDaemon, running = wasKeep, wasRunning
		codexDaemonSeen.Lock()
		codexDaemonSeen.at, codexDaemonSeen.check = time.Time{}, provider.CodexDaemonCheck{}
		codexDaemonSeen.Unlock()
	})
	running = func(string) []runningProc { return nil }
	since := time.Now().Add(-3 * time.Hour)
	answer := provider.CodexDaemonCheck{}
	keepCodexDaemon = func(context.Context, string, time.Time) provider.CodexDaemonCheck { return answer }
	// the copies the row shows: on Windows StaleCopies itself
	copies := func() []StaleCopy {
		if runtime.GOOS == "windows" {
			return codex(home).StaleCopies()
		}
		return codexDaemonBehind()
	}

	if got := copies(); len(got) != 0 {
		t.Fatalf("never checked: %+v, want none", got)
	}
	codexDaemonCheck(cfg)
	if got := copies(); len(got) != 0 {
		t.Fatalf("a daemon on uni-switch's list: %+v, want none", got)
	}
	answer = provider.CodexDaemonCheck{Behind: true, Attached: 1, Since: since}
	codexDaemonCheck(cfg)
	if got := copies(); len(got) != 1 || got[0].Kind != "daemon" || !got[0].Since.Equal(since) {
		t.Fatalf("a daemon behind: %+v, want the daemon since %v", got, since)
	}
	// restarted by the user from the row
	CodexDaemonRestarted()
	if got := copies(); len(got) != 0 {
		t.Fatalf("restarted: %+v, want none", got)
	}
	// restarted by uni-switch, no session on it
	answer = provider.CodexDaemonCheck{Behind: true, Restarted: true, Since: since}
	codexDaemonCheck(cfg)
	if got := copies(); len(got) != 0 {
		t.Fatalf("restarted by uni-switch: %+v, want none", got)
	}
	// a check from longer ago than the loop looks says nothing now
	answer = provider.CodexDaemonCheck{Behind: true, Attached: 1, Since: since}
	codexDaemonCheck(cfg)
	codexDaemonSeen.Lock()
	codexDaemonSeen.at = time.Now().Add(-4 * codexDaemonEvery)
	codexDaemonSeen.Unlock()
	if got := copies(); len(got) != 0 {
		t.Fatalf("an old check: %+v, want none", got)
	}
}

// Qyburn3 on X: "为什么我每天好多次要重启" — the Codex row said 已接入 ·
// 退出并重开 Codex 应用后生效 several times a day for a Codex app already
// connected. Beside the ChatGPT sign-in uni-switch writes the app's list
// (uni-switch-desktop-models.json) with the backend's entries as it sends
// them, asked for again each hour, and every entry's details follow
// uni-switch's version and models.dev; each rewrite counted as a change to
// reopen for. Only a change in which models the lists hold (or where the
// config sends Codex) does; a stash an older uni-switch wrote over the same
// files is taken as it is.
func TestCodexStaleNotForAListsDetails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip(staleOnWindows)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CODEX_HOME", "")
	dir := filepath.Join(home, ".codex")
	os.MkdirAll(dir, 0o755)
	cfg := filepath.Join(dir, "config.toml")
	desk := filepath.Join(dir, "uni-switch-desktop-models.json")
	os.WriteFile(cfg, []byte("model = \"gpt-6-sol\"\nopenai_base_url = \"http://127.0.0.1:3425/backend-api/codex\"\nmodel_catalog_json = \""+tomlString(desk)+"\"\n\n"+
		"[model_providers.uni-switch]\nname = \"uni-switch\"\nbase_url = \"http://127.0.0.1:3425/v1\"\n"), 0o600)
	entry := func(slug, vis, instr string, prio int) string {
		return `{"slug":"` + slug + `","visibility":"` + vis + `","base_instructions":"` + instr + `","comp_hash":"` + instr + `","priority":` + strconv.Itoa(prio) + `}`
	}
	write := func(entries ...string) {
		os.WriteFile(desk, []byte(`{"models":[`+strings.Join(entries, ",")+`]}`), 0o644)
	}
	write(entry("gpt-6-sol", "list", "v1", 1), entry("codex-auto-review", "hide", "v1", 2), entry("ds/deepseek-v4-pro", "list", "uni-switch", 3))
	then := time.Now().Add(-2 * time.Hour)
	os.Chtimes(cfg, then, then)
	os.Chtimes(desk, then, then)

	was := running
	t.Cleanup(func() { running = was })
	running = func(string) []runningProc {
		// the Codex app, opened an hour ago
		return []runningProc{{time.Hour, "/Applications/Codex.app/Contents/Resources/codex app-server --listen stdio://"}}
	}
	a := codex(home)
	if n := a.Stale(); n != 0 {
		t.Fatalf("opened after the list was written: %d stale, want 0", n)
	}

	// the backend's entries come back with other details, in another
	// order, and uni-switch's entry is written with another priority
	write(entry("codex-auto-review", "hide", "v2", 1), entry("gpt-6-sol", "list", "v2", 2), entry("ds/deepseek-v4-pro", "list", "uni-switch 2", 4))
	if n := a.Stale(); n != 0 {
		t.Fatalf("the same models, other details: %d stale, want 0", n)
	}

	// a model uni-switch serves now: the app opened before lacks it
	write(entry("codex-auto-review", "hide", "v2", 1), entry("gpt-6-sol", "list", "v2", 2), entry("ds/deepseek-v4-pro", "list", "uni-switch 2", 4), entry("ds/deepseek-v5", "list", "uni-switch", 5))
	if n := a.Stale(); n != 1 {
		t.Fatalf("a model added: %d stale, want 1", n)
	}

	// an older uni-switch's stash, a digest of every byte, over the files as
	// they are: kept at its time, not a change first seen now
	old := sha256.Sum256([]byte(a.readsBefore()()))
	stash(map[string]string{"codex.started": hex.EncodeToString(old[:8]) + " " + strconv.FormatInt(then.UnixNano(), 10)})
	if n := a.Stale(); n != 0 {
		t.Fatalf("an older uni-switch's stash, nothing changed since: %d stale, want 0", n)
	}
	if n := codex(home).Stale(); n != 0 {
		t.Fatalf("asked again: %d stale, want 0", n)
	}
}
