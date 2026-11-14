package usage

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

func writeCodexIdentity(t *testing.T, id, session string, source any) string {
	t.Helper()
	payload := map[string]any{"id": id, "session_id": session}
	if source != nil {
		payload["source"] = source
	}
	line, err := json.Marshal(map[string]any{"timestamp": "2026-09-30T10:00:00Z", "type": "session_meta", "payload": payload})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sessions.CodexDir(), "sessions", "2026", "09", "30", "rollout-2026-09-30T10-00-00-"+id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(line, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeCodexCall(t *testing.T, path, id, session, source, responseID, model, effort string, at time.Time, input, output int) {
	t.Helper()
	var sourceJSON json.RawMessage
	if err := json.Unmarshal([]byte(source), &sourceJSON); err != nil {
		t.Fatal(err)
	}
	lines := []any{
		map[string]any{"timestamp": "2026-09-30T10:00:00Z", "type": "session_meta", "payload": map[string]any{"id": id, "session_id": session, "source": sourceJSON}},
		map[string]any{"timestamp": at.Add(-time.Second).Format(time.RFC3339Nano), "type": "turn_context", "payload": map[string]any{"turn_id": "turn-" + responseID, "model": model, "effort": effort}},
		map[string]any{"timestamp": at.Format(time.RFC3339Nano), "type": "token_usage_record", "payload": map[string]any{
			"response_id": responseID, "session_id": session, "thread_id": id, "turn_id": "turn-" + responseID,
			"usage": map[string]int{"input_tokens": input, "cached_input_tokens": 0, "cache_write_input_tokens": 0, "output_tokens": output, "reasoning_output_tokens": 0, "total_tokens": input + output},
		}},
	}
	var data []byte
	for _, line := range lines {
		b, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, b...)
		data = append(data, '\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func codexSpawn(parent string) map[string]any {
	return map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{"parent_thread_id": parent}}}
}

func findRowByResponse(rows []Row, responseID string) (Row, bool) {
	for _, row := range rows {
		if row.ResponseID == responseID {
			return row, true
		}
	}
	return Row{}, false
}

func findModelEffort(rows []sessions.ModelEffort, model, effort string) (sessions.ModelEffort, bool) {
	for _, row := range rows {
		if row.Model == model && row.Effort == effort {
			return row, true
		}
	}
	return sessions.ModelEffort{}, false
}

func TestCodexLedgerResponseIdentityAndOwnConsumption(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC))
	writeCodexIdentity(t, "main-a", "native-main", "vscode")
	writeCodexIdentity(t, "child-b", "native-main", codexSpawn("main-a"))
	writeCodexIdentity(t, "nested-c", "native-main", codexSpawn("child-b"))
	writeCodexIdentity(t, "other-child", "native-main", codexSpawn("main-a"))

	localAt := now.Add(-time.Hour)
	call := func(thread, response, effort string, input, output int) sessions.Call {
		return sessions.Call{Time: localAt, Agent: "codex", Session: "native-main", ThreadID: thread, ResponseID: response, Model: "m", Effort: effort,
			Tokens: sessions.Tokens{Input: input, Output: output}, RequestID: "native-" + response}
	}
	matchedLocal := call("child-b", "resp-match", "low", 80, 8)
	matchedLocal.Model = "native-model"
	duplicate := matchedLocal
	duplicate.Session = "inherited-session"
	nestedLocal := call("nested-c", "resp-nested", "low", 50, 5)
	nestedLocal.Model = "native-model"
	calls := []sessions.Call{
		call("main-a", "resp-main", "low", 20, 3),
		matchedLocal,
		duplicate,
		call("child-b", "resp-direct", "low", 40, 4),
		nestedLocal,
		call("child-b", "resp-conflict", "high", 70, 7),
		call("other-child", "resp-conflict", "high", 70, 7),
		{Time: localAt, Agent: "codex", Session: "native-main", ThreadID: "child-b", ResponseID: "resp-failed", Model: "m", Effort: "high", Error: "rate_limit", ErrorText: "rate limited"},
	}
	old := LogCalls
	LogCalls = func(time.Time) []sessions.Call { return calls }
	t.Cleanup(func() { LogCalls = old })

	appendGateway := func(response, session, nativeSession, kind, effort string, input, output int, rejected bool) {
		t.Helper()
		Append(Record{Time: now.Add(-5 * time.Minute), Agent: "codex", Provider: "openai", Model: "m", Effort: effort, Input: input, Output: output,
			Status: 200, Session: session, NativeSession: nativeSession, ResponseID: response, Kind: kind, Rejected: rejected})
	}
	appendGateway("resp-match", "old-session-A", "native-B", "collab_spawn", "high", 500, 50, false)
	appendGateway("resp-nested", "old-session-A", "native-C", "collab_spawn", "medium", 150, 15, false)
	appendGateway("resp-conflict", "main-a", "native-conflict", "collab_spawn", "high", 700, 70, false)
	appendGateway("resp-unpaired", "main-a", "native-unpaired", "collab_spawn", "medium", 900, 90, false)
	appendGateway("resp-rejected", "main-a", "native-rejected", "collab_spawn", "high", 10, 1, true)

	ledger := LedgerOf(All, Filter{})
	rows := ledger.Rows
	if row, ok := findRowByResponse(rows, "resp-match"); !ok || row.Source != "" || row.Model != "m" || row.Effort != "high" || row.Input != 500 || row.Output != 50 || !row.Priced || row.Session != "main-a" || row.Subagent != "child-b" || row.ParentAgent != "" || row.NativeSession != "native-B" || row.SessionRole != "subagent" {
		t.Fatalf("canonical direct child row: %+v, found=%t", row, ok)
	}
	if row, ok := findRowByResponse(rows, "resp-nested"); !ok || row.Model != "m" || row.SessionRole != "subagent" || row.Session != "main-a" || row.Subagent != "nested-c" || row.ParentAgent != "child-b" || row.Input != 150 || row.Effort != "medium" {
		t.Fatalf("nested child row: %+v, found=%t", row, ok)
	}
	if filtered := LedgerOf(All, Filter{Query: "nested-c"}).Rows; len(filtered) != 1 || filtered[0].ResponseID != "resp-nested" {
		t.Fatalf("ledger child-id filter = %+v", filtered)
	}
	parentFiltered := LedgerOf(All, Filter{Query: "child-b"}).Rows
	if len(parentFiltered) != 4 {
		t.Fatalf("ledger subagent and direct-parent filter = %+v", parentFiltered)
	}
	for _, id := range []string{"resp-conflict", "resp-unpaired", "resp-rejected"} {
		row, ok := findRowByResponse(rows, id)
		if !ok || row.SessionRole != "unknown" || row.Subagent != "" || row.ParentAgent != "" {
			t.Errorf("%s must have unknown identity: %+v, found=%t", id, row, ok)
		}
	}
	if row, ok := findRowByResponse(rows, "resp-unpaired"); !ok || row.Session != "main-a" || row.Kind != "collab_spawn" {
		t.Fatalf("unpaired shared-session row changed: %+v, found=%t", row, ok)
	}
	if row, ok := findRowByResponse(rows, "resp-main"); !ok || row.SessionRole != "main" || row.Session != "main-a" || row.Subagent != "" {
		t.Fatalf("main native row: %+v, found=%t", row, ok)
	}
	if row, ok := findRowByResponse(rows, "resp-failed"); !ok || row.SessionRole != "subagent" || !row.Failed() || row.Input+row.Output != 0 {
		t.Fatalf("zero-token failure row: %+v, found=%t", row, ok)
	}

	consumption := CodexModelEffort(All)
	if len(consumption) != 3 {
		t.Fatalf("actor groups = %+v; want only main, child, and nested child", consumption)
	}
	main, ok := findModelEffort(consumption["main-a"], "m", "low")
	if !ok || main.Input != 20 || main.Output != 3 {
		t.Errorf("main consumption = %+v, found=%t", main, ok)
	}
	childHigh, ok := findModelEffort(consumption["child-b"], "m", "high")
	matched, _ := findRowByResponse(rows, "resp-match")
	if !ok || childHigh.Input != 500 || childHigh.Output != 50 || !childHigh.Priced || math.Abs(childHigh.Cost-matched.Cost) > 1e-12 {
		t.Errorf("child gateway consumption = %+v, found=%t; gateway row=%+v", childHigh, ok, matched)
	}
	childLow, ok := findModelEffort(consumption["child-b"], "m", "low")
	if !ok || childLow.Input != 40 || childLow.Output != 4 {
		t.Errorf("child native consumption = %+v, found=%t", childLow, ok)
	}
	if got := len(consumption["child-b"]); got != 2 {
		t.Errorf("same model efforts were merged: %+v", consumption["child-b"])
	}
	nested, ok := findModelEffort(consumption["nested-c"], "m", "medium")
	if !ok || nested.Input != 150 || nested.Output != 15 {
		t.Errorf("nested consumption = %+v, found=%t", nested, ok)
	}
	if _, exists := consumption["other-child"]; exists {
		t.Errorf("conflicting actor was attributed to one child: %+v", consumption["other-child"])
	}
	direct := Direct(All)
	if direct.Calls != 3 || direct.Input != 60 || direct.Output != 7 {
		t.Errorf("direct summary retained gateway calls or copied history: %+v", direct.Totals)
	}
}

