package sessions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func codexIdentityRollout(t *testing.T, codex, filenameID string, lines ...string) string {
	t.Helper()
	path := filepath.Join(codex, "sessions", "2026", "09", "20", "rollout-2026-09-20T10-00-00-"+filenameID+".jsonl")
	writeLines(t, path, lines...)
	return path
}

func codexIdentityMeta(t *testing.T, id, session, source, parent, fork string) string {
	t.Helper()
	payload := map[string]any{"id": id, "session_id": session}
	if source != "" {
		payload["source"] = json.RawMessage(source)
	}
	if parent != "" {
		payload["parent_thread_id"] = parent
	}
	if fork != "" {
		payload["forked_from_id"] = fork
	}
	line, err := json.Marshal(map[string]any{"timestamp": stamp(0), "type": "session_meta", "payload": payload})
	if err != nil {
		t.Fatal(err)
	}
	return string(line)
}

func codexIdentityUsage(at int, responseID, session, thread string) string {
	return fmt.Sprintf(`{"timestamp":%q,"type":"token_usage_record","payload":{"response_id":%q,"session_id":%q,"thread_id":%q,"turn_id":"turn","usage":%s}}`,
		stamp(at), responseID, session, thread, cxUse(100, 20, 0, 10, 3))
}

func TestCodexIdentityParentChainsAndSummaryCache(t *testing.T) {
	d := setupCalls(t)
	spawn := func(parent string) string {
		return fmt.Sprintf(`{"subagent":{"thread_spawn":{"parent_thread_id":%q,"depth":2}}}`, parent)
	}
	rows := []struct {
		fileID, id, session, source, parent, fork string
	}{
		{"main-a", "main-a", "shared-a", `"vscode"`, "", ""},
		{"child-b", "child-b", "shared-a", spawn("main-a"), "", ""},
		{"nested-c", "nested-c", "shared-a", spawn("child-b"), "", ""},
		{"child-d", "child-d", "shared-a", spawn("main-a"), "", ""},
		{"fork-f", "fork-f", "fork-f", `"vscode"`, "", "main-a"},
		{"missing-parent", "missing-parent", "shared-a", spawn("absent-parent"), "", ""},
		{"missing-source-parent", "missing-source-parent", "shared-a", spawn(""), "", ""},
		{"top-parent", "top-parent", "shared-a", `{"subagent":"review"}`, "main-a", ""},
		{"conflicting-parent", "conflicting-parent", "shared-a", spawn("main-a"), "other-parent", ""},
		{"cycle-x", "cycle-x", "shared-a", spawn("cycle-y"), "", ""},
		{"cycle-y", "cycle-y", "shared-a", spawn("cycle-x"), "", ""},
		{"unknown-source", "unknown-source", "shared-a", `"other"`, "", ""},
	}
	for i, row := range rows {
		meta := codexIdentityMeta(t, row.id, row.session, row.source, row.parent, row.fork)
		codexIdentityRollout(t, d.codex, row.fileID, meta, codexIdentityUsage(i+1, "response-"+row.id, row.session, ""))
	}
	codexIdentityRollout(t, d.codex, "missing-meta", codexIdentityUsage(20, "response-missing-meta", "shared-a", ""))

	identities := CodexIdentities()
	wants := map[string]CodexIdentity{
		"main-a":                {ID: "main-a", Role: codexRoleMain, RootSession: "main-a"},
		"child-b":               {ID: "child-b", Role: codexRoleSubagent, ParentSession: "main-a", RootSession: "main-a"},
		"nested-c":              {ID: "nested-c", Role: codexRoleSubagent, ParentSession: "child-b", RootSession: "main-a"},
		"child-d":               {ID: "child-d", Role: codexRoleSubagent, ParentSession: "main-a", RootSession: "main-a"},
		"fork-f":                {ID: "fork-f", Role: codexRoleMain, RootSession: "fork-f"},
		"missing-parent":        {ID: "missing-parent", Role: codexRoleSubagent, ParentSession: "absent-parent"},
		"missing-source-parent": {ID: "missing-source-parent", Role: codexRoleSubagent},
		"top-parent":            {ID: "top-parent", Role: codexRoleSubagent, ParentSession: "main-a", RootSession: "main-a"},
		"conflicting-parent":    {ID: "conflicting-parent", Role: codexRoleUnknown},
		"cycle-x":               {ID: "cycle-x", Role: codexRoleSubagent, ParentSession: "cycle-y"},
		"cycle-y":               {ID: "cycle-y", Role: codexRoleSubagent, ParentSession: "cycle-x"},
		"missing-meta":          {ID: "missing-meta", Role: codexRoleUnknown},
		"unknown-source":        {ID: "unknown-source", Role: codexRoleUnknown},
	}
	for id, want := range wants {
		if got := identities[id]; got != want {
			t.Errorf("identity %s = %+v, want %+v", id, got, want)
		}
	}
	if sources, ids := CallSourcesAndCodexIdentities(); !reflect.DeepEqual(sources, CallSources()) || !reflect.DeepEqual(ids, CodexIdentities()) {
		t.Fatalf("one walk for both differs: %d sources, %d identities", len(sources), len(ids))
	}

	listed := map[string]Session{}
	for _, s := range List(All) {
		if s.Agent == "codex" {
			listed[s.ID] = s
		}
	}
	if got := listed["nested-c"]; got.ID != "nested-c" || got.Role != codexRoleSubagent || got.ParentSession != "child-b" || got.RootSession != "main-a" {
		t.Fatalf("listed nested thread: %+v", got)
	}
	if got := listed["fork-f"]; got.Role != codexRoleMain || got.RootSession != "fork-f" {
		t.Fatalf("fork was bound to its source: %+v", got)
	}
	if got, ok := Get("codex:nested-c"); !ok || got.Role != codexRoleSubagent || got.ParentSession != "child-b" || got.RootSession != "main-a" {
		t.Fatalf("Get nested thread: %+v, %t", got, ok)
	}
	managed := map[string]Managed{}
	for _, s := range ListAgent("codex") {
		managed[s.ID] = s
	}
	if got := managed["nested-c"]; got.Role != codexRoleSubagent || got.ParentSession != "child-b" || got.RootSession != "main-a" {
		t.Fatalf("managed nested thread: %+v", got)
	}

	stats := StatsAt(0, callT0.Add(24*time.Hour))
	for _, s := range stats.Sessions {
		if s.Agent == "codex" && s.ID == "nested-c" {
			if s.Role != codexRoleSubagent || s.ParentSession != "child-b" || s.RootSession != "main-a" {
				t.Fatalf("stats summary identity: %+v", s)
			}
			goto cache
		}
	}
	t.Fatal("nested Codex thread missing from stats summaries")

cache:
	before := CodexIdentities()
	Saved()
	Reset()
	if after := CodexIdentities(); !reflect.DeepEqual(before, after) {
		t.Fatalf("summary cache reload changed identities\n got: %+v\nwant: %+v", after, before)
	}
}

