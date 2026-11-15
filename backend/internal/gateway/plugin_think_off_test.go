package gateway

import (
	"strings"

	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// TestOffAsNone: only an effort spelled "off" changes, to none; the rest
// of the body, numbers included, goes as it came, and another level or
// an "off" that isn't an effort is left alone (plugins#334).
func TestOffAsNone(t *testing.T) {
	for _, tt := range []struct {
		name     string
		from     provider.Protocol
		in, want string
	}{
		{"chat", provider.Chat, `{"model":"m","reasoning_effort":"off","max_tokens":12345678901}`, `{"max_tokens":12345678901,"model":"m","reasoning_effort":"none"}`},
		{"responses", provider.Responses, `{"model":"m","reasoning":{"effort":"Off","summary":"auto","max_tokens":4096}}`, `{"model":"m","reasoning":{"effort":"none","max_tokens":4096,"summary":"auto"}}`},
		{"chat reasoning object", provider.Chat, `{"model":"m","reasoning":{"effort":"off"}}`, `{"model":"m","reasoning":{"effort":"none"}}`},
		{"low", provider.Chat, `{"model":"m","reasoning_effort":"low"}`, `{"model":"m","reasoning_effort":"low"}`},
		{"offline", provider.Chat, `{"model":"m","reasoning_effort":"offline"}`, `{"model":"m","reasoning_effort":"offline"}`},
		{"off in text", provider.Chat, `{"model":"m","messages":[{"role":"user","content":"effort\": \"off\""}]}`, `{"model":"m","messages":[{"role":"user","content":"effort\": \"off\""}]}`},
		{"anthropic", provider.Anthropic, `{"model":"m","output_config":{"effort":"off"}}`, `{"model":"m","output_config":{"effort":"off"}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.TrimSpace(string(offAsNone(tt.from, []byte(tt.in))))
			if got != tt.want {
				t.Errorf("offAsNone(%s) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

// TestAskedAtOff: a model asked ":off" is asked at none, as ":none" is.
func TestAskedAtOff(t *testing.T) {
	for _, id := range []string{"group/g:off", "group/g:OFF"} {
		if m, e := askedAt(id); m != "group/g" || e != "none" {
			t.Errorf("askedAt(%q) = %q, %q; want group/g, none", id, m, e)
		}
	}
	if m, e := askedAt("group/g:offline"); m != "group/g:offline" || e != "" {
		t.Errorf("askedAt(group/g:offline) = %q, %q", m, e)
	}
}