func TestCodexRequestPageFoldsCopiesBeforeResponseMatching(t *testing.T) {
	pageHome(t)
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	identities := map[string]sessions.CodexIdentity{
		"main-a":      {ID: "main-a", Role: "main", RootSession: "main-a"},
		"child-b":     {ID: "child-b", Role: "subagent", ParentSession: "main-a", RootSession: "main-a"},
		"other-child": {ID: "other-child", Role: "subagent", ParentSession: "main-a", RootSession: "main-a"},
		"nested-c":    {ID: "nested-c", Role: "subagent", ParentSession: "child-b", RootSession: "main-a"},
	}
	makePage := func(actors ...string) RequestPage {
		gateway := &rowChunk{}
		gateway.add(Row{Record: Record{Time: now.Add(-time.Minute), Agent: "codex", Provider: "openai", Model: "m", Effort: "high", Input: 500, Output: 50, Status: 200, ResponseID: "resp"}}, "", 1, false)
		local := &rowChunk{}
		for i, actor := range actors {
			local.add(Row{Record: Record{Time: now.Add(-time.Hour), Agent: "codex", Model: "native-model", Effort: "low", Input: 80, Output: 8,
				Session: "native-session-" + actor, ResponseID: "resp"}, Source: "log", codexActor: actor}, "", int64(i), false)
		}
		return buildRequestBlocksWithCodex(All, Filter{}, 0, 100, now, []*rowChunk{gateway}, []*rowChunk{local}, nil, nil, identities)
	}

	page := makePage("child-b", "child-b")
	if len(page.Rows) != 1 || page.Sum.Calls != 1 || page.Sum.Input != 500 || page.Rows[0].Model != "m" || page.Rows[0].Effort != "high" || page.Rows[0].SessionRole != "subagent" || page.Rows[0].Session != "main-a" || page.Rows[0].Subagent != "child-b" {
		t.Fatalf("identical native history copies were not folded into the gateway call: %+v", page)
	}
	page = makePage("child-b", "other-child")
	if len(page.Rows) != 1 || page.Sum.Calls != 1 || page.Sum.Input != 500 || page.Rows[0].SessionRole != "unknown" || page.Rows[0].Subagent != "" {
		t.Fatalf("conflicting native actors were guessed: %+v", page)
	}

	gateway, local := &rowChunk{}, &rowChunk{}
	gateway.add(Row{Record: Record{Time: now.Add(-time.Minute), Agent: "codex", Provider: "openai", Model: "m", Input: 300, Output: 30, Status: 200, ResponseID: "resp-parent"}}, "", 1, false)
	local.add(Row{Record: Record{Time: now.Add(-time.Hour), Agent: "codex", Model: "native-model", Input: 30, Output: 3,
		Session: "native-session", ResponseID: "resp-parent"}, Source: "log", codexActor: "nested-c"}, "", 1, false)
	page = buildRequestBlocksWithCodex(All, Filter{Query: "child-b"}, 0, 100, now, []*rowChunk{gateway}, []*rowChunk{local}, nil, nil, identities)
	if len(page.Rows) != 1 || page.Rows[0].ResponseID != "resp-parent" || page.Rows[0].ParentAgent != "child-b" {
		t.Fatalf("request-page parent-id filter = %+v", page.Rows)
	}
}

