package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// Old settings files must not change, block or reroute a request after the
// configurable masking feature is removed. All credentials here are synthetic.
func TestRetiredMaskingSettingsDoNotChangeOutboundCount(t *testing.T) {
	f := &fake{ctype: "application/json", reply: `{"input_tokens":17}`}
	setup(t, provider.Anthropic, f)
	var st settings.Settings
	if err := json.Unmarshal([]byte(`{"redact":true,"redactPersonal":true,"redactWords":["Nightjar"],"redactModelOff":true,"redactActions":{"API_KEY":"block"},"redactRouteTo":"missing/model"}`), &st); err != nil {
		t.Fatal(err)
	}
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	const content = "Nightjar 13812345678 synthetic.user@example.test sk-proj-QAonly1234567890abcdefghijklmnopqrstuvwxyz"
	body := `{"model":"fake/m1","system":"` + content + `","messages":[{"role":"user","content":"` + content + `"}],"tools":[{"name":"lookup","description":"` + content + `","input_schema":{"type":"object"}}]}`
	code, reply := post(t, "/v1/messages/count_tokens", body)
	if code != 200 || reply != f.reply {
		t.Fatalf("legacy masking blocked the request: %d %s", code, reply)
	}
	if f.calls != 1 || strings.Count(string(f.got), content) != 3 {
		t.Fatalf("legacy masking changed outbound content: %s", f.got)
	}
	if _, err := os.Stat(filepath.Join(settings.Dir(), "redact.key")); !os.IsNotExist(err) {
		t.Fatalf("retired placeholder key created: %v", err)
	}
}
