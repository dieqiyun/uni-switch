package gateway

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// #1865: chatgpt.com's Codex images backend answers gpt-image-2,
// gpt-image-2.5 and a made-up name alike with the same image, so a ChatGPT
// account lists one GPT Image, said to be the vendor's pick. A gpt-image-2.5
// saved when uni-switch listed it is kept as saved, still draws there, and
// stands for that one entry. An API key's gpt-image-2.5, where the model
// field matters, is listed and kept as it is.
func TestCodexListsOneGPTImage(t *testing.T) {
	codexSignedIn(t)
	var mu sync.Mutex
	var bodies []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"created":1,"output_format":"png","data":[{"b64_json":"`+base64.StdEncoding.EncodeToString(pngBytes)+`"}]}`)
	}))
	defer up.Close()
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	defer func() { provider.CodexBase = was }()
	if err := provider.Save(provider.Provider{ID: "openai", Name: "OpenAI", Chat: up.URL + "/v1", Key: "key"}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("openai", up.URL+"/v1", []catalog.Model{{ID: "gpt-image-2", Draws: true}, {ID: "gpt-image-2.5", Draws: true}}); err != nil {
		t.Fatal(err)
	}
	listed := map[string][]string{}
	for _, p := range provider.All() {
		if !p.On() {
			continue
		}
		for _, m := range Drawers(p) {
			listed[p.ID] = append(listed[p.ID], m.ID)
		}
		if VendorPicksDrawer(p) != (p.ID == "codex") {
			t.Errorf("VendorPicksDrawer(%s) = %v", p.ID, VendorPicksDrawer(p))
		}
	}
	if strings.Join(listed["codex"], ",") != "gpt-image-2" {
		t.Fatalf("codex lists %v, want only gpt-image-2", listed["codex"])
	}
	if strings.Join(listed["openai"], ",") != "gpt-image-2,gpt-image-2.5" {
		t.Fatalf("an API key lists %v, want gpt-image-2 and gpt-image-2.5", listed["openai"])
	}
	for ref, want := range map[string]string{
		"codex/gpt-image-2.5":  "codex/gpt-image-2",
		"codex/gpt-image-2":    "codex/gpt-image-2",
		"openai/gpt-image-2.5": "openai/gpt-image-2.5",
		"codex/gpt-6.1-sol":    "codex/gpt-6.1-sol",
		"gone/gpt-image-2.5":   "gone/gpt-image-2.5",
		"off":                  "off",
		"":                     "",
	} {
		if got := ListedDrawer(ref); got != want {
			t.Errorf("ListedDrawer(%q) = %q, want %q", ref, got, want)
		}
	}
	// the saved pick is kept, isn't missing, and draws at chatgpt.com
	st := settings.Load()
	st.ImageGen = "codex/gpt-image-2.5"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	if m := DrawerMissing(); m != "" {
		t.Fatalf("DrawerMissing = %q", m)
	}
	if m, ok := drawer(); !ok || m != "codex/gpt-image-2.5" {
		t.Fatalf("drawer = %q %v", m, ok)
	}
	code, _, raw := postImages(t, New(), "/v1/images/generations", "application/json", `{"prompt":"a uni-switch"}`)
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 || !strings.Contains(bodies[0], `"model":"gpt-image-2.5"`) {
		t.Fatalf("asked %v", bodies)
	}
	if settings.Load().ImageGen != "codex/gpt-image-2.5" {
		t.Fatalf("the saved pick was rewritten: %q", settings.Load().ImageGen)
	}
}
