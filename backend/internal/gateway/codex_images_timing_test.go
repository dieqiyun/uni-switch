package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// computerUseShots are n distinct w×h JPEG screenshots as data URLs: a
// desktop with noise along its top, so each is as large as a real one,
// and a stamp that makes each its own.
func computerUseShots(n, w, h int) []string {
	base := shotImage(w, h)
	noise := rand.New(rand.NewPCG(3, 4))
	for i := 0; i < 200*base.Stride; i++ {
		if i%4 != 3 {
			base.Pix[i] = byte(noise.Uint32())
		}
	}
	out := make([]string, n)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range n {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			m := image.NewRGBA(base.Rect)
			copy(m.Pix, base.Pix)
			for y := 300; y < 364; y++ {
				for x := 0; x < 64; x++ {
					o := y*m.Stride + (x+(i%40)*64)*4
					m.Pix[o], m.Pix[o+1], m.Pix[o+2] = byte(i), byte(i*7), byte(i*13)
				}
			}
			var b bytes.Buffer
			jpeg.Encode(&b, m, &jpeg.Options{Quality: 85})
			out[i] = "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
		}()
	}
	wg.Wait()
	return out
}

// computerUseTurn is a long Computer Use conversation as Codex sends it on
// the Responses API: each step a user or assistant message, a tool call
// and the tool's output with its screenshot.
func computerUseTurn(model string, shots []string) []byte {
	para := strings.Repeat("Clicked the button in the toolbar, waited for the sheet and read what it says. ", 30)
	var input []any
	for i, shot := range shots {
		id := fmt.Sprintf("call_%04d", i)
		input = append(input,
			map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": para}}},
			map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": para}}},
			map[string]any{"type": "function_call", "call_id": id, "name": "computer", "arguments": `{"action":"screenshot"}`},
			map[string]any{"type": "function_call_output", "call_id": id, "output": []any{
				map[string]any{"type": "input_text", "text": "screenshot taken"},
				map[string]any{"type": "input_image", "image_url": shot}}},
		)
	}
	input = append(input, map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "go on"}}})
	b, _ := json.Marshal(map[string]any{"model": model, "stream": true, "instructions": para, "input": input})
	return b
}

// turnTimer stands in for an upstream that answers at once, and notes when
// each request reached it: the time from the agent's request to then is
// uni-switch's own work on it, the upload to a local server aside.
type turnTimer struct {
	mu      sync.Mutex
	arrived time.Time
}

func (tt *turnTimer) handler(w http.ResponseWriter, r *http.Request) {
	tt.mu.Lock()
	tt.arrived = time.Now()
	tt.mu.Unlock()
	io.Copy(io.Discard, r.Body)
	w.Header().Set("Content-Type", "text/event-stream")
	io.WriteString(w, sse(
		`data: {"type":"response.created","response":{"id":"r1","model":"gpt-6.1-sol"}}`,
		`data: {"type":"response.output_text.delta","delta":"ok"}`,
		`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[],"usage":{"input_tokens":7,"output_tokens":2}}}`))
}

// turn sends body to s and says how long uni-switch took before the upstream
// had it, and in all. Its CPU profile samples carry the label turn=name,
// for pprof's -tagfocus: on a loaded machine the time a turn's work took
// says more than how long it waited.
func (tt *turnTimer) turn(t *testing.T, s *Server, body []byte, name string) (local, all time.Duration) {
	t.Helper()
	start := time.Now()
	rec := httptest.NewRecorder()
	pprof.Do(context.Background(), pprof.Labels("turn", name), func(context.Context) {
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(body)))
	})
	all = time.Since(start)
	if rec.Code != 200 {
		t.Fatalf("status %d: %.300s", rec.Code, rec.Body.String())
	}
	tt.mu.Lock()
	defer tt.mu.Unlock()
	return tt.arrived.Sub(start), all
}

