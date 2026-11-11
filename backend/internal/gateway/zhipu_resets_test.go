package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Zhipu's 1308 and 1310, in the words its error code pages give (docs.z.ai
// api-code, docs.bigmodel.cn api-code). zaiPlayer is the refusal as Player
// quoted it on Discord (2026-10-15 08:25 UTC), its time to the minute.
const (
	zaiPlayer = "Usage limit reached for 5 hour. Your limit will reset at 2026-10-15 20:12"
	zai1308   = "Usage limit reached for 5 hour. Your limit will reset at 2026-10-15 20:12:13"
	zai1310   = "Weekly/Monthly Limit Exhausted. Your limit will reset at 2026-10-10 21:00:06"
	bigmodel  = "已达到 5 小时的使用上限。您的限额将在 2026-10-15 20:12:13 重置"
)

// A Zhipu reset time, written without a zone, is read as Beijing's, in the
// words of either site and either envelope; one cut short, with a zone of
// its own, garbled, past, too far off, or another vendor's words, is not
// read here.
func TestResetsZhipu(t *testing.T) {
	player := time.Date(2026, 10, 15, 8, 25, 0, 0, time.UTC) // 16:25 in Beijing
	for _, c := range []struct {
		name, body string
		now        time.Time
		want       time.Duration
	}{
		{"Player's words", zaiPlayer, player, 3*time.Hour + 47*time.Minute},
		{"Player's words, in Z.ai's OpenAI envelope", `{"error":{"code":"1308","message":"` + zaiPlayer + `"}}`, player, 3*time.Hour + 47*time.Minute},
		{"with seconds, in Z.ai's Anthropic envelope", `{"type":"error","error":{"type":"1308","message":"` + zai1308 + `"},"request_id":"x"}`, player, 3*time.Hour + 47*time.Minute + 13*time.Second},
		// as code-yeongyu/senpi#3348 saw it: 39 seconds as UTC+8, 8 hours as UTC
		{"senpi#3348", "Usage limit reached for 5 hour. Your limit will reset at 2026-09-22 01:18:11", time.Date(2026, 9, 21, 17, 17, 32, 0, time.UTC), 39 * time.Second},
		// as kodustech/kodus-ai#2104 saw it, at 11:32:34 UTC
		{"1310", `{"error":{"code":"1310","message":"` + zai1310 + `"}}`, time.Date(2026, 10, 9, 11, 32, 34, 0, time.UTC), 25*time.Hour + 27*time.Minute + 32*time.Second},
		{"bigmodel.cn", `{"error":{"code":"1308","message":"` + bigmodel + `"}}`, player, 3*time.Hour + 47*time.Minute + 13*time.Second},
		{"bigmodel.cn escaped", escaped(bigmodel, "1308"), player, 3*time.Hour + 47*time.Minute + 13*time.Second},
		// a Remote uni-switch's held refusal, read by the uni-switch asking it
		{"held by a Remote uni-switch", "Z.ai: " + zai1308 + " (out of its allowance: not asked again before 20:12 UTC+8)", player, 3*time.Hour + 47*time.Minute + 13*time.Second},
		// cut short
		{"cut in the seconds", zai1308[:len(zai1308)-1], player, 0},
		{"cut after the minute's colon", zai1308[:len(zai1308)-2], player, 0},
		{"cut in the minute", zaiPlayer[:len(zaiPlayer)-1], player, 0},
		{"cut in the date", zaiPlayer[:strings.Index(zaiPlayer, " 20:12")], player, 0},
		// a zone of its own is resetsWritten's to read, never taken as Beijing's
		{"its own UTC", zai1308 + " UTC", player, 0},
		{"its own offset", zai1308 + "+00:00", player, 0},
		{"its own Z", zai1308 + "Z", player, 0},
		// garbled, past, too far off
		{"month 13", strings.Replace(zai1308, "2026-10-15", "2026-13-15", 1), player, 0},
		{"hour 25", strings.Replace(zai1308, "20:12:13", "25:12:13", 1), player, 0},
		{"past", zai1308, time.Date(2026, 10, 15, 12, 13, 0, 0, time.UTC), 0},
		{"over a week off", zai1308, time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), 0},
		// 1310 is a week's or a month's: a month's reset is believed
		// weeks off, as Ark's is (#3276)
		{"1310, 9 days off", zai1310, time.Date(2026, 10, 1, 13, 0, 6, 0, time.UTC), 9 * 24 * time.Hour},
		{"1310, 36 days off", zai1310, time.Date(2026, 9, 4, 13, 0, 6, 0, time.UTC), 0},
		// another vendor's words, without a zone: not taken as Beijing's
		{"WorkBuddy without its offset", strings.Replace(wbZh, " UTC+8", "", 1), player, 0},
		{"a date in passing", "this model is retired at 2026-10-15 20:12:13", player, 0},
	} {
		if got := resetsZhipu([]byte(c.body), c.now); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
		if c.want > 0 {
			if got := resetsAt([]byte(c.body), c.now); got != c.want {
				t.Errorf("%s: resetsAt %v, want %v", c.name, got, c.want)
			}
		}
	}
}