func TestCodexExactResponsePairPreemptsLegacyMatchInLedgerAndDirect(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC))
	writeCodexIdentity(t, "main-a", "shared-a", "vscode")
	writeCodexIdentity(t, "child-b", "shared-a", codexSpawn("main-a"))
	gatewayAt := now.Add(-5 * time.Minute)
	calls := []sessions.Call{
		{Time: gatewayAt.Add(time.Second), Agent: "codex", Session: "shared-a", ThreadID: "main-a", ResponseID: "resp-a", Model: "native-model", Effort: "low",
			Tokens: sessions.Tokens{Input: 300, Output: 30}},
		{Time: now.Add(-time.Hour), Agent: "codex", Session: "shared-b", ThreadID: "child-b", ResponseID: "resp-b", Model: "native-model", Effort: "low",
			Tokens: sessions.Tokens{Input: 100, Output: 10}},
	}
	old := LogCalls
	LogCalls = func(time.Time) []sessions.Call { return calls }
	t.Cleanup(func() { LogCalls = old })
	Append(Record{Time: gatewayAt, Agent: "codex", Provider: "openai", Model: "m", Effort: "high", Input: 300, Output: 30,
		Status: 200, Session: "shared-a", ResponseID: "resp-b"})

	rows := LedgerOf(All, Filter{}).Rows
	if len(rows) != 2 {
		t.Fatalf("ledger rows = %+v; want gateway B plus native A", rows)
	}
	if row, ok := findRowByResponse(rows, "resp-b"); !ok || row.Source != "" || row.SessionRole != "subagent" || row.Session != "main-a" || row.Subagent != "child-b" || row.Input != 300 {
		t.Fatalf("gateway B was not paired to child B: %+v, found=%t", row, ok)
	}
	if row, ok := findRowByResponse(rows, "resp-a"); !ok || row.Source != "log" || row.SessionRole != "main" || row.Session != "main-a" || row.Input != 300 {
		t.Fatalf("nearby main call A was swallowed: %+v, found=%t", row, ok)
	}
	consumption := CodexModelEffort(All)
	main, ok := findModelEffort(consumption["main-a"], "native-model", "low")
	if !ok || main.Input != 300 || main.Output != 30 {
		t.Fatalf("main call A consumption = %+v, found=%t", main, ok)
	}
	child, ok := findModelEffort(consumption["child-b"], "m", "high")
	if !ok || child.Input != 300 || child.Output != 30 {
		t.Fatalf("child B gateway consumption = %+v, found=%t", child, ok)
	}
	if direct := Direct(All); direct.Calls != 1 || direct.Input != 300 || direct.Output != 30 {
		t.Fatalf("direct summary lost main call A or kept gateway B: %+v", direct.Totals)
	}
}

