package edit

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// utf8BOM is what PowerShell's `Set-Content -Encoding UTF8` (5.1), Notepad
// and Claude Code channel switchers on Windows put in front of a file.
const utf8BOM = "\xef\xbb\xbf"

// claudeSettingsCRLF is a Windows ~/.claude/settings.json as such a tool
// leaves it: a BOM, CRLF line ends, two-space indent, the user's env (#1534).
const claudeSettingsCRLF = utf8BOM + "{\r\n" +
	"  \"env\": {\r\n" +
	"    \"ANTHROPIC_BASE_URL\": \"https://api.deepseek.com/anthropic\",\r\n" +
	"    \"ANTHROPIC_AUTH_TOKEN\": \"sk-user-own\",\r\n" +
	"    \"API_TIMEOUT_MS\": \"600000\"\r\n" +
	"  },\r\n" +
	"  \"permissions\": {\r\n" +
	"    \"allow\": [\r\n" +
	"      \"Bash(git status)\"\r\n" +
	"    ]\r\n" +
	"  },\r\n" +
	"  \"model\": \"deepseek-chat\",\r\n" +
	"  \"alwaysThinkingEnabled\": true\r\n" +
	"}\r\n"

// keptAsWritten fails unless the file at p starts with exactly one BOM,
// has only CRLF line ends, and holds every one of want.
func keptAsWritten(t *testing.T, p string, want ...string) string {
	t.Helper()
	got := read(t, p)
	if !strings.HasPrefix(got, utf8BOM) || strings.HasPrefix(got[len(utf8BOM):], utf8BOM) {
		t.Fatalf("the file's BOM was not kept once: %q", got)
	}
	if strings.Count(got, "\n") != strings.Count(got, "\r\n") {
		t.Fatalf("the file's CRLF line ends were not kept: %q", got)
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Fatalf("%q missing from %q", w, got)
		}
	}
	return got
}