func TestCodexCallThreadIDPriorityAndCache(t *testing.T) {
	d := setupCalls(t)
	path := codexIdentityRollout(t, d.codex, "file-thread",
		codexIdentityMeta(t, "own-thread", "shared-session", `"vscode"`, "", ""),
		`{"timestamp":"`+stamp(1)+`","type":"turn_context","payload":{"turn_id":"turn","model":"gpt-6-astra","effort":"high"}}`,
		codexIdentityUsage(2, "response-explicit", "shared-session", "payload-thread"),
		codexIdentityUsage(3, "response-file", "shared-session", ""),
		codexIdentityUsage(4, "response-upgrade", "shared-session", ""),
		codexIdentityUsage(4, "response-upgrade", "shared-session", "payload-upgraded"),
		codexIdentityUsage(6, "response-conflict", "shared-session", "payload-first"),
		codexIdentityUsage(7, "response-conflict", "shared-session", "payload-conflict"),
	)
	want := map[string]string{
		"response-explicit": "payload-thread",
		"response-file":     "own-thread",
		"response-upgrade":  "payload-upgraded",
		"response-conflict": "",
	}
	check := func() {
		t.Helper()
		got := map[string]Call{}
		for _, c := range Calls(time.Time{}) {
			got[c.ResponseID] = c
		}
		if len(got) != len(want) {
			t.Fatalf("calls: %+v", got)
		}
		for response, thread := range want {
			c := got[response]
			if c.ThreadID != thread || c.Session != "shared-session" || c.Effort != "high" {
				t.Errorf("%s identity = %+v; want thread %s with shared session and effort", response, c, thread)
			}
		}
	}
	check()
	Reset()
	disk := loadCalls(file{path: path, agent: "codex"})
	if disk == nil || len(disk.Calls) != len(want) {
		t.Fatalf("call cache did not reload from disk: %+v", disk)
	}
	for _, c := range disk.Calls {
		if c.ThreadID != want[c.ResponseID] {
			t.Errorf("disk cached %s thread id %q, want %q", c.ResponseID, c.ThreadID, want[c.ResponseID])
		}
	}
	check()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestCodexIdentityParentMergeAcrossRolloutFiles(t *testing.T) {
	d := setupCalls(t)
	spawn := func(parent string) string {
		return fmt.Sprintf(`{"subagent":{"thread_spawn":{"parent_thread_id":%q}}}`, parent)
	}
	codexIdentityRollout(t, d.codex, "upgrade-first", codexIdentityMeta(t, "child-upgrade", "shared-child", `{"subagent":{"other":"review"}}`, "", ""))
	codexIdentityRollout(t, d.codex, "upgrade-later", codexIdentityMeta(t, "child-upgrade", "shared-child", spawn("main-root"), "", ""))
	codexIdentityRollout(t, d.codex, "main-root", codexIdentityMeta(t, "main-root", "shared-root", `"cli"`, "", ""))
	codexIdentityRollout(t, d.codex, "conflict-first", codexIdentityMeta(t, "child-conflict", "shared-conflict", spawn("main-root"), "", ""))
	codexIdentityRollout(t, d.codex, "conflict-later", codexIdentityMeta(t, "child-conflict", "shared-conflict", spawn("main-other"), "", ""))
	codexIdentityRollout(t, d.codex, "main-other", codexIdentityMeta(t, "main-other", "shared-other", `"exec"`, "", ""))

	identities := CodexIdentities()
	if got := identities["child-upgrade"]; got.Role != codexRoleSubagent || got.ParentSession != "main-root" || got.RootSession != "main-root" {
		t.Errorf("later parent did not upgrade the missing parent: %+v", got)
	}
	if got := identities["child-conflict"]; got.Role != codexRoleUnknown || got.ParentSession != "" || got.RootSession != "" {
		t.Errorf("conflicting parents across rollout files were guessed: %+v", got)
	}
}

func TestCodexIdentityFieldsOmitOtherAgents(t *testing.T) {
	cl := Session{Agent: "claude", ID: "claude-session"}
	encoded, err := json.Marshal(cl)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"role"`, `"parent_session"`, `"root_session"`, `"model_effort"`} {
		if strings.Contains(string(encoded), key) {
			t.Fatalf("non-Codex session has %s: %s", key, encoded)
		}
	}
	summary, err := json.Marshal(Summary{Agent: "claude", ID: "claude-session"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"role"`, `"parent_session"`, `"root_session"`, `"model_effort"`} {
		if strings.Contains(string(summary), key) {
			t.Fatalf("non-Codex summary has %s: %s", key, summary)
		}
	}
}

