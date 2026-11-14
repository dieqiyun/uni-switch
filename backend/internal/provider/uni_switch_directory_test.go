package provider

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestUniSwitchProviderDirectory(t *testing.T) {
	var relays []string
	for _, p := range Presets() {
		if p.Kind == "vendor" {
			t.Fatalf("removed vendor remains available: %s", p.ID)
		}
		if p.Kind == "partner" {
			t.Fatalf("commercial partner remains available: %s", p.ID)
		}
		if p.Kind == KindRelay {
			relays = append(relays, p.ID)
		}
	}
	if !reflect.DeepEqual(relays, []string{"dieqiyun"}) {
		t.Fatalf("relay directory = %v; want only dieqiyun", relays)
	}
	p, err := FromPreset("dieqiyun")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "蝶祈云API" || p.Website != "https://www.dieqiyun.top" || p.Chat != "https://cfapi.dieqiyun.top/v1" || p.Icon != "dieqiyun" {
		t.Fatalf("incorrect relay defaults: %+v", p)
	}
	if p.Responses != "https://cfapi.dieqiyun.top/v1" || p.Anthropic != "https://cfapi.dieqiyun.top" {
		t.Fatalf("incorrect native API bases: %+v", p)
	}
	for _, id := range []string{"openrouter", "opencode-go", "clinepass", "opencode-zen", "kilo", "commandcode", "together", "fireworks", "siliconflow", "nvidia", "modelscope", "aihubmix", "pipellm", "302ai", "cherryin", "yylx", "vercel-jev", "cloudflare-jev"} {
		if Preset(id) != nil {
			t.Errorf("removed relay %s can still be added from a preset", id)
		}
	}
	for _, id := range []string{"ollama", "lmstudio", OMLXPreset, MLXServePreset} {
		if Preset(id) == nil {
			t.Errorf("unrelated preset %s was removed", id)
		}
	}
}

func TestUniSwitchDieqiyunEndpoints(t *testing.T) {
	detectHome(t)
	pr := Preset("dieqiyun")
	if len(pr.Regions) != 3 {
		t.Fatalf("endpoint choices = %d; want 3 dialogue endpoints", len(pr.Regions))
	}
	for i, host := range []string{"cfapi", "haapi", "hw"} {
		t.Run(host, func(t *testing.T) {
			r := pr.Regions[i]
			root := "https://" + host + ".dieqiyun.top"
			if r.ID != host || r.Chat != root+"/v1" || r.Note == "" {
				t.Fatalf("incorrect endpoint choice: %+v", r)
			}
			if r.ImagesOnly {
				t.Fatalf("dialogue selector advertises an image-only endpoint: %s", host)
			}
			if r.Responses != root+"/v1" || r.Anthropic != root {
				t.Fatalf("incorrect protocol bases: %+v", r)
			}
			p, err := ParseImport("uni-switch://import?preset=dieqiyun&region=" + host)
			if err != nil {
				t.Fatal(err)
			}
			p.ID, p.Key = "dieqiyun-"+host, "sk-synthetic"
			if err := Save(p); err != nil {
				t.Fatal(err)
			}
			got, err := Find(p.ID)
			if err != nil || got.Chat != r.Chat || got.Responses != r.Responses || got.Anthropic != r.Anthropic {
				t.Fatalf("saved endpoint changed: %+v (%v)", got, err)
			}
			if probesBalance(p) {
				t.Fatal("alternate endpoint unexpectedly starts automatic balance probing")
			}
		})
	}
}

func TestUniSwitchVendorImportsStayCustom(t *testing.T) {
	for _, base := range []string{
		"https://api.openai.com/v1",
		"https://api.deepseek.com/v1",
		"https://api.longcat.chat/openai/v1",
		"https://resource.openai.azure.com/openai/v1",
	} {
		p, why := imported("My endpoint", "sk-synthetic", endpoints{chat: base}, []string{"test-model"})
		if why != "" || p.Preset != "" || p.Name != "My endpoint" || p.Chat != base {
			t.Errorf("custom import at %s: %+v (%s)", base, p, why)
		}
	}
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(`{"openai":{"type":"api","key":"sk-synthetic"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	items, err := readOpenCodeAuth(path)
	if err != nil || len(items) != 1 || items[0].Skip == "" || items[0].Provider.Chat != "" {
		t.Fatalf("key-only import invented a removed endpoint: %+v (%v)", items, err)
	}
}

func TestUniSwitchLegacyVendorConfiguration(t *testing.T) {
	detectHome(t)
	p := Provider{ID: "saved", Name: "Saved endpoint", Preset: "tencent-token-plan",
		Chat: "https://api.lkeap.cloud.tencent.com/plan/v3", Key: "sk-synthetic", Models: []string{"test-model"}}
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := Find(p.ID)
	if err != nil || got.Chat != p.Chat || got.Preset != p.Preset || got.Name != p.Name || got.Key != p.Key {
		t.Fatalf("stored settings changed during preset removal: %+v (%v)", got, err)
	}
	if Preset(got.Preset) != nil {
		t.Fatal("saved data revived a removed preset")
	}
}

func TestUniSwitchVendorPresetsRemoved(t *testing.T) {
	for _, id := range []string{
		"anthropic", "openai", "google", VertexPreset, "deepseek", "xai",
		"moonshot", "moonshot-cn", "kimi-code", "kimi-code-cn", "zhipu", "zai",
		"minimax", "minimax-cn", "stepfun", "stepfun-cn", "xiaomi", "longcat",
		"baidu-qianfan", "tencent-cloud", "huaweicloud", "volcengine",
		"qwen", "qwen-cn", "qwen-token-plan", "bailian-token-plan",
		"qwencloud-token-plan", "qwencloud", "mistral", "voyage", "groq",
		"bedrock", "bedrock-mantle", AzurePreset, "ollama-cloud", "nous",
		"typesafe", "bailian-decision", "qwen-decision", "volcengine-decision",
		"qianfan-token-plan", "tencent-token-plan", "tencent-tokenhub-cn", "tencent-tokenhub",
	} {
		t.Run(id, func(t *testing.T) {
			if Preset(id) != nil {
				t.Errorf("removed vendor %s still has a preset", id)
			}
			if _, err := FromPreset(id); err == nil {
				t.Error("removed vendor can still be constructed from a preset")
			}
			if _, err := ParseImport("uni-switch://import?preset=" + id + "&key=sk-synthetic"); err == nil {
				t.Error("import link can still restore a removed preset")
			}
		})
	}
}