func TestCodexQueryPageExactResponsePairPreemptsLegacyMatch(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC))
	gatewayAt := now.Add(-5 * time.Minute)
	mainPath := filepath.Join(sessions.CodexDir(), "sessions", "2026", "09", "30", "rollout-2026-09-30T10-00-00-main-a.jsonl")
	childPath := filepath.Join(sessions.CodexDir(), "sessions", "2026", "09", "30", "rollout-2026-09-30T10-00-00-child-b.jsonl")
	writeCodexCall(t, mainPath, "main-a", "shared-a", `"vscode"`, "resp-a", "native-model", "low", gatewayAt.Add(time.Second), 300, 30)
	writeCodexCall(t, childPath, "child-b", "shared-b", `{"subagent":{"thread_spawn":{"parent_thread_id":"main-a"}}}`, "resp-b", "native-model", "low", now.Add(-time.Hour), 100, 10)
	Append(Record{Time: gatewayAt, Agent: "codex", Provider: "openai", Model: "m", Effort: "high", Input: 300, Output: 30,
		Status: 200, Session: "shared-a", ResponseID: "resp-b"})

	page := QueryPage(All, Filter{}, 0, 100)
	if len(page.Rows) != 2 || page.Sum.Input != 600 || page.Sum.Output != 60 {
		t.Fatalf("query page lost a real call: %+v", page)
	}
	if row, ok := findRowByResponse(page.Rows, "resp-b"); !ok || row.Source != "" || row.SessionRole != "subagent" || row.Session != "main-a" || row.Subagent != "child-b" {
		t.Fatalf("query page paired gateway B incorrectly: %+v, found=%t", row, ok)
	}
	if row, ok := findRowByResponse(page.Rows, "resp-a"); !ok || row.Source != "log" || row.SessionRole != "main" || row.Session != "main-a" || row.Input != 300 {
		t.Fatalf("query page swallowed nearby main call A: %+v, found=%t", row, ok)
	}
}

