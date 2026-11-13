package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// forgetCodexLists starts the test with no backend list kept, and ends it
// with every ask of the backend it started done.
func forgetCodexLists(t *testing.T) {
	t.Helper()
	reset := func() {
		codexLists.asking.Wait()
		codexLists.Lock()
		codexLists.at = map[string]*codexListed{}
		codexLists.Unlock()
	}
	reset()
	t.Cleanup(reset)
}

// codexListAsk is one /models request as a Codex app-server sends it.
func codexListAsk(h http.Handler) (int, map[string]bool) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", CodexPath+"/models?client_version=0.164.0", nil)
	req.Header.Set("Authorization", "Bearer chatgpt-token")
	req.Header.Set("chatgpt-account-id", "acct-1")
	h.ServeHTTP(rec, req)
	var list struct {
		Models []map[string]any `json:"models"`
	}
	json.Unmarshal(rec.Body.Bytes(), &list)
	has := map[string]bool{}
	for _, m := range list.Models {
		s, _ := m["slug"].(string)
		has[s] = true
	}
	return rec.Code, has
}

// Twenty Codex app-servers asking for the model list at once, with a
// ChatGPT backend slower than uni-switch waits for it (#3734): the backend is
// asked once for all of them, and its list, though it came after they
// were answered, is what the next ones get, at once, with uni-switch's
// models. Before, each asked on its own and gave up at codexModelsWait,
// so a slow backend's list never came through.
func TestCodexModelListSharedWhenSlow(t *testing.T) {
	setup(t, provider.Chat, &fake{t: t})
	var asks atomic.Int32
	hold := make(chan struct{})
	var holdOnce sync.Once
	letGo := func() { holdOnce.Do(func() { close(hold) }) }
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) {
		asks.Add(1)
		select {
		case <-hold:
		case <-r.Context().Done():
			return
		}
		w.Write([]byte(`{"models":[{"slug":"gpt-live","priority":1}]}`))
	})
	t.Cleanup(letGo)
	home, _ := os.UserHomeDir()
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"),
		[]byte(`{"etag":"\"v1\"","models":[{"slug":"gpt-cached","priority":1,"base_instructions":"x"}]}`), 0o644)
	was := codexModelsWait
	codexModelsWait = 200 * time.Millisecond
	t.Cleanup(func() { codexModelsWait = was })
	h := New().Handler()

	// the backend held: every one answered from Codex's last list, the
	// backend asked once
	var wg sync.WaitGroup
	var bad atomic.Int32
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if code, has := codexListAsk(h); code != 200 || !has["fake/m1"] || !has["gpt-cached"] {
				bad.Add(1)
			}
		}()
	}
	wg.Wait()
	if bad.Load() != 0 {
		t.Fatalf("%d of 20 answers without uni-switch's models or Codex's last list", bad.Load())
	}
	if n := asks.Load(); n != 1 {
		t.Fatalf("the backend was asked %d times for 20 lists at once, want 1", n)
	}

	// it answers late; the ask was not given up with the first answers
	letGo()
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, has := codexListAsk(h)
		if has["gpt-live"] {
			if !has["fake/m1"] || has["gpt-cached"] {
				t.Fatalf("the backend's list without uni-switch's models, or beside the cached one: %v", has)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the backend's list, answered after uni-switch stopped waiting, never reached a caller")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// fresh, it is handed out again without asking
	n := asks.Load()
	for range 5 {
		if _, has := codexListAsk(h); !has["gpt-live"] || !has["fake/m1"] {
			t.Fatalf("a fresh list not handed out again: %v", has)
		}
	}
	if got := asks.Load() - n; got != 0 {
		t.Fatalf("the backend was asked %d times more within codexListFor", got)
	}
}

// Past codexListFor the last list is handed out at once while the backend
// is asked again, so a slow backend holds no answer once it has answered
// one; what it says then is what the callers after get.
func TestCodexModelListStaleAnsweredAtOnce(t *testing.T) {
	setup(t, provider.Chat, &fake{t: t})
	var asks atomic.Int32
	slow := make(chan struct{})
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) {
		if asks.Add(1) == 1 {
			w.Write([]byte(`{"models":[{"slug":"gpt-old","priority":1}]}`))
			return
		}
		select {
		case <-slow:
		case <-r.Context().Done():
			return
		}
		w.Write([]byte(`{"models":[{"slug":"gpt-new","priority":1}]}`))
	})
	t.Cleanup(func() { close(slow) })
	was, wasFor := codexModelsWait, codexListFor
	codexModelsWait, codexListFor = 3*time.Second, time.Nanosecond
	t.Cleanup(func() { codexModelsWait, codexListFor = was, wasFor })
	h := New().Handler()
	if _, has := codexListAsk(h); !has["gpt-old"] {
		t.Fatalf("first list %v", has)
	}
	// stale at once; the backend now holds its answer. Each answer comes
	// from the list it has, well inside the 3 s it would have waited
	start := time.Now()
	for range 3 {
		if _, has := codexListAsk(h); !has["gpt-old"] || !has["fake/m1"] {
			t.Fatalf("a stale list not handed out: %v", has)
		}
	}
	if d := time.Since(start); d >= codexModelsWait {
		t.Fatalf("three answers took %v with a list in hand: they waited on the backend", d)
	}
	// the ask they started reaches the backend, once
	for deadline := time.Now().Add(10 * time.Second); asks.Load() < 2 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	if n := asks.Load(); n != 2 {
		t.Fatalf("the backend was asked %d times, want 2: once, then once again for the stale list", n)
	}
}

// A sign-in the backend refuses is Codex's to see, and no list from
// before it is handed out in its place.
func TestCodexModelListRefusedSignInPassed(t *testing.T) {
	setup(t, provider.Chat, &fake{t: t})
	var asks atomic.Int32
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) {
		if asks.Add(1) == 1 {
			w.Write([]byte(`{"models":[{"slug":"gpt-old","priority":1}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		w.Write([]byte(`{"detail":"token expired"}`))
	})
	was := codexListFor
	codexListFor = time.Nanosecond
	t.Cleanup(func() { codexListFor = was })
	h := New().Handler()
	if code, has := codexListAsk(h); code != 200 || !has["gpt-old"] {
		t.Fatalf("first list %d %v", code, has)
	}
	// stale: handed out while the backend is asked again, which refuses
	codexListAsk(h)
	codexLists.asking.Wait()
	if code, _ := codexListAsk(h); code != 401 {
		t.Fatalf("after the backend refused the sign-in: %d, want its 401", code)
	}
}

// A backend that always answers after uni-switch stops waiting still gets its
// list to Codex: the ask goes on, and the next caller has it. Before,
// every ask was given up at codexModelsWait, so such a backend's list,
// and the version and tools its entries say, never came through (#3734).
func TestCodexModelListBackendAlwaysSlow(t *testing.T) {
	setup(t, provider.Chat, &fake{t: t})
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) {
		// three times what uni-switch waits, well inside codexListFetch
		select {
		case <-time.After(600 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
		w.Write([]byte(`{"models":[{"slug":"gpt-live","priority":1}]}`))
	})
	was := codexModelsWait
	codexModelsWait = 200 * time.Millisecond
	t.Cleanup(func() { codexModelsWait = was })
	h := New().Handler()
	for deadline := time.Now().Add(10 * time.Second); ; {
		if _, has := codexListAsk(h); has["gpt-live"] && has["fake/m1"] {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("a backend slower than uni-switch waits never got its list through")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