// The JSON layer reads through the BOM and writes the file back with it,
// the user's own keys and line ends as they were. On the old code every
// write here failed with "invalid JSON/JSONC" and every read found nothing.
func TestJSONSettingsWithBOM(t *testing.T) {
	p := tmpFile(t, "settings.json", claudeSettingsCRLF)
	if v, ok := GetJSON(p, "model"); !ok || v != "deepseek-chat" {
		t.Fatalf("GetJSON through a BOM: %q %v", v, ok)
	}
	if err := SetJSON(p, KV{Path: "model", Value: "uni-switch/deepseek/deepseek-flash"}, KV{Path: "env.UNI_SWITCH", Value: "1"}); err != nil {
		t.Fatal(err)
	}
	keptAsWritten(t, p, `"model": "uni-switch/deepseek/deepseek-flash"`, `"UNI_SWITCH": "1"`, `"ANTHROPIC_AUTH_TOKEN": "sk-user-own"`, `"Bash(git status)"`)
	if err := DelJSON(p, "env.UNI_SWITCH"); err != nil {
		t.Fatal(err)
	}
	got := keptAsWritten(t, p, `"ANTHROPIC_BASE_URL"`)
	if strings.Contains(got, "UNI_SWITCH") {
		t.Fatalf("DelJSON through a BOM left the key: %q", got)
	}
	if err := SetJSON(p, KV{Path: "model", Value: "deepseek-chat"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); got != claudeSettingsCRLF {
		t.Fatalf("set back, the file is not the user's own bytes:\n%q\nwant\n%q", got, claudeSettingsCRLF)
	}
}

func TestPatchJSONKeepsBOM(t *testing.T) {
	out, err := PatchJSON([]byte(claudeSettingsCRLF), []KV{{Path: "model", Value: "x"}}, []string{"alwaysThinkingEnabled"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, []byte(utf8BOM)) || bytes.HasPrefix(out[3:], []byte(utf8BOM)) || !bytes.Contains(out, []byte(`"model": "x"`)) || bytes.Contains(out, []byte("alwaysThinking")) {
		t.Fatalf("PatchJSON through a BOM: %q", out)
	}
}

// VS Code's chatLanguageModels.json, an array at the top level.
func TestJSONItemWithBOM(t *testing.T) {
	in := utf8BOM + "[\r\n  {\r\n    \"vendor\": \"customoai\",\r\n    \"name\": \"mine\"\r\n  }\r\n]\r\n"
	p := tmpFile(t, "chatLanguageModels.json", in)
	if _, ok := GetJSONItem(p, map[string]string{"name": "mine"}); !ok {
		t.Fatal("GetJSONItem through a BOM found nothing")
	}
	if err := SetJSONItem(p, map[string]string{"name": "uni-switch"}, map[string]any{"vendor": "customoai", "name": "uni-switch"}); err != nil {
		t.Fatal(err)
	}
	keptAsWritten(t, p, `"name": "mine"`, `"name": "uni-switch"`)
	if err := DelJSONItem(p, map[string]string{"name": "uni-switch"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); got != in {
		t.Fatalf("DelJSONItem through a BOM:\n%q\nwant\n%q", got, in)
	}
}

// Codex's config.toml saved by a Windows editor: Codex itself reads through
// the BOM (its toml parser skips one), where uni-switch refused the file with
// "UTF-8 BOM is not supported".
func TestTOMLConfigWithBOM(t *testing.T) {
	in := utf8BOM + "model = \"gpt-5\"\r\n\r\n[model_providers.mine]\r\nname = \"Mine\"\r\nbase_url = \"https://example.com/v1\"\r\n"
	p := tmpFile(t, "config.toml", in)
	if v, ok := GetTOMLTop(p, "model"); !ok || v != "gpt-5" {
		t.Fatalf("GetTOMLTop through a BOM: %q %v", v, ok)
	}
	if tables, err := TOMLTables(p); err != nil || len(tables) != 1 {
		t.Fatalf("TOMLTables through a BOM: %v %v", tables, err)
	}
	if err := SetTOMLTop(p, KV{Path: "model_provider", Value: "uni-switch"}); err != nil {
		t.Fatal(err)
	}
	if err := SetTOMLTable(p, "model_providers.uni-switch", KV{Path: "name", Value: "uni-switch"}); err != nil {
		t.Fatal(err)
	}
	keptAsWritten(t, p, `model_provider = "uni-switch"`, "[model_providers.uni-switch]", "[model_providers.mine]")
	if err := DelTOMLTable(p, "model_providers.uni-switch"); err != nil {
		t.Fatal(err)
	}
	if err := DelTOMLTop(p, "model_provider"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); got != in {
		t.Fatalf("taken back off, the file is not the user's own bytes:\n%q\nwant\n%q", got, in)
	}
}

// The first key of a .env or YAML file is its own, not one with the BOM
// glued to its name.
func TestEnvAndYAMLWithBOM(t *testing.T) {
	env := tmpFile(t, ".env", utf8BOM+"OPENAI_API_KEY=sk-own\r\nOTHER=1\r\n")
	if v, ok := GetEnvFile(env, "OPENAI_API_KEY"); !ok || v != "sk-own" {
		t.Fatalf("GetEnvFile through a BOM: %q %v", v, ok)
	}
	if err := SetEnvFile(env, KV{Path: "OPENAI_API_KEY", Value: "uni-switch"}); err != nil {
		t.Fatal(err)
	}
	got := keptAsWritten(t, env, "OPENAI_API_KEY=uni-switch", "OTHER=1")
	if strings.Count(got, "OPENAI_API_KEY") != 1 {
		t.Fatalf("SetEnvFile added the key beside the first one: %q", got)
	}
	y := tmpFile(t, "config.yaml", utf8BOM+"model: mine\r\nprovider: own\r\n")
	if v, ok := GetYAMLTop(y, "model"); !ok || v != "mine" {
		t.Fatalf("GetYAMLTop through a BOM: %q %v", v, ok)
	}
	if err := SetYAMLTop(y, KV{Path: "model", Value: "uni-switch"}); err != nil {
		t.Fatal(err)
	}
	got = keptAsWritten(t, y, "model: uni-switch", "provider: own")
	if strings.Count(got, "model:") != 1 {
		t.Fatalf("SetYAMLTop added the key beside the first one: %q", got)
	}
}

// A file without a BOM gets none, and a file emptied stays empty.
func TestWriteAtomicAddsNoBOM(t *testing.T) {
	p := tmpFile(t, "settings.json", "{\"a\": 1}\n")
	if err := SetJSON(p, KV{Path: "a", Value: 2}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); got != "{\"a\": 2}\n" {
		t.Fatalf("got %q", got)
	}
	q := tmpFile(t, "x.json", utf8BOM+"{}\n")
	if err := WriteAtomic(q, nil); err != nil {
		t.Fatal(err)
	}
	if got := read(t, q); got != "" {
		t.Fatalf("an emptied file got %q", got)
	}
	n := filepath.Join(t.TempDir(), "new.json")
	if err := WriteAtomic(n, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(n); string(b) != "{}" {
		t.Fatalf("a new file got %q", b)
	}
}