func TestCodexCompactionPromotesThreadID(t *testing.T) {
	usage := cxUsage{Input: 100, Output: 10, Cached: 20, Reasoning: 3, Known: 31}
	state := &codexUsageState{}
	if change := state.record(codexContribution{ResponseID: "response", Session: "shared", Model: "gpt-6-astra", At: callT0, Usage: usage}); change == nil {
		t.Fatal("initial response was not recorded")
	}
	change := state.compacted(callT0, "gpt-6-astra", cxCompaction{
		ResponseID: "response",
		Record:     &cxRecordedUsage{ThreadID: "payload-thread", SessionID: "shared", ResponseID: "response", Usage: &usage},
	})
	if change == nil || change.Value.ThreadID != "payload-thread" || len(state.Entries) != 1 || state.Entries[0].Usage != usage {
		t.Fatalf("compaction did not promote thread identity without duplicating usage: change=%+v entries=%+v", change, state.Entries)
	}
}

func TestCodexSourceNativeVariants(t *testing.T) {
	for _, tc := range []struct {
		source string
		role   string
		parent string
	}{
		{`"cli"`, codexRoleMain, ""},
		{`"vscode"`, codexRoleMain, ""},
		{`"exec"`, codexRoleMain, ""},
		{`"mcp"`, codexRoleMain, ""},
		{`{"custom":"codex"}`, codexRoleMain, ""},
		{`{"subagent":{"thread_spawn":{"parent_thread_id":"parent"}}}`, codexRoleSubagent, "parent"},
		{`{"subagent":{"other":"review"}}`, codexRoleSubagent, ""},
		{`"other"`, codexRoleUnknown, ""},
	} {
		role, parent := codexSource(json.RawMessage(tc.source))
		if role != tc.role || parent != tc.parent {
			t.Errorf("codexSource(%s) = %s/%s, want %s/%s", tc.source, role, parent, tc.role, tc.parent)
		}
	}
}

