package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// tongzhenbao on X: an image to Volcengine Ark's Coding Plan took 100+s to
// the first token. The plan's /models lists none of its models, and
// models.dev's resellers disagree on doubao-seed-2.1-pro, so uni-switch took
// it for text-only and had another model describe every image before Ark
// was asked. Ark's own list says it sees: its image goes to it as sent, and
// the describer isn't asked. A plan model that is text-only
// (deepseek-v4-pro) still has its image described.
func TestArkPlanModelThatSeesIsSentTheImage(t *testing.T) {
	s, u := eyed(t)
	up := strings.TrimSuffix(mustFind(t, "probe").Chat, "/v1")
	if err := provider.Save(provider.Provider{ID: "volcengine", Name: "Volcengine Ark", Preset: "volcengine", Chat: up + "/api/coding/v3", Key: "ark-k",
		Models: []string{"doubao-seed-2.1-pro", "glm-5.3-flash", "deepseek-v4-pro"}}); err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"doubao-seed-2.1-pro", "glm-5.3-flash", "deepseek-v4-pro"} {
		body := `{"model":"volcengine/` + model + `","input":[{"role":"user","content":[{"type":"input_text","text":"read"},{"type":"input_image","image_url":"data:image/png;base64,aGVsbG8="}]}]}`
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", model, rec.Code, rec.Body.String())
		}
	}
	if got := u.sent("doubao-seed-2.1-pro"); len(got) != 1 || !strings.Contains(got[0], "aGVsbG8=") || strings.Contains(got[0], "HELLO") {
		t.Fatalf("doubao-seed-2.1-pro, which Ark says sees, was sent %v", got)
	}
	// XYenon: the plan docs say glm-5.3-flash takes images.
	if got := u.sent("glm-5.3-flash"); len(got) != 1 || !strings.Contains(got[0], "aGVsbG8=") || strings.Contains(got[0], "HELLO") {
		t.Fatalf("glm-5.3-flash, which Ark's docs say sees, was sent %v", got)
	}
	if got := u.sent("deepseek-v4-pro"); len(got) != 1 || strings.Contains(got[0], "aGVsbG8=") || !strings.Contains(got[0], "HELLO") {
		t.Fatalf("deepseek-v4-pro, which is text-only, was sent %v", got)
	}
	if n := len(u.sent("eye")); n != 1 {
		t.Fatalf("the describer was asked %d times, want once (for deepseek-v4-pro alone)", n)
	}
}
