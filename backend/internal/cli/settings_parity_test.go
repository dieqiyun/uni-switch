package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settingsapi"
)

// The window and the CLI do the same things (#4745). These tests hold the
// two together: every setting the Settings page saves is one `uni-switch
// settings` sets, and every /api route the window's pages call has a
// command, or is named below as not having one yet, with why. A route or a
// setting added on one side only fails here.

// guiSettingsKeys are the keys the window's pages send POST /api/settings:
// prefsKeep's (what every save sends) and each savePrefs({ ...keep, k: … })
// call's own, in app.js and the built-in plugins' pages.
func guiSettingsKeys(t *testing.T) []string {
	t.Helper()
	assets := filepath.Join("..", "gui", "assets")
	key := regexp.MustCompile(`[{,]\s*([A-Za-z_]\w*)\s*[:,}]`)
	var keys []string
	add := func(src string) {
		for _, m := range key.FindAllStringSubmatch(src, -1) {
			keys = append(keys, m[1])
		}
	}
	err := filepath.WalkDir(assets, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".js") || strings.Contains(p, "vendor") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		src := string(b)
		if i := strings.Index(src, "function prefsKeep(s) {"); i >= 0 {
			body := src[i:]
			body = body[strings.Index(body, "return {")+len("return"):]
			add(body[:balanced(body)])
		}
		for rest := src; ; {
			i := strings.Index(rest, "savePrefs({")
			if i < 0 {
				break
			}
			rest = rest[i+len("savePrefs("):]
			add(rest[:balanced(rest)])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(keys)
	keys = slices.Compact(keys)
	if len(keys) < 60 {
		t.Fatalf("read %d keys from the pages' saves: %v", len(keys), keys)
	}
	return keys
}

// balanced is how far the {…} that src starts with goes.
func balanced(src string) int {
	depth := 0
	for i, c := range src {
		switch c {
		case '{', '(', '[':
			depth++
		case '}', ')', ']':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(src)
}

// Every key the pages send POST /api/settings is one `uni-switch settings`
// sets, and every one it sets is one the pages send.
func TestSettingsCLISetsWhatTheWindowSaves(t *testing.T) {
	gui := guiSettingsKeys(t)
	// spread into a save's body, not keys of it
	notKeys := []string{"keep", "change"}
	for _, k := range gui {
		if !slices.Contains(notKeys, k) && !settingsapi.Known(k) {
			t.Errorf("the window saves %q, which `uni-switch settings` can't set: add it to settingsapi.Keys", k)
		}
	}
	for _, k := range settingsapi.Keys {
		if !slices.Contains(gui, k) {
			t.Errorf("`uni-switch settings` sets %q, which no page sends", k)
		}
	}
}

// What a route without a command is waiting for. Later stages of #4745
// take routes out of notYet into cliForRoute.
const (
	// W: the window's own, with nothing to do in a terminal: its size,
	// icons and fonts, opening a folder, what's new, arranging a list by
	// dragging, the pages' own data
	W = "the window's own"
	// S2: a setting with a route of its own (its own checks and
	// side effects), stage 2
	S2 = "stage 2: a setting saved by its own route"
	// P: providers, accounts, sign-ins and plugins past what the
	// provider, accounts and plugin commands do, stage 2
	P = "stage 2: providers, accounts and sign-ins"
	// L: the library past `uni-switch library`, stage 3
	L = "stage 3: the library"
	// U: usage, sessions and traces past `uni-switch
	// usage`, `quota` and `sessions`, stage 3
	U = "stage 3: usage, sessions and traces"
	// A: connecting agents and the rest, stage 3
	A = "stage 3: agents and the rest"
)

// cliForRoute is the command that does what a route does, as `uni-switch …`.
var cliForRoute = map[string]string{
	"GET /api/agent-models/{id}":           "uni-switch models",
	"GET /api/settings":                    "uni-switch settings list",
	"POST /api/settings":                   "uni-switch settings set",
	"POST /api/settings/account-discovery": "uni-switch accounts discovery",
	"POST /api/settings/search-api":        "uni-switch search",
	"POST /api/settings/search-key":        "uni-switch search",
	"POST /api/settings/search-count":      "uni-switch search count",
	"POST /api/settings/update-mirror":     "uni-switch update mirror",
	"POST /api/sync":                       "uni-switch sync",
	"GET /api/caller-keys":                 "uni-switch gateway-key list",
	"POST /api/caller-keys/{action}":       "uni-switch gateway-key",
	"GET /api/groups":                      "uni-switch groups",
	"POST /api/groups/{action}":            "uni-switch group",
	"GET /api/import/{id}":                 "uni-switch import",
	"GET /api/library":                     "uni-switch library",
	"POST /api/library/{what}/{action}":    "uni-switch library",
	"GET /api/login/usage":                 "uni-switch quota",
	"GET /api/providers":                   "uni-switch providers",
	"POST /api/provider/{action}":          "uni-switch provider",
	"GET /api/sessions/reads":              "uni-switch sessions read",
	"POST /api/sessions/reads":             "uni-switch sessions read",
	"GET /api/sessions":                    "uni-switch sessions",
	"POST /api/uninstall":                  "uni-switch uninstall",
	"GET /api/usage":                       "uni-switch usage",
	"GET /api/usage/quotas":                "uni-switch quota",
	"GET /api/usage/quotas/history":        "uni-switch quota history",
	"GET /api/usage/requests.csv":          "uni-switch usage --csv",
	"POST /api/usage/workbuddy-checkin":    "uni-switch accounts checkin",
	"GET /api/update":                      "uni-switch update check",
	"POST /api/update/check":               "uni-switch update check",
	"POST /api/settings/agent-compact":     "uni-switch model compact",
	"POST /api/update/install":             "uni-switch update",
}

var notYet = map[string]string{
	"POST /api/settings/account-warmup":          "Per-account schedules are managed in the account UI; CLI retains shared warm-up settings.",
	"POST /api/settings/account-checkin":         "Per-account switches are managed in the account UI; CLI retains shared check-in settings.",
	"POST /api/agent-fast":                       A,  // agent_models.go
	"POST /api/agent-models/{id}":                A,  // agent_models.go
	"GET /api/agents/cli":                        A,  // api.go
	"GET /api/agents/install":                    A,  // api.go
	"GET /api/agents/preview/{id}":               A,  // api.go
	"GET /api/cli":                               A,  // api.go
	"GET /api/drift":                             A,  // api.go
	"GET /api/notices":                           W,  // api.go
	"GET /api/omarchy":                           W,  // api.go
	"GET /api/quit":                              W,  // api.go: the window's Quit question
	"GET /api/state":                             W,  // api.go
	"POST /api/agents/{action}/{id}":             A,  // api.go
	"POST /api/agents/arrange":                   W,  // api.go
	"POST /api/agents/cli/{id}":                  A,  // api.go
	"POST /api/agents/disconnect-offline/{id}":   A,  // api.go
	"POST /api/agents/rescan":                    A,  // api.go
	"POST /api/agents/restart/{id}":              A,  // api.go
	"POST /api/agents/stage/{id}":                A,  // api.go
	"POST /api/cli":                              A,  // api.go
	"POST /api/cli-behind/quiet":                 W,  // api.go
	"POST /api/gateway/held/reveal":              W,  // api.go
	"POST /api/profile/{action}":                 A,  // api.go
	"POST /api/proxy/check":                      S2, // api.go
	"POST /api/set":                              S2, // api.go
	"POST /api/settings/bare-ids":                S2, // api.go
	"POST /api/settings/busy-load":               S2, // api.go
	"POST /api/settings/checkin-account":         S2, // api.go
	"POST /api/settings/claude-cache":            S2, // tune.go
	"POST /api/settings/codex-agents-v1":         S2, // api.go
	"POST /api/settings/codex-async-compact":     S2, // api.go
	"POST /api/settings/codex-auto-reset":        S2, // api.go
	"POST /api/settings/codex-auto-review":       S2, // api.go
	"POST /api/settings/codex-compact-held/drop": S2, // api.go
	"POST /api/settings/codex-credits":           S2, // api.go
	"POST /api/settings/codex-descriptions":      S2, // api.go
	"POST /api/settings/codex-predictions":       S2, // api.go
	"POST /api/settings/codex-reset-lead":        S2, // api.go
	"POST /api/settings/codex-titles":            S2, // api.go
	"POST /api/settings/codex-warm-at":           S2, // api.go
	"POST /api/settings/cors":                    S2, // api.go
	"POST /api/settings/desktop-longest":         S2, // api.go
	"POST /api/settings/disabled-models":         S2, // api.go
	"POST /api/settings/full-context":            S2, // api.go
	"POST /api/settings/github-token":            S2, // api.go
	"POST /api/settings/guide":                   W,  // api.go
	"POST /api/settings/log/reveal":              W,  // api.go
	"POST /api/settings/login":                   S2, // api.go
	"POST /api/settings/minimax-checkin":         S2, // api.go
	"POST /api/settings/panel-shortcut":          W,  // api.go
	"POST /api/settings/plain-names":             S2, // api.go
	"POST /api/settings/port":                    S2, // api.go
	"POST /api/settings/qoder-checkin":           S2, // api.go
	"POST /api/settings/quota-left":              S2, // api.go
	"POST /api/settings/quota-reads":             S2, // api.go
	"POST /api/settings/reveal":                  W,  // api.go
	"POST /api/settings/search-all":              S2, // api.go
	"POST /api/settings/search-keys":             S2, // api.go
	"POST /api/settings/search-quota":            S2, // api.go
	"POST /api/settings/text-size":               W,  // api.go
	"POST /api/settings/trae-checkin":            S2, // api.go
	"POST /api/settings/update-skip":             S2, // api.go
	"POST /api/settings/workbuddy-checkin":       S2, // api.go
	"POST /api/usage/arrange":                    W,  // api.go
	"POST /api/warm-now":                         A,  // api.go
	"POST /api/window/{action}":                  W,  // api.go
	"GET /api/archive":                           S2, // archive.go
	"GET /api/archive/file":                      S2, // archive.go
	"POST /api/archive/export":                   S2, // archive.go
	"POST /api/settings/archive":                 S2, // archive.go
	"GET /api/web-page":                          W,  // apppage.go
	"POST /api/web-page":                         W,  // apppage.go
	"GET /api/library/browser":                   L,  // browser.go
	"POST /api/library/browser/setup":            L,  // browser.go
	"POST /api/library/browser/drag":             L,  // browser.go
	"POST /api/library/browser/extension":        L,  // browser.go
	"GET /api/caller-keys/models":                P,  // caller_keys.go
	"GET /api/caller-keys/mcp":                   L,  // caller_keys.go: the servers `uni-switch gateway-key mcp` takes
	"POST /api/config/repair":                    A,  // config_repair.go
	"GET /api/context":                           U,  // context.go
	"GET /api/context/text":                      W,  // context.go
	"GET /api/context/reply":                     W,  // context_archive.go: the reply half of the same detail view
	"GET /api/dev/wait":                          W,  // dev_on.go
	"POST /api/dev/import":                       W,  // dev_on.go
	"GET /api/fonts":                             W,  // fonts.go
	"POST /api/settings/gateway-mode":            S2, // gatewaymode.go
	"POST /api/gateway/restart":                  A,  // gateway_takeover.go
	"POST /api/gateway/take-over":                A,  // gateway_takeover.go
	"GET /api/importapps":                        P,  // importapps.go
	"GET /api/importapps/discovery":              P,  // importapps.go
	"POST /api/importapps":                       P,  // importapps.go
	"POST /api/import/icon":                      W,  // importlink.go
	"GET /api/library/icon":                      W,  // library.go
	"GET /api/library/market/servers":            L,  // library.go
	"GET /api/library/market/skills":             L,  // library.go
	"GET /api/library/mcp-signin/{id}":           L,  // library.go
	"GET /api/library/rtk":                       L,  // library.go
	"GET /api/library/skill":                     L,  // library.go
	"POST /api/library/market/about":             L,  // library.go
	"POST /api/library/mcp-signin":               L,  // library.go
	"POST /api/library/mcp-signin/{id}/cancel":   L,  // library.go
	"POST /api/library/mcp-signout":              L,  // library.go
	"POST /api/library/mcp/check":                L,  // library.go
	"POST /api/library/mcp/config":               L,  // library.go
	"POST /api/library/projects/choose":          W,  // library.go
	"POST /api/library/reveal":                   W,  // library.go
	"POST /api/library/rtk":                      L,  // library.go
	"POST /api/library/rtk/install":              L,  // library.go
	"POST /api/library/rtk/path":                 L,  // library.go
	"POST /api/library/rtk/upgrade":              L,  // library.go
	"POST /api/library/skills/check":             L,  // library.go
	"POST /api/library/skills/probe":             L,  // library.go
	"POST /api/library/skills/regroup":           W,  // library.go
	"GET /api/omarchy/widget":                    W,  // omarchy.go
	"POST /api/omarchy/widget":                   W,  // omarchy.go
	"GET /api/claude/mode":                       P,  // providers.go
	"GET /api/gateway/call":                      P,  // providers.go
	"GET /api/icons/{name}":                      W,  // providers.go
	"GET /api/lanes":                             P,  // providers.go
	"GET /api/signin/{id}":                       P,  // providers.go
	"GET /api/upstream":                          P,  // providers.go
	"POST /api/codex/app/reopen":                 W,  // providers.go
	"POST /api/codex/daemon/{action}":            P,  // providers.go
	"POST /api/copy":                             W,  // providers.go
	"POST /api/icons":                            W,  // providers.go
	"POST /api/icons/favicon":                    W,  // providers.go
	"POST /api/icons/url":                        W,  // providers.go
	"POST /api/keys/{action}":                    P,  // providers.go
	"POST /api/login/{action}":                   P,  // providers.go
	"POST /api/open":                             W,  // providers.go
	"POST /api/providers/arrange":                W,  // providers.go
	"POST /api/signin":                           P,  // providers.go
	"POST /api/signin/{id}/callback":             P,  // providers.go
	"POST /api/signin/{id}/cancel":               P,  // providers.go
	"POST /api/signin/claude-token":              P,  // providers.go
	"POST /api/signin/import":                    P,  // providers.go
	"POST /api/stepfun/{id}/session":             P,  // providers.go
	"POST /api/stepfun/{id}/signout":             P,  // providers.go
	"GET /api/sessions/manage":                   U,  // sessions_manage.go
	"GET /api/sessions/search":                   U,  // sessions_manage.go
	"POST /api/sessions/codex-provider":          U,  // sessions_manage.go
	"POST /api/sessions/delete":                  U,  // sessions_manage.go
	"POST /api/sessions/purge":                   U,  // sessions_manage.go
	"POST /api/sessions/recording":               U,  // sessions_manage.go
	"POST /api/sessions/relocate-claude":         U,  // sessions_manage.go
	"POST /api/sessions/restore":                 U,  // sessions_manage.go
	"GET /api/sessions/markdown":                 U,  // sessions.go
	"GET /api/sessions/one":                      U,  // sessions.go
	"GET /api/sessions/overview":                 U,  // sessions.go
	"GET /api/sessions/progress":                 U,  // sessions.go
	"GET /api/sessions/stats":                    U,  // sessions.go
	"GET /api/sessions/transcript":               U,  // sessions.go
	"POST /api/agents/open":                      W,  // sessions.go
	"POST /api/sessions/carry":                   U,  // sessions.go
	"POST /api/sessions/export":                  U,  // sessions.go
	"POST /api/sessions/terminal":                W,  // sessions.go
	"GET /api/model-loads":                       U,  // speeds.go
	"GET /api/model-speeds":                      U,  // speeds.go
	"GET /api/gateway/held/{name}":               U,  // trace.go
	"GET /api/gateway/history":                   U,  // trace.go
	"GET /api/gateway/route":                     U,  // trace.go
	"GET /api/gateway/trace":                     U,  // trace.go
	"POST /api/gateway/session-titles":           U,  // trace.go
	"POST /api/gateway/unrest":                   U,  // trace.go
	"GET /api/tune":                              U,  // tune.go
	"POST /api/tune/apply":                       U,  // tune.go
	"POST /api/tune/undo":                        U,  // tune.go
	"POST /api/uninstall/finish":                 A,  // uninstall.go
	"GET /api/usage/deleted/into":                U,  // usage.go
	"GET /api/usage/heatmap":                     U,  // usage.go
	"GET /api/usage/quotas/history/holds":        U,  // usage.go
	"GET /api/usage/requests":                    U,  // usage.go
	"GET /api/usage/requests/content":            U,  // usage.go
	"POST /api/usage/checkin-account":            U,  // usage.go: one account now; `uni-switch accounts checkin` does every account
	"POST /api/usage/clear":                      U,  // usage.go
	"POST /api/usage/codex-reset":                U,  // usage.go
	"POST /api/usage/deleted":                    U,  // usage.go
	"POST /api/usage/minimax-checkin":            U,  // usage.go
	"POST /api/usage/qoder-checkin":              U,  // usage.go
	"POST /api/usage/quotas/refresh":             U,  // usage.go
	"POST /api/usage/requests/export":            U,  // usage.go
	"POST /api/usage/trae-checkin":               U,  // usage.go
	"GET /api/update/notes":                      W,  // update.go
	"GET /api/whatsnew":                          W,  // whatsnew.go
	"POST /api/whatsnew/seen":                    W,  // whatsnew.go
	"POST /api/whatsnew/today":                   W,  // whatsnew.go
}

// guiRoutes are the /api routes internal/gui serves, as "METHOD /path".
func guiRoutes(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "gui", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`HandleFunc\("([A-Z]+ /api/[^"]*)"`)
	var routes []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			routes = append(routes, m[1])
		}
	}
	slices.Sort(routes)
	routes = slices.Compact(routes)
	if len(routes) < 200 {
		t.Fatalf("read %d routes from internal/gui", len(routes))
	}
	return routes
}

// Every route the window serves has a command, or is named in notYet;
// none is in both, and neither names a route that's gone. Every command
// named is one uni-switch has.
func TestEveryRouteHasACommandOrIsNamed(t *testing.T) {
	routes := guiRoutes(t)
	cmds := runCommands(t)
	for _, r := range routes {
		cmd, has := cliForRoute[r]
		_, later := notYet[r]
		switch {
		case has && later:
			t.Errorf("%s has a command (%s) and is in notYet: take it out of notYet", r, cmd)
		case !has && !later:
			t.Errorf("%s has no command: name the one that does what it does in cliForRoute, or say why there's none yet in notYet", r)
		}
	}
	for r, cmd := range cliForRoute {
		if !slices.Contains(routes, r) {
			t.Errorf("cliForRoute names %s, which internal/gui doesn't serve", r)
		}
		f := strings.Fields(cmd)
		if len(f) < 2 || f[0] != "uni-switch" || !slices.Contains(cmds, f[1]) {
			t.Errorf("%s: %q isn't a uni-switch command", r, cmd)
		}
	}
	for r := range notYet {
		if !slices.Contains(routes, r) {
			t.Errorf("notYet names %s, which internal/gui doesn't serve", r)
		}
	}
}
