//go:build !windows

package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/gui"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A routing group or a provider saved answers before the agents' model
// lists are rewritten. Every save ends in catalog.Changed, which rewrote
// each agent's files inside the request; on Windows each file an agent
// holds open cost half a second of retried renames, and a save took
// seconds now and then while an agent was at work (CyberCaius on Discord:
// 路由组和供应商编辑保存有时候保存会卡很久才保存成功).
//
// The slow sync is made here by Pi's models.json being a FIFO: the sync's
// read of it waits until the test opens the other end, as a write over a
// held file waits. The save must answer while it waits, and the sync must
// still run after.
func TestSaveAnswersBeforeTheAgentsSync(t *testing.T) {
	home := t.TempDir()
	for k, v := range map[string]string{"HOME": home, "USERPROFILE": home,
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"), "XDG_CACHE_HOME": filepath.Join(home, ".cache"),
		"XDG_DATA_HOME": filepath.Join(home, ".local", "share"), "PI_CODING_AGENT_DIR": ""} {
		t.Setenv(k, v)
	}
	was := catalog.Changed
	catalog.Changed = nil
	t.Cleanup(func() {
		agent.SyncCatalogSettled()
		catalog.Changed = was
	})
	if err := provider.Save(provider.Provider{ID: "a", Name: "a", Key: "ka",
		Chat: "http://127.0.0.1:1/v1", Models: []string{"m1", "m2"}}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".pi", "agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pi runs a model of uni-switch's: connected, so a sync writes its list
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"defaultProvider":"uni-switch","defaultModel":"a/m1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Pi as uni-switch connected it (Connect's connect.was stash): a sync
	// writes only an agent connected in uni-switch
	if err := os.WriteFile(filepath.Join(filepath.Dir(provider.Path()), "stash.json"), []byte(`{"pi.connect.was":"{}"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "models.json")
	h := gui.Handler(nil, nil)
	post := func(path, body string) (int, string) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
		return rec.Code, rec.Body.String()
	}

	syncAgentsOnChange()
	for _, c := range []struct{ name, path, body string }{
		{"group", "/api/groups/save", `{"id":"g1","from":"g1","name":"G1","members":["a/m1"]}`},
		{"provider", "/api/provider/save", `{"id":"a","from":"a","name":"A","key":"ka","chat":"http://127.0.0.1:1/v1","models":["m1"]}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := syscall.Mkfifo(fifo, 0o600); err != nil {
				t.Fatal(err)
			}
			// release opens the other end of the sync's held read, so the
			// read gets {} and goes on. It waits up to 20s for a reader and
			// says whether one was there; it never blocks past that, so
			// nothing outlives the test, and the FIFO is gone after it.
			released := false
			release := func() bool {
				if released {
					return true
				}
				defer os.Remove(fifo)
				for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
					// ENXIO until a reader has it open
					w, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0)
					if err != nil {
						continue
					}
					// a later read finds no file, as before the hold
					os.Remove(fifo)
					w.WriteString(`{}`)
					w.Close()
					released = true
					return true
				}
				return false
			}
			defer release()

			type answer struct {
				code int
				body string
			}
			done := make(chan answer, 1)
			start := time.Now()
			go func() {
				code, body := post(c.path, c.body)
				done <- answer{code, body}
			}()
			// generous: a loaded machine answers a save in well under this,
			// while the held sync never ends until released
			select {
			case a := <-done:
				if a.code != http.StatusOK {
					t.Fatalf("save: %d %s", a.code, a.body)
				}
			case <-time.After(20 * time.Second):
				held := release()
				<-done
				t.Fatalf("the %s save waited %s (sync held: %v): it answers only after the agents' files are written",
					c.name, time.Since(start).Round(time.Millisecond), held)
			}
			// the sync did run, and is held on Pi's models.json
			if !release() {
				t.Fatalf("no sync ran after the %s save", c.name)
			}
			agent.SyncCatalogSettled()
		})
	}
}