// heldClock gives the time with its offset, so a reader on another clock
// can tell whose it is. Each zone is passed in, never set on time.Local:
// time.Now reads time.Local, and a goroutine an earlier test left running
// read it while this test wrote it, a race on ubuntu CI (run 39242954115).
func TestHeldClock(t *testing.T) {
	cst := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 15, 16, 25, 0, 0, cst)
	for _, c := range []struct {
		zone *time.Location
		at   time.Time
		want string
	}{
		{cst, time.Date(2026, 10, 15, 20, 12, 13, 0, cst), "20:12 UTC+8"},
		{cst, time.Date(2026, 10, 16, 9, 0, 0, 0, cst), "Oct 16 09:00 UTC+8"},
		{time.UTC, time.Date(2026, 10, 15, 20, 12, 13, 0, cst), "12:12 UTC+0"},
		{time.FixedZone("", -(3*3600 + 30*60)), time.Date(2026, 10, 15, 20, 12, 13, 0, cst), "08:42 UTC-3:30"},
	} {
		got := heldClockIn(c.at, now, c.zone)
		if got != c.want {
			t.Errorf("%v in %v: %q, want %q", c.at, c.zone, got, c.want)
		}
	}
}

// Player on Discord: a Z.ai key refused with 1308, which names 20:12 in
// Beijing's time, rested a quarter of an hour, and the refusal given again
// said "not asked again before 17:12", a time of no zone the vendor never
// named. It rests until the time Z.ai named, and says it with its offset.
func TestZhipuRestsUntilItsReset(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	restingUntil.Lock()
	restingUntil.m, restingUntil.note = map[string]time.Time{}, map[string]Rest{}
	restingUntil.Unlock()
	t.Cleanup(func() {
		restingUntil.Lock()
		restingUntil.m, restingUntil.note = map[string]time.Time{}, map[string]Rest{}
		restingUntil.Unlock()
	})
	// three hours on, to the second, as Z.ai writes it
	reset := time.Now().Add(3 * time.Hour).Truncate(time.Second)
	said := "Usage limit reached for 5 hour. Your limit will reset at " + reset.In(zhipuZone).Format("2006-01-02 15:04:05")
	asked := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		asked++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"code":"1308","message":"`+said+`"}}`)
	}))
	t.Cleanup(up.Close)
	p := provider.Provider{ID: "mockzai", Name: "Z.ai", Chat: up.URL + "/v1", Models: []string{"glm-5.2"}, Key: "k"}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	srv := New()
	post := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"mockzai/glm-5.2","messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Content-Type", "application/json")
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	want := int(time.Until(reset) / time.Second)
	for i := range 2 {
		rec := post()
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("request %d: %d %s", i, rec.Code, rec.Body)
		}
		if ra, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || ra < want-10 || ra > want+1 {
			t.Errorf("request %d: Retry-After %q, want about %d (Z.ai's own time)", i, rec.Header().Get("Retry-After"), want)
		}
		if i == 1 {
			if !strings.Contains(rec.Body.String(), "not asked again before "+heldClock(reset, time.Now())) {
				t.Errorf("held refusal doesn't name Z.ai's time with its offset: %s", rec.Body)
			}
		}
	}
	if asked != 1 {
		t.Errorf("Z.ai asked %d times, want once: the second is held until its reset", asked)
	}
}
