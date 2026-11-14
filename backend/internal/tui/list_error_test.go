package tui

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A plugin's account showing its defaults alone says why on the
// Providers page, as the app's editor does (gnayiab on X: uni-switch's TUI in
// WSL had Cursor with Auto alone and nothing said).
func TestProvidersSayWhyAListFellBack(t *testing.T) {
	home(t)
	m := model{w: 160, h: 40}
	m.provs = []provider.Provider{{ID: "cursor", Name: "Cursor"}, {ID: "deepseek", Name: "DeepSeek"}}
	listError = func(p provider.Provider) string {
		if p.ID == "cursor" {
			return "fetch failed: unable to get local issuer certificate"
		}
		return ""
	}
	t.Cleanup(func() { listError = provider.Provider.ListError })
	var cursor, deepseek string
	for _, l := range strings.Split(m.viewProviders(), "\n") {
		switch {
		case strings.Contains(l, "Cursor"):
			cursor = l
		case strings.Contains(l, "DeepSeek"):
			deepseek = l
		}
	}
	if !strings.Contains(cursor, "couldn't list its models: fetch failed: unable to get local issuer certificate") {
		t.Fatalf("Cursor's row doesn't say why: %q", cursor)
	}
	if strings.Contains(deepseek, "couldn't list") {
		t.Fatalf("DeepSeek's row says a list failed: %q", deepseek)
	}
}
