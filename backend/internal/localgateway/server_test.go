package localgateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/state"
)

func TestGoListenerAuthenticatesAndUsesOnlyAppliedTargetSnapshot(t *testing.T) {
	var count atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if r.Header.Get("Authorization") != "Bearer synthetic-applied" {
			t.Error("used a draft credential")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"msg-fixture","type":"message","role":"assistant","model":"claude-fixture","content":[{"type":"text","text":"你好"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer upstream.Close()
	dir := t.TempDir()
	db, e := state.Open(filepath.Join(dir, "uni-switch.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	_, e = db.Exec("CREATE TABLE targets(id TEXT PRIMARY KEY,active TEXT);INSERT INTO targets VALUES('codex','applied')")
	if e != nil {
		t.Fatal(e)
	}
	p := agent.Provider{Summary: map[string]any{"id": "applied", "family": "codex", "model": "claude-fixture", "baseUrl": upstream.URL, "authMode": "bearer", "codexOptions": map[string]any{"protocol": "anthropic", "models": []any{}}}, APIKey: "synthetic-applied"}
	if e = edit.WriteAtomic(filepath.Join(dir, "bridge-active.json"), strptr(agent.Canonical(p))); e != nil {
		t.Fatal(e)
	}
	route, e := Start(dir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { CloseAll(context.Background()) })
	if !Healthy(route) {
		t.Fatal("Go listener unhealthy")
	}
	again, e := Start(dir)
	if e != nil || again != route {
		t.Fatal("listener startup not idempotent", e)
	}
	call := func(auth, origin string, want int) {
		t.Helper()
		req, e := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/v1/applied/responses", route.Port), strings.NewReader(`{"model":"claude-fixture","input":"hi","max_output_tokens":16}`))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("status %d want %d: %s", res.StatusCode, want, body)
		}
		if want == 200 && !strings.Contains(string(body), "你好") {
			t.Fatal(string(body))
		}
	}
	call("", "", 401)
	call("Bearer "+route.Token, "https://browser.invalid", 401)
	if count.Load() != 0 {
		t.Fatal("unauthorized request reached upstream")
	}
	call("Bearer "+route.Token, "", 200)
	_, _ = db.Exec("UPDATE targets SET active=NULL WHERE id='codex'")
	call("Bearer "+route.Token, "", 409)
	if count.Load() != 1 {
		t.Fatal("restored target still forwarded")
	}
}