// TestComputerUseTurnTiming times uni-switch's own work on a long Computer
// Use turn to a ChatGPT account, as fflake33's (Discord): the first turn,
// and the next, which sends every past screenshot again. Run it with
// UNI_SWITCH_TURN_TIMING=1 (and -cpuprofile/-memprofile to see where).
func TestComputerUseTurnTiming(t *testing.T) {
	if os.Getenv("UNI_SWITCH_TURN_TIMING") == "" {
		t.Skip("UNI_SWITCH_TURN_TIMING=1 times a long Computer Use turn")
	}
	codexSignedIn(t)
	tt := &turnTimer{}
	up := httptest.NewServer(http.HandlerFunc(tt.handler))
	t.Cleanup(up.Close)
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })

	n := 160
	fmt.Sscan(os.Getenv("UNI_SWITCH_TURN_SHOTS"), &n)
	start := time.Now()
	shots := computerUseShots(n+3, 2940, 1912)
	t.Logf("%d screenshots made in %v", n+3, time.Since(start))
	s := New()
	for turn := 1; turn <= 4; turn++ {
		// each turn adds a screenshot to those sent before
		body := computerUseTurn("codex/gpt-5.5", shots[:n+turn-1])
		local, all := tt.turn(t, s, body, fmt.Sprint(turn))
		t.Logf("turn %d: %d images, %d MB: uni-switch %v before the upstream had it, %v in all", turn, n+turn-1, len(body)>>20, local, all)
	}
}

// TestCopilotLongTurnTiming times uni-switch's own work on a long Codex turn
// with no images to a Copilot account (echo_ts, Discord). Run it with
// UNI_SWITCH_TURN_TIMING=1.
func TestCopilotLongTurnTiming(t *testing.T) {
	if os.Getenv("UNI_SWITCH_TURN_TIMING") == "" {
		t.Skip("UNI_SWITCH_TURN_TIMING=1 times a long Copilot turn")
	}
	fresh(t)
	cfg := os.Getenv("XDG_CONFIG_HOME")
	os.MkdirAll(filepath.Join(cfg, "github-copilot"), 0o755)
	os.WriteFile(filepath.Join(cfg, "github-copilot", "apps.json"), mustJSON(map[string]any{
		"github.com:Iv1.x": map[string]any{"user": "echo", "oauth_token": "gho_timing"},
	}), 0o600)
	tt := &turnTimer{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			io.WriteString(w, `{"data":[{"id":"gpt-6.1-sol","name":"GPT-6.1 Sol","model_picker_enabled":true,"policy":{"state":"enabled"},"supported_endpoints":["/responses"],"capabilities":{"type":"chat","supports":{"reasoning_effort":["low","medium","high"]}}}]}`)
			return
		}
		tt.handler(w, r)
	}))
	t.Cleanup(api.Close)
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			json.NewEncoder(w).Encode(map[string]any{"token": "sess", "expires_at": time.Now().Add(time.Hour).Unix(), "endpoints": map[string]string{"api": api.URL}})
			return
		}
		io.WriteString(w, `{"copilot_plan":"individual","access_type_sku":"monthly_subscriber_quota"}`)
	}))
	t.Cleanup(gh.Close)
	oldTok, oldUser := provider.CopilotTokenURL, provider.CopilotUserURL
	provider.CopilotTokenURL, provider.CopilotUserURL = gh.URL+"/token", gh.URL+"/user"
	t.Cleanup(func() { provider.CopilotTokenURL, provider.CopilotUserURL = oldTok, oldUser })
	p, err := provider.Find("copilot")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := New()
	for _, words := range []int{2_000, 30_000, 150_000} {
		body := largeConversation(provider.Responses, "copilot/gpt-6.1-sol", words)
		for turn := 1; turn <= 3; turn++ {
			local, all := tt.turn(t, s, body, fmt.Sprintf("%dk-%d", words/1000, turn))
			t.Logf("%d KB, turn %d: uni-switch %v before the upstream had it, %v in all", len(body)>>10, turn, local, all)
		}
	}
}