func TestCodexRejectedGatewayDoesNotPairWithNativeCall(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC))
	writeCodexIdentity(t, "main-a", "shared-a", "vscode")
	writeCodexCall(t, filepath.Join(sessions.CodexDir(), "sessions", "2026", "09", "30", "rollout-2026-09-30T10-00-00-child-b.jsonl"),
		"child-b", "shared-b", `{"subagent":{"thread_spawn":{"parent_thread_id":"main-a"}}}`, "resp-rejected", "native-model", "low", now.Add(-time.Minute), 40, 4)
	Append(Record{Time: now.Add(-time.Minute), Agent: "codex", Provider: "openai", Model: "m", Effort: "high", Status: 429,
		Session: "shared-a", ResponseID: "resp-rejected", Rejected: true})

	page := QueryPage(All, Filter{}, 0, 100)
	if len(page.Rows) != 2 || page.Sum.Input != 40 || page.Sum.Output != 4 {
		t.Fatalf("rejected response pair consumed the native call: %+v", page)
	}
	rejected, native := Row{}, Row{}
	for _, row := range page.Rows {
		if row.Rejected {
			rejected = row
		} else if row.Source == "log" {
			native = row
		}
	}
	if !rejected.Rejected || rejected.SessionRole != "unknown" || rejected.Session != "shared-a" {
		t.Fatalf("rejected gateway row identity = %+v", rejected)
	}
	if native.ResponseID != "resp-rejected" || native.SessionRole != "subagent" || native.Subagent != "child-b" || native.Input != 40 {
		t.Fatalf("positive native call did not retain its own identity and tokens: %+v", native)
	}
	if got := CodexModelEffort(All)["child-b"]; len(got) != 1 || got[0].Model != "native-model" || got[0].Effort != "low" || got[0].Input != 40 || got[0].Output != 4 {
		t.Fatalf("rejected gateway contributed to actor consumption: %+v", got)
	}
	if direct := Direct(All); direct.Calls != 1 || direct.Input != 40 || direct.Output != 4 {
		t.Fatalf("rejected gateway hid native direct usage: %+v", direct.Totals)
	}
}

func TestCodexKnownSubagentWithUnknownRootKeepsOwnConsumption(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC))
	writeCodexCall(t, filepath.Join(sessions.CodexDir(), "sessions", "2026", "09", "30", "rollout-2026-09-30T10-00-00-child-orphan.jsonl"),
		"child-orphan", "shared-header", `{"subagent":{"thread_spawn":{"parent_thread_id":"missing-parent"}}}`, "resp-orphan", "native-model", "low", now.Add(-time.Minute), 40, 4)
	Append(Record{Time: now.Add(-30 * time.Second), Agent: "codex", Provider: "openai", Model: "m", Effort: "high", Input: 500, Output: 50,
		Status: 200, Session: "shared-header", NativeSession: "shared-header", ResponseID: "resp-orphan"})

	page := QueryPage(All, Filter{}, 0, 100)
	row, ok := findRowByResponse(page.Rows, "resp-orphan")
	if !ok || row.SessionRole != "subagent" || row.Session != "" || row.Subagent != "child-orphan" || row.ParentAgent != "" || row.NativeSession != "shared-header" || row.Input != 500 {
		t.Fatalf("subagent with an unconfirmed root was projected as main: %+v, found=%t", row, ok)
	}
	if got := CodexModelEffort(All)["child-orphan"]; len(got) != 1 || got[0].Model != "m" || got[0].Effort != "high" || got[0].Input != 500 || got[0].Output != 50 {
		t.Fatalf("known child consumption was lost with an unknown root: %+v", got)
	}
}