func TestCodexIdentityInvalidatesOldSummaryCache(t *testing.T) {
	d := setupCalls(t)
	path := codexIdentityRollout(t, d.codex, "old-cache-thread",
		codexIdentityMeta(t, "old-cache-thread", "shared-session", `"exec"`, "", ""),
	)
	writeCache(&save{cacheFile: cacheFile{
		Version: cacheVersion - 1,
		Zone:    zone(),
		Files: map[string]*state{path: {
			ID:   "old-cache-thread",
			Size: 1,
			Mod:  1,
		}},
	}, path: CachePath()})
	Reset()
	if got := CodexIdentities()["old-cache-thread"]; got.Role != codexRoleMain || got.RootSession != "old-cache-thread" {
		t.Fatalf("old summary cache hid current identity: %+v", got)
	}
}

// While no rollout changes, the identities are the ones built before, not
// built again; a rollout whose identity changed has them built afresh, the
// same as building them with nothing kept (#2278).
func TestCodexIdentitiesKeptWhileNoRolloutChanges(t *testing.T) {
	d := setupCalls(t)
	spawn := `{"subagent":{"thread_spawn":{"parent_thread_id":"main-k","depth":1}}}`
	codexIdentityRollout(t, d.codex, "main-k", codexIdentityMeta(t, "main-k", "s-k", `"vscode"`, "", ""), codexIdentityUsage(1, "r-main", "s-k", ""))
	child := codexIdentityRollout(t, d.codex, "child-k", codexIdentityMeta(t, "child-k", "s-k", `"vscode"`, "", ""), codexIdentityUsage(2, "r-child", "s-k", ""))
	built := func() map[string]CodexIdentity {
		mu.Lock()
		defer mu.Unlock()
		return codexIdentitiesBuilt(append(codexFiles(), wslFiles("codex")...))
	}
	same := func(a, b map[string]CodexIdentity) bool {
		return reflect.ValueOf(a).UnsafePointer() == reflect.ValueOf(b).UnsafePointer()
	}
	first := CodexIdentities()
	if got := first["child-k"]; got.Role != codexRoleMain {
		t.Fatalf("child-k before it is a subagent: %+v", got)
	}
	if again := CodexIdentities(); !same(first, again) {
		t.Fatal("the identities were built again with no rollout changed")
	}
	if !reflect.DeepEqual(first, built()) {
		t.Fatalf("kept identities differ from built ones\n got: %+v\nwant: %+v", first, built())
	}
	// the child's rollout is written again as a subagent of main-k
	writeLines(t, child, codexIdentityMeta(t, "child-k", "s-k", spawn, "", ""), codexIdentityUsage(2, "r-child", "s-k", ""), codexIdentityUsage(3, "r-child2", "s-k", ""))
	after := CodexIdentities()
	if same(first, after) {
		t.Fatal("a rollout changed and the identities kept from before were handed out")
	}
	if got := after["child-k"]; got.Role != codexRoleSubagent || got.RootSession != "main-k" {
		t.Fatalf("child-k after its rollout changed: %+v", got)
	}
	if !reflect.DeepEqual(after, built()) {
		t.Fatalf("identities after a change differ from built ones\n got: %+v\nwant: %+v", after, built())
	}
}
