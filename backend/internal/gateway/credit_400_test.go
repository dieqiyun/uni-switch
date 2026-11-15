package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// superMagicBroke is SuperMagic's answer to an account whose points ran
// out, word for word as mycf_711's trace showed it (#4095): a 400, which
// failure reads as out of credit ("充值") while none of quotaWords is in it.
const superMagicBroke = "非常抱歉，当前对话需要调用大模型 / 付费工具，您的可用积分已不足。\n您可以点击右上角「用户头像」，通过「充值」通道补充积分，完成后即可继续对话。"

func superMagicBrokeBody() string {
	b, _ := json.Marshal(map[string]any{"error": map[string]any{"message": superMagicBroke}})
	return string(b)
}

// An error failure reads as out of credit is one another account may
// answer, whatever its 4xx: retryable and failure agree, and a shape
// refusal that also says to top up is the credit it says (#4095).
func TestCreditRefusalIsRetryableAtAnyStatus(t *testing.T) {
	for _, body := range []string{
		superMagicBrokeBody(),
		"超级麦吉: " + superMagicBroke, // as uni-switch tells it on
	} {
		for _, status := range []int{400, 409, 422} {
			if f := failure(status, []byte(body)); f != failCredit {
				t.Fatalf("%d: failure %q, want credit", status, f)
			}
			if !retryable(status, []byte(body)) {
				t.Errorf("%d %s: not retryable, though failure reads it as out of credit", status, body)
			}
		}
	}
	if shapeRefused(400, []byte(`{"error":{"message":"Unknown parameter: 'x'. 请充值后重试"}}`)) {
		t.Errorf("a shape refusal that says to top up rests nobody")
	}
	// what isn't credit is as before
	if retryable(400, []byte(`{"error":{"message":"messages: field required"}}`)) {
		t.Errorf("a plain bad request is retryable")
	}
}

// mycf_711 (#4095): of three SuperMagic accounts, the one asked answered
// 400 「可用积分已不足」 and that went to the agent, though another had
// 6,467 points. The next account is asked and answers, and the empty one
// rests as out of credit, so the next request goes past it.
func TestCredit400MovesToTheNextAccount(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	restingUntil.Lock()
	restingUntil.m, restingUntil.note = map[string]time.Time{}, map[string]Rest{}
	restingUntil.Unlock()
	routed.Lock()
	routed.failures = map[string]int{}
	routed.Unlock()
	var mu sync.Mutex
	var seen []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		mu.Lock()
		seen = append(seen, key)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if key == "k-empty" {
			w.WriteHeader(400)
			io.WriteString(w, superMagicBrokeBody())
			return
		}
		io.WriteString(w, `{"id":"from-`+key+`","choices":[]}`)
	}))
	defer up.Close()
	p := provider.Provider{ID: "magicrew", Name: "超级麦吉", Chat: up.URL + "/v1", Models: []string{"m1"},
		Routing: provider.Ordered, Key: "k-empty", KeyName: "Empty", Keys: []provider.KeyAccount{{Name: "Full", Key: "k-full"}}}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	saved, err := provider.Find("magicrew")
	if err != nil {
		t.Fatal(err)
	}
	s := New()
	send := func() (int, string) {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions",
			strings.NewReader(`{"model":"magicrew/m1","messages":[{"role":"user","content":"hi"}]}`)))
		return rec.Code, rec.Body.String()
	}
	if code, body := send(); code != 200 || !strings.Contains(body, "from-k-full") {
		t.Fatalf("%d %s; tried %v", code, body, seen)
	}
	if strings.Join(seen, ",") != "k-empty,k-full" {
		t.Fatalf("tried %v", seen)
	}
	r, ok := RestOf(saved.RestKey(saved.KeyList()[0]))
	if !ok || r.Why != failCredit || r.Status != 400 {
		t.Fatalf("the empty account's rest: %+v %v", r, ok)
	}
	mu.Lock()
	seen = nil
	mu.Unlock()
	if code, body := send(); code != 200 || !strings.Contains(body, "from-k-full") || strings.Join(seen, ",") != "k-full" {
		t.Fatalf("next: %d %s, tried %v", code, body, seen)
	}
}
