package provider

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

// zcodeEncrypt stores a value as ZCode's credential store does.
func zcodeEncrypt(t *testing.T, v string) string {
	t.Helper()
	block, _ := aes.NewCipher(zcodeSecret())
	gcm, _ := cipher.NewGCM(block)
	iv := []byte("0123456789ab")
	sealed := gcm.Seal(nil, iv, []byte(v), nil)
	ct, tag := sealed[:len(sealed)-16], sealed[len(sealed)-16:]
	b := base64.RawURLEncoding.EncodeToString
	return "enc:v1:" + b(iv) + "." + b(tag) + "." + b(ct)
}

// zcodeTestJWT is a token shaped as ZCode's, running out at exp.
func zcodeTestJWT(exp time.Time) string {
	b := base64.RawURLEncoding.EncodeToString
	claims, _ := json.Marshal(map[string]any{"sub": "u1", "exp": exp.Unix()})
	return b([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + b(claims) + ".sig"
}

// ZCode 3.15 keeps no coding plan key in its credentials: a BigModel GLM
// Coding Pro's key sits in config.json, the built-in coding plan
// provider's options.apiKey, and the account reads as that plan, not as
// one with no plan and a Start Plan ended (#2130).
func TestZCodePlanKeyFromConfig(t *testing.T) {
	home := signIn(t)
	t.Setenv("ZCODE_CREDENTIAL_SECRET", "test-secret")
	dir := filepath.Join(home, ".zcode", "v2")
	writeFile(t, filepath.Join(dir, "credentials.json"), map[string]any{
		"oauth:bigmodel:user_info":    zcodeEncrypt(t, `{"user_id":"u1","email":"bm@example.com","name":"bm"}`),
		"oauth:bigmodel:access_token": zcodeEncrypt(t, "tok"),
		"zcodejwttoken":               zcodeEncrypt(t, "Bearer zc"),
	})
	// as the reporter's: OpenCode's layout, the key as it is
	writeFile(t, filepath.Join(dir, "config.json"), map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"provider": map[string]any{
			"builtin:bigmodel-coding-plan": map[string]any{
				"name":    "GLM Coding Plan",
				"options": map[string]any{"apiKey": "bm.secret", "baseURL": "https://open.bigmodel.cn/api/anthropic"},
				"models":  map[string]any{"glm-5.1": map[string]any{}},
			},
			"builtin:zai-coding-plan": map[string]any{"options": map[string]any{"apiKey": ""}},
		},
	})
	who, k, ok := zcodeOwn()
	if !ok || who != "bm@example.com" || k.Key != "bm.secret" || k.Base != ZCodeBigModelBase || k.JWT != "zc" {
		t.Fatalf("own: %v %q %+v", ok, who, k)
	}

	// a key written encrypted reads too; one that isn't a key doesn't
	writeFile(t, filepath.Join(dir, "config.json"), map[string]any{"provider": map[string]any{
		"builtin:zai-coding-plan":      map[string]any{"options": map[string]any{"apiKey": "{env:ZAI_KEY}"}},
		"builtin:bigmodel-coding-plan": map[string]any{"options": map[string]any{"apiKey": zcodeEncrypt(t, "bm.secret")}},
	}})
	if _, k, _ := zcodeOwn(); k.Key != "bm.secret" || k.Base != ZCodeBigModelBase {
		t.Fatalf("encrypted: %+v", k)
	}
	// a key in credentials.json still comes first
	writeFile(t, filepath.Join(dir, "credentials.json"), map[string]any{
		"account-provider:coding-plan:account:zai-individual-coding-plan:account:a:api-key": zcodeEncrypt(t, "own.secret"),
	})
	if _, k, _ := zcodeOwn(); k.Key != "own.secret" || k.Base != ZCodeZaiBase {
		t.Fatalf("credentials first: %+v", k)
	}
}