func TestCodexQueryPageRefreshesIdentityAndSharesJSONFields(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC))
	writeCodexIdentity(t, "main-a", "native-main", "vscode")
	childPath := filepath.Join(sessions.CodexDir(), "sessions", "2026", "09", "30", "rollout-2026-09-30T10-00-00-child-b.jsonl")
	writeCodexCall(t, childPath, "child-b", "native-main", `{"subagent":{"thread_spawn":{"parent_thread_id":"main-a"}}}`, "resp-cache", "native-model", "low", now.Add(-time.Minute), 40, 4)
	Append(Record{Time: now.Add(-30 * time.Second), Agent: "codex", Provider: "openai", Model: "m", Effort: "high", Input: 500, Output: 50,
		Status: 200, Session: "old-session", NativeSession: "native-header", ResponseID: "resp-cache", Kind: "collab_spawn"})

	page := QueryPage(All, Filter{}, 0, 100)
	if len(page.Rows) != 1 || page.Rows[0].Model != "m" || page.Rows[0].Effort != "high" || page.Rows[0].Input != 500 || page.Rows[0].SessionRole != "subagent" || page.Rows[0].Session != "main-a" || page.Rows[0].Subagent != "child-b" || page.Rows[0].NativeSession != "native-header" {
		t.Fatalf("initial cached request page: %+v", page.Rows)
	}
	encoded, err := json.Marshal(page.Rows[0])
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["response_id"] != "resp-cache" || wire["session_role"] != "subagent" {
		t.Fatalf("usage row JSON lacks identity fields: %s", encoded)
	}
	filtered := QueryPage(All, Filter{Query: "child-b"}, 0, 100)
	if len(filtered.Rows) != 1 || filtered.Rows[0].ResponseID != "resp-cache" {
		t.Fatalf("request-page child-id filter = %+v", filtered.Rows)
	}

	writeCodexCall(t, childPath, "child-b", "native-main", `"vscode"`, "resp-cache", "native-model", "low", now.Add(-time.Minute), 40, 4)
	if err := os.Chtimes(childPath, now, now); err != nil {
		t.Fatal(err)
	}
	refreshed := QueryPage(All, Filter{}, 0, 100)
	if len(refreshed.Rows) != 1 || refreshed.Rows[0].SessionRole != "main" || refreshed.Rows[0].Session != "child-b" || refreshed.Rows[0].Subagent != "" || refreshed.Rows[0].ParentAgent != "" {
		t.Fatalf("request page did not refresh changed Codex identity: %+v", refreshed.Rows)
	}
}

func TestLogRecordResponseIDRemainsCodexOnly(t *testing.T) {
	row := Row{Record: logRecord(sessions.Call{Agent: "claude", ResponseID: "claude-response"})}
	if row.ResponseID != "" || row.SessionRole != "" {
		t.Fatalf("non-Codex call gained Codex identity data: %+v", row)
	}
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if containsJSONField(encoded, "response_id") || containsJSONField(encoded, "session_role") {
		t.Fatalf("non-Codex row JSON changed: %s", encoded)
	}
	identities := map[string]sessions.CodexIdentity{"thread": {ID: "thread", Role: "main", RootSession: "thread"}}
	call := sessions.Call{Agent: "claude", ThreadID: "thread"}
	if got := codexIdentityRow(Row{Record: logRecord(call)}, codexActor(call), identities); got.SessionRole != "" || got.Subagent != "" {
		t.Fatalf("non-Codex ThreadID changed identity fields: %+v", got)
	}
}

func containsJSONField(data []byte, field string) bool {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return false
	}
	_, ok := object[field]
	return ok
}

func TestCodexIdentityFixturePath(t *testing.T) {
	pageHome(t)
	path := filepath.Join(sessions.CodexDir(), "sessions", "2026", "09", "30", "rollout-2026-09-30T10-00-00-main-a.jsonl")
	writeCodexCall(t, path, "main-a", "native-main", `"vscode"`, "resp-fixture", "m", "low", time.Date(2026, 9, 30, 11, 0, 0, 0, time.UTC), 1, 1)
	calls := sessions.Calls(time.Time{})
	if len(calls) != 1 || calls[0].ThreadID != "main-a" || calls[0].ResponseID != "resp-fixture" || calls[0].Model != "m" || calls[0].Effort != "low" {
		t.Fatalf("fixture did not produce a Codex call: %s (%d calls): %+v", path, len(calls), calls)
	}
	if got := logRecord(calls[0]).ResponseID; got != "resp-fixture" {
		t.Fatalf("local Codex response ID was lost from its usage row: %q", got)
	}
}

func TestCodexActorSurvivesPackedRequestChunk(t *testing.T) {
	identities := map[string]sessions.CodexIdentity{"child": {ID: "child", Role: "subagent", ParentSession: "main", RootSession: "main"}}
	chunk := &rowChunk{}
	for i := range 64 {
		chunk.add(Row{Record: Record{Agent: "codex", Model: "model", Session: "old", ResponseID: "response", Status: 200}, Source: "log", codexActor: "child"}, "", int64(i), false)
	}
	packed := chunk.pack()
	if len(packed.Archive) == 0 {
		t.Fatal("fixture did not use packed chunk storage")
	}
	decoded := packed.unpack()
	if decoded == nil {
		t.Fatal("packed chunk could not be decoded")
	}
	decoded.codexIdentities = identities
	row := decoded.row(0)
	if row.SessionRole != "subagent" || row.Session != "main" || row.Subagent != "child" {
		t.Fatalf("packed Codex actor identity was lost: %+v", row)
	}
	if chunk.codexActors.len() != len(chunk.Rows) || chunk.codexActors.at(0) != "child" {
		t.Fatal("query identity changed the source chunk")
	}
}

