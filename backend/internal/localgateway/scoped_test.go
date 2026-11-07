package localgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/state"
)

func TestScopedListenersOnlyServeTheirAppliedClient(t *testing.T) {
	var count atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if r.Header.Get("Authorization") != "Bearer synthetic-applied" {
			t.Error("draft credential used")
		}
		var body map[string]any
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil || r.URL.Path != "/v1/chat/completions" || body["model"] != "fixture" || !strings.Contains(agent.Canonical(body["messages"]), "hi") {
			t.Error("wrong upstream protocol, model or request", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"reply","choices":[{"message":{"role":"assistant","content":"scoped"},"finish_reason":"stop"}],"usage":{}}`)
	}))
	defer up.Close()
	dir := t.TempDir()
	db, e := state.Open(filepath.Join(dir, "uni-switch.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec("CREATE TABLE client_configs(id TEXT PRIMARY KEY,state TEXT)"); e != nil {
		t.Fatal(e)
	}
	p := agent.Provider{Summary: map[string]any{"id": "applied", "family": "codex", "model": "fixture", "baseUrl": up.URL, "authMode": "bearer", "codexOptions": map[string]any{"protocol": "openai", "models": []any{map[string]any{"id": "fixture", "enabled": true, "profileOverrides": map[string]any{"endpoints": map[string]any{"responses": false, "chatCompletions": true}}}}}}, APIKey: "synthetic-applied"}
	for _, id := range []string{"fx", "zed", "lody", "cursorlocal"} {
		if _, e = db.Exec("INSERT INTO client_configs VALUES(?,?)", id, agent.Canonical(map[string]any{"active": "applied", "provider": p})); e != nil {
			t.Fatal(e)
		}
	}
	t.Cleanup(func() { CloseAll(context.Background()) })
	master, e := Start(dir)
	if e != nil {
		t.Fatal(e)
	}
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/clients/cursorlocal/applied/v1/models", master.Port), nil)
	req.Header.Set("Authorization", "Bearer "+master.Token)
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	var catalog map[string]any
	e = json.NewDecoder(res.Body).Decode(&catalog)
	res.Body.Close()
	if e != nil || res.StatusCode != 200 || !strings.Contains(agent.Canonical(catalog), `"api_types":["chat_completions"]`) || !strings.Contains(agent.Canonical(catalog), `"supports_streaming":true`) {
		t.Fatal("Cursor discovery did not receive its native capability catalog", e)
	}
	fx, e := StartScoped(dir, "fx")
	if e != nil {
		t.Fatal(e)
	}
	zed, e := StartScoped(dir, "zed")
	if e != nil {
		t.Fatal(e)
	}
	lody, e := StartScoped(dir, "lody")
	if e != nil {
		t.Fatal(e)
	}
	if !scopedHealthy(fx, "fx") || scopedHealthy(fx, "zed") || scopedHealthy(Route{Port: fx.Port, Token: strings.Repeat("b", 64)}, "fx") {
		t.Fatal("health did not verify client identity and token")
	}
	// Simulate a second core discovering the already running listener.
	servers.Lock()
	saved := servers.values[dir+"|fx"]
	delete(servers.values, dir+"|fx")
	servers.Unlock()
	again, e := StartScoped(dir, "fx")
	servers.Lock()
	servers.values[dir+"|fx"] = saved
	servers.Unlock()
	if e != nil || again != fx {
		t.Fatal("could not reuse own listener", e)
	}
	call := func(route Route, path, auth, origin string, want int) {
		t.Helper()
		req, _ := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d%s", route.Port, path), strings.NewReader(`{"model":"fixture","messages":[{"role":"user","content":"hi"}],"max_tokens":16}`))
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
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("%s got %d want %d: %s", path, res.StatusCode, want, b)
		}
		if want == 200 && !strings.Contains(string(b), "scoped") {
			t.Fatalf("upstream text lost: %s", b)
		}
	}
	call(fx, "/v1/chat/completions", "", "", 200)
	call(fx, "/clients/zed/applied/v1/chat/completions", "", "", 404)
	call(fx, "/v1/chat/completions", "", "https://synthetic.invalid", 403)
	call(zed, "/v1/chat/completions", "", "", 401)
	call(zed, "/v1/chat/completions", "Bearer "+fx.Token, "", 401)
	call(zed, "/v1/chat/completions", "Bearer "+zed.Token, "", 200)
	call(lody, "/v1/chat/completions", "", "", 401)
	call(lody, "/v1/chat/completions", "Bearer magpie-lody", "", 200)
	// Drafts are irrelevant: the state row carries the applied provider.
	if _, e = db.Exec("UPDATE client_configs SET state=? WHERE id='fx'", `{"active":null}`); e != nil {
		t.Fatal(e)
	}
	call(fx, "/v1/chat/completions", "", "", 409)
	call(zed, "/v1/chat/completions", "Bearer "+zed.Token, "", 200)
	if count.Load() != 4 {
		t.Fatal("rejected request reached upstream", count.Load())
	}
}