// The Sessions page asks for every Codex thread's consumption over all
// history each time it refreshes: the ledger is read again only when a call
// changed, and a changed rollout shows at once.
func TestCodexModelEffortReadsLedgerOnlyWhenCallsChange(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC))
	Append(Record{Time: now.Add(-2 * time.Hour), Agent: "claude-code", Provider: "openai", Model: "m", Input: 1, Output: 1, Status: 200})
	path := filepath.Join(sessions.CodexDir(), "sessions", "2026", "09", "30", "rollout-2026-09-30T10-00-00-main-a.jsonl")
	writeCodexCall(t, path, "main-a", "main-a", `"vscode"`, "resp-1", "m", "low", now.Add(-time.Hour), 20, 3)
	built := 0
	effortBuilt = func() { built++ }
	t.Cleanup(func() { effortBuilt = nil })

	first := CodexModelEffort(All)
	if got, ok := findModelEffort(first["main-a"], "m", "low"); !ok || got.Input != 20 {
		t.Fatalf("first read = %+v", first)
	}
	again := CodexModelEffort(All)
	if built != 1 {
		t.Fatalf("ledger read %d times for an unchanged history, want 1", built)
	}
	if got, _ := findModelEffort(again["main-a"], "m", "low"); got.Input != 20 {
		t.Fatalf("cached answer = %+v", again)
	}

	writeCodexCall(t, path, "main-a", "main-a", `"vscode"`, "resp-1", "m", "low", now.Add(-time.Hour), 2000, 30)
	later := now.Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	changed := CodexModelEffort(All)
	if got, _ := findModelEffort(changed["main-a"], "m", "low"); built != 2 || got.Input != 2000 {
		t.Fatalf("after the rollout changed: built %d, %+v", built, changed)
	}
	Append(Record{Time: now.Add(-time.Minute), Agent: "codex", Provider: "openai", Model: "m", Input: 5, Output: 1, Status: 200, ResponseID: "resp-2"})
	CodexModelEffort(All)
	if built != 3 {
		t.Fatalf("a new gateway call did not read the ledger again: built %d", built)
	}
}

// A new Codex thread, a subagent's too, keeps the chunks of the files read
// before (#3493): a thread's identity is given to its rows as the page is
// built, so a child read before its parent's file names the parent's
// session once that file is there, and nothing else is read again.
func TestCodexNewThreadKeepsReadChunks(t *testing.T) {
	pageHome(t)
	now := holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC))
	dir := filepath.Join(sessions.CodexDir(), "sessions", "2026", "09", "30")
	var read []string
	reader := func(s sessions.CallSource) []sessions.Call {
		read = append(read, filepath.Base(s.Path))
		return sessions.ReadCallSource(s)
	}
	writeCodexCall(t, filepath.Join(dir, "rollout-2026-09-30T10-00-00-child-b.jsonl"), "child-b", "shared-b", `{"subagent":{"thread_spawn":{"parent_thread_id":"main-a"}}}`, "resp-b", "m", "low", now.Add(-time.Hour), 100, 10)
	if p := queryPage(Today, Filter{}, 0, 100, reader); p.Total != 1 {
		t.Fatalf("%d rows", p.Total)
	}
	read = nil
	writeCodexCall(t, filepath.Join(dir, "rollout-2026-09-30T10-00-00-main-a.jsonl"), "main-a", "shared-a", `"vscode"`, "resp-a", "m", "low", now.Add(-2*time.Hour), 300, 30)
	p := queryPage(Today, Filter{}, 0, 100, reader)
	if len(read) != 1 || read[0] != "rollout-2026-09-30T10-00-00-main-a.jsonl" {
		t.Fatalf("a new thread read again %v", read)
	}
	if row, ok := findRowByResponse(p.Rows, "resp-b"); !ok || row.SessionRole != "subagent" || row.Session != "main-a" {
		t.Fatalf("the child's row doesn't name its parent's session: %+v, found=%t", row, ok)
	}
}
